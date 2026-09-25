package udev

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/controller/blockdevice"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/udev/netlink"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

// Udev bridges host device changes into NDM's block-device scanner.
//
// Linux emits a uevent when a device is added, removed, or changed. udevd
// receives that kernel event, applies its rules, and publishes the enriched
// result to the processed-event multicast group. This type listens for those
// enriched events and wakes the scanner; the scanner remains the sole owner
// of BlockDevice reconciliation and Kubernetes API writes.
type Udev struct {
	namespace   string
	nodeName    string
	scanner     *blockdevice.Scanner
	injectError bool
}

func NewUdev(opt *option.Option, scanner *blockdevice.Scanner) *Udev {
	return &Udev{
		namespace:   opt.Namespace,
		nodeName:    opt.NodeName,
		scanner:     scanner,
		injectError: opt.InjectUdevMonitorError,
	}
}

func (u *Udev) Monitor(ctx context.Context) {
	// Netlink delivery and host mount changes are independent sources of state
	// changes. Run both watchers: a mount/umount does not necessarily produce a
	// useful block uevent, but it can change whether NDM may use a disk.
	//
	// A read failure terminates one watcher. Its supervisor below restarts it so
	// a transient host-side failure does not permanently disable reconciliation.
	udevErrChan := make(chan error, 1)
	go u.spawnMonitor(ctx, udevErrChan)
	mountErrChan := make(chan error, 1)
	go u.spawnMountWatcher(ctx, mountErrChan)
}

func (u *Udev) spawnMonitor(ctx context.Context, errChan chan error) {
	// The monitor returns only on a fatal socket error or cancellation. Keep the
	// supervisor separate from the worker so the worker can be replaced safely.
	go u.monitor(ctx, errChan)
	for {
		select {
		case err, ok := <-errChan:
			if !ok {
				return
			}
			if err != nil {
				logrus.WithError(err).Error("Failed to monitor udev events")
			}
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
			go u.monitor(ctx, errChan)
		case <-ctx.Done():
			return
		}
	}
}

func (u *Udev) spawnMountWatcher(ctx context.Context, errChan chan error) {
	// Mount watching uses the same restart model as uevent monitoring. This is
	// intentionally a separate worker because it watches a procfs file, not a
	// Netlink socket.
	go u.watchMounts(ctx, errChan)
	for {
		select {
		case err, ok := <-errChan:
			if !ok {
				return
			}
			if err != nil {
				logrus.WithError(err).Error("Failed to watch mounts")
			}
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
			go u.watchMounts(ctx, errChan)
		case <-ctx.Done():
			return
		}
	}
}

