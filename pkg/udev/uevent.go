package udev

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/udev/netlink"
)

var (
	// restartDelay is the pause before a failed watcher is started again.
	restartDelay = 2 * time.Second

	// multipathSettleDelay is how long to wait with a new device-mapper disk,
	// so multipathd can finish (re)building the multipath map.
	multipathSettleDelay = time.Second
)

// Scanner is the part of the block device scanner the watchers need.
type Scanner interface {
	// Wake requests a scan of all disks of the node. It must not block.
	Wake()
	// ApplyExcludeFiltersForDisk returns true if the disk has to be ignored.
	ApplyExcludeFiltersForDisk(disk *block.Disk) bool
}

// Udev watches the host for changes that matter for the block devices of the
// node and wakes the scanner. The scanner stays the only one that reconciles
// BlockDevice resources. As every scan looks at the complete state of the node,
// the watchers only have to say "something changed", not what exactly.
//
// There are two independent sources:
//   - udev events (add/remove of disks), see monitor.
//   - changes of the host mount table, see watchMounts. A mount does not
//     necessarily come with a block event, but decides if a disk is in use.
//
// Both follow the usual pattern for watches: after (re)connecting the scanner is
// woken once, as everything that happened while not watching has been missed,
// and a failed watcher is restarted until the context is cancelled.
type Udev struct {
	namespace     string
	nodeName      string
	scanner       Scanner
	blockInfo     block.Info
	mountInfoPath string

	// injectError makes the next monitor run fail, to test the restart in CI.
	injectError atomic.Bool
}

func NewUdev(opt *option.Option, scanner Scanner, blockInfo block.Info) *Udev {
	u := &Udev{
		namespace:     opt.Namespace,
		nodeName:      opt.NodeName,
		scanner:       scanner,
		blockInfo:     blockInfo,
		mountInfoPath: hostMountInfo,
	}
	u.injectError.Store(opt.InjectUdevMonitorError)
	return u
}

// Monitor starts the watchers in the background. They run until ctx is cancelled.
func (u *Udev) Monitor(ctx context.Context) {
	go u.run(ctx, "udev monitor", u.monitor)
	go u.run(ctx, "mount watcher", u.watchMounts)
}

// run executes watch again and again until ctx is cancelled. A watcher only
// returns on failure or on cancellation.
func (u *Udev) run(ctx context.Context, name string, watch func(context.Context) error) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := watch(ctx); err != nil && ctx.Err() == nil {
			logrus.WithError(err).Errorf("The %s failed, restarting", name)
		}
	}, restartDelay)
}

// monitor receives udev events until ctx is cancelled or the connection fails.
func (u *Udev) monitor(ctx context.Context) error {
	if u.injectError.CompareAndSwap(true, false) {
		return errors.New("testing error")
	}

	conn, err := netlink.Dial()
	if err != nil {
		return fmt.Errorf("connect to udev events: %w", err)
	}
	defer conn.Close()

	// Conn.Read does not know about contexts, closing the connection unblocks it.
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()

	logrus.WithField("node", u.nodeName).Info("Start monitoring udev events")
	u.scanner.Wake()

	for {
		properties, err := conn.Read()
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, netlink.ErrEventsLost):
			logrus.WithField("node", u.nodeName).Warn("Udev events lost, rescanning all disks")
			u.scanner.Wake()
		case err != nil:
			return fmt.Errorf("receive udev event: %w", err)
		default:
			u.handle(Device(properties))
		}
	}
}

// handle wakes the scanner if a disk was added or removed, other events are ignored.
func (u *Udev) handle(device Device) {
	action := device.Action()
	if (action != ActionAdd && action != ActionRemove) || !device.IsDisk() {
		return
	}
	devPath := device.GetDevName()
	if devPath == "" {
		logrus.WithFields(logrus.Fields{
			"node":   u.nodeName,
			"action": action,
		}).Debug("Ignoring udev event without device name")
		return
	}

	logrus.WithFields(logrus.Fields{
		"node":   u.nodeName,
		"action": action,
		"device": devPath,
	}).Debug("Handling udev event")

	if action == ActionRemove {
		// The device is gone from sysfs already, so only the event itself
		// tells which disk it was. The scanner deactivates or deletes its BlockDevice.
		disk := &block.Disk{Name: strings.TrimPrefix(devPath, "/dev/")}
		device.UpdateDiskFromUdev(disk)
		logrus.WithFields(logrus.Fields{
			"node":      u.nodeName,
			"namespace": u.namespace,
			"device":    devPath,
			"vendor":    disk.Vendor,
			"model":     disk.Model,
			"serial":    disk.SerialNumber,
			"wwn":       disk.WWN,
		}).Info("Disk removed")
		u.scanner.Wake()
		return
	}

	if strings.HasPrefix(filepath.Base(devPath), "dm-") {
		// The scan is level-triggered, so it does not matter that this may
		// overtake the events that follow.
		time.AfterFunc(multipathSettleDelay, func() { u.handleAdd(devPath) })
		return
	}
	u.handleAdd(devPath)
}

func (u *Udev) handleAdd(devPath string) {
	disk := u.blockInfo.GetDiskByDevPath(devPath)
	if disk == nil {
		logrus.WithFields(logrus.Fields{
			"node":   u.nodeName,
			"device": devPath,
		}).Warn("Unable to query details of added disk")
		return
	}
	if u.scanner.ApplyExcludeFiltersForDisk(disk) {
		return
	}

	logrus.WithFields(logrus.Fields{
		"node":      u.nodeName,
		"namespace": u.namespace,
		"device":    devPath,
	}).Info("Disk added")
	u.scanner.Wake()
}