func (u *Udev) monitor(ctx context.Context, errors chan error) {
	logrus.WithField("node", u.nodeName).Info("Start monitoring udev processed events")

	// Simulator hook: injects artificial error for testing the respawn logic.
	if u.injectError {
		logrus.Infof("Injecting error to udev monitor for testing")
		select {
		case errors <- fmt.Errorf("testing error"):
		case <-ctx.Done():
		}
		u.injectError = false
		return
	}

	// Connect to the Netlink socket listening for udev processed events (group 2).
	conn, err := netlink.DialUdev()
	if err != nil {
		select {
		case errors <- fmt.Errorf("unable to connect to netlink uevent socket: %w", err):
		case <-ctx.Done():
		}
		return
	}
	defer conn.Close()

	uqueue := make(chan *netlink.UEvent, 32)
	errChan := make(chan error, 1)

	// monitorCtx ensures that the background reader goroutine is cancelled
	// whenever monitor exits (e.g. on socket error or parent ctx cancellation).
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		errChan <- conn.Monitor(monitorCtx, uqueue)
	}()

	// Event handling loop: dispatch incoming uevents or report monitor errors.
	for {
		select {
		case uevent := <-uqueue:
			u.ActionHandler(uevent)
		case err := <-errChan:
			if err != nil {
				select {
				case errors <- err:
				case <-ctx.Done():
				}
			} else if ctx.Err() == nil {
				select {
				case errors <- fmt.Errorf("udev monitor exited unexpectedly"):
				case <-ctx.Done():
				}
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

// ActionHandler processes an incoming udev event and triggers scanner wake-ups.
func (u *Udev) ActionHandler(uevent *netlink.UEvent) {
	if uevent == nil {
		return
	}

	// Buffer overrun recovery: wake scanner immediately for full reconciliation.
	if uevent.Action == netlink.ActionRescan {
		logrus.WithField("node", u.nodeName).Info("Udev buffer overrun rescan triggering scanner wake")
		u.wakeUpScanner(uevent, "all")
		return
	}

	// Filter out non-disk devices (e.g. partitions, cdrom, loop devices).
	udevDevice := Device(uevent.Env)
	if !udevDevice.IsDisk() {
		return
	}
	devPath := udevDevice.GetDevName()
	if devPath == "" {
		logrus.WithFields(logrus.Fields{
			"node":        u.nodeName,
			"udevAction":  uevent.Action,
			"udevDevName": uevent.DevName,
			"udevDevPath": uevent.DevPath,
		}).Debug("Skipping udev action: unable to determine device path")
		return
	}
	logrus.WithFields(logrus.Fields{
		"node":          u.nodeName,
		"udevAction":    uevent.Action,
		"udevDevName":   uevent.DevName,
		"udevDevPath":   uevent.DevPath,
		"udevDevType":   udevDevice[UdevDevtype],
		"udevSubsystem": uevent.Subsystem,
		"device":        devPath,
	}).Debug("Handling udev action")

	// Multipath stabilization delay:
	// In multipath setups (Fibre Channel, iSCSI, SAS multipath), physical path changes
	// (e.g. sda, sdb) cause multipathd to reload the underlying device-mapper target (/dev/dm-X).
	// During this brief kernel table reload window, accessing the DM device can return EBUSY,
	// ENXIO, or transient stale state. Waiting 1 second allows multipathd to finish reassembling
	// the map before NDM interrogates the device.
	// We execute this asynchronously for added devices so the uevent receive loop is not blocked.
	if strings.Contains(devPath, "dm-") && uevent.Action == netlink.ActionAdd {
		go func() {
			time.Sleep(1 * time.Second)
			u.handleAction(uevent, udevDevice, devPath)
		}()
		return
	}

	u.handleAction(uevent, udevDevice, devPath)
}

func (u *Udev) handleAction(uevent *netlink.UEvent, udevDevice Device, devPath string) {
	var disk *block.Disk

	if uevent.Action == netlink.ActionRemove {
		// When a disk is removed (unplugged, Fibre Channel zone removed, virtual disk detached),
		// the Linux VFS/kobject subsystem removes the sysfs directory (/sys/class/block/<dev>)
		// BEFORE emitting the netlink remove event.
		// Consequently, GetDiskByDevPath() would fail with ENOENT. We instead reconstruct the minimal
		// identity (vendor, model, serial, WWN) from the uevent's Env map and pass it to the scanner
		// so it can mark the corresponding Kubernetes BlockDevice resource as Unhealthy/Missing.
		disk = &block.Disk{Name: strings.TrimPrefix(devPath, "/dev/")}
		udevDevice.UpdateDiskFromUdev(disk)
		logrus.WithFields(logrus.Fields{
			"node":   u.nodeName,
			"device": devPath,
			"vendor": disk.Vendor,
			"model":  disk.Model,
			"serial": disk.SerialNumber,
			"wwn":    disk.WWN,
		}).Info("Removing disk")
		// Wake up scanner to reconcile and mark the block device resource as gone/unhealthy.
		u.wakeUpScanner(uevent, devPath)
		return
	}

	// Only "add" and "remove" trigger scanner actions; ignore other actions (e.g. "change").
	if uevent.Action != netlink.ActionAdd {
		return
	}

	// For added disks, query sysfs/udev for full block device details.
	if u.scanner == nil || u.scanner.BlockInfo == nil {
		return
	}
	disk = u.scanner.BlockInfo.GetDiskByDevPath(devPath)
	if disk == nil {
		logrus.WithFields(logrus.Fields{
			"node":   u.nodeName,
			"device": devPath,
		}).Warn("Unable to query block device details from sysfs/udev")
		return
	}

	// Skip disks matched by global exclusion filters (e.g. OS disk, Longhorn disks).
	if u.scanner.ApplyExcludeFiltersForDisk(disk) {
		return
	}

	// Wake up scanner to create or update the BlockDevice CR in Kubernetes.
	u.wakeUpScanner(uevent, devPath)
}

func (u *Udev) wakeUpScanner(uevent *netlink.UEvent, devPath string) {
	if u.scanner == nil || u.scanner.Cond == nil {
		logrus.WithFields(logrus.Fields{
			"node":       u.nodeName,
			"udevAction": uevent.Action,
			"device":     devPath,
		}).Warn("Scanner or condition variable not initialized, skipping scanner wake")
		return
	}
	// Storage event coalescing:
	// Hotplugging a single drive can generate dozens of uevents in a few milliseconds
	// (disk add, partition table scan, partition add 1..N, filesystem probe, by-path/by-id symlinks).
	// Spawning an API reconciliation for every single uevent would overwhelm the Kubernetes API server.
	// NDM uses a sync.Cond to coalesce bursts: Signal() wakes up the scanner loop once to reconcile
	// all node disks in a single pass.
	utils.CallerWithCondLock(u.scanner.Cond, func() any {
		logrus.WithFields(logrus.Fields{
			"node":       u.nodeName,
			"namespace":  u.namespace,
			"kind":       "BlockDevice",
			"udevAction": uevent.Action,
			"device":     devPath,
		}).Info("Udev action triggering scanner wake")
		u.scanner.Cond.Signal()
		return nil
	})
}
