package provisioner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ghwutil "github.com/jaypipes/ghw/pkg/util"
	"github.com/sirupsen/logrus"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/lvm"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type NeedMountUpdateOP int8

const (
	TypeLonghornV1 = "LonghornV1"
	TypeLonghornV2 = "LonghornV2"
	TypeLVM        = "LVM"

	// longhorn disk tags
	ErrorCacheDiskTagsNotInitialized = "CacheDiskTags is not initialized"

	// longhorn MountStatus
	NeedMountUpdateNoOp NeedMountUpdateOP = 1 << iota
	NeedMountUpdateMount
	NeedMountUpdateUnmount
)

func (f NeedMountUpdateOP) Has(flag NeedMountUpdateOP) bool {
	return f&flag != 0
}

type Provisioner interface {
	// Format is the Prerequisites for the provisioner
	// You should do the format-related operations (including mkfs, mount ...etc) here
	// Return values: bool1: isFormatComplete, bool2: isRequeueNeeded, error: error
	Format(string) (bool, bool, error)

	// UnFormat is the cleanup operation for the provisioner
	// You should call this after the UnProvision (if needed)
	// Return values: bool: isRequeueNeeded, error: error
	UnFormat() (bool, error)

	// Provision is the main operation for the provisioner
	// You should do all provision things like provision to specific storage, add to volume group ...etc
	// Return values: bool: isRequeueNeeded, error: error
	Provision() (bool, error)

	// UnProvision is the cleanup operation for the provisioner
	// You should cleanup all the things like remove from volume group, unprovision from storage ...etc
	// Return values: bool: isRequeueNeeded, error: error
	UnProvision() (bool, error)

	// Update is the mechanism to update anything you needed.
	// Like tags on the longhorn nodes, ensure the vg active for LVM ...etc
	// Return values: bool: isRequeueNeeded, error: error
	Update() (bool, error)
}

type provisioner struct {
	name      string
	blockInfo block.Info
	device    *diskv1.BlockDevice
}

func (p *provisioner) GetProvisionerName() string {
	return p.name
}

// wipeDevice is a helper function to clean up the device before using it.
// E.g., existing LVM and filesystem artifacts will be removed.
func (p *provisioner) wipeDevice(devPath string) error {
	logrus.WithFields(logrus.Fields{
		"provisioner": p.name,
		"device":      devPath,
	}).Info("Wiping the device")
	// Cleanup LVM artifacts from the device if it was used by LVM before.
	err := lvm.Cleanup(devPath)
	if err != nil {
		return err
	}
	// Remove any existing filesystem artifacts from the device.
	_, err = utils.NewExecutor().Execute("wipefs", []string{"-a", devPath})
	if err != nil {
		return err
	}
	return nil
}

func setCondDiskAddedToNodeFalse(device *diskv1.BlockDevice, message string, targetStatus diskv1.BlockDeviceProvisionPhase) {
	device.Status.ProvisionPhase = targetStatus
	diskv1.DiskAddedToNode.SetError(device, "", nil)
	diskv1.DiskAddedToNode.SetStatusBool(device, false)
	diskv1.DiskAddedToNode.Message(device, message)
}

func setCondDiskAddedToNodeTrue(device *diskv1.BlockDevice, message string, targetStatus diskv1.BlockDeviceProvisionPhase) {
	device.Status.ProvisionPhase = targetStatus
	diskv1.DiskAddedToNode.SetError(device, "", nil)
	diskv1.DiskAddedToNode.SetStatusBool(device, true)
	diskv1.DiskAddedToNode.Message(device, message)
}

func SetCondDeviceFormattingFail(device *diskv1.BlockDevice, err error) {
	diskv1.DeviceFormatting.SetError(device, "", err)
	diskv1.DeviceFormatting.SetStatusBool(device, false)
}

// DiskTags is a cache mechanism for the blockdevices Tags (spec.Tags), it only changed from Harvester side.
type DiskTags struct {
	diskTags    map[string][]string
	lock        *sync.RWMutex
	initialized bool
}

func NewLonghornDiskTags() *DiskTags {
	return &DiskTags{
		diskTags:    make(map[string][]string),
		lock:        &sync.RWMutex{},
		initialized: false,
	}
}

func (d *DiskTags) DeleteDiskTags(dev string) {
	d.lock.Lock()
	defer d.lock.Unlock()

	delete(d.diskTags, dev)
}

func (d *DiskTags) UpdateDiskTags(dev string, tags []string) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.diskTags[dev] = tags
}

func (d *DiskTags) UpdateInitialized() {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.initialized = true
}

func (d *DiskTags) Initialized() bool {
	d.lock.RLock()
	defer d.lock.RUnlock()

	return d.initialized
}

func (d *DiskTags) GetDiskTags(dev string) []string {
	d.lock.RLock()
	defer d.lock.RUnlock()

	return d.diskTags[dev]
}

func (d *DiskTags) DevExist(dev string) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()

	_, found := d.diskTags[dev]
	return found
}

// semaphore is a simple semaphore implementation in channel
type Semaphore struct {
	ch chan struct{}
}

// newSemaphore creates a new semaphore with the given capacity.
func NewSemaphore(n uint) *Semaphore {
	return &Semaphore{
		ch: make(chan struct{}, n),
	}
}

// acquire a semaphore to prevent concurrent update
func (s *Semaphore) acquire() bool {
	logrus.Debugf("Pre-acquire channel stats: %d/%d", len(s.ch), cap(s.ch))
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		// full
		return false
	}
}

// release the semaphore
func (s *Semaphore) release() bool {
	select {
	case <-s.ch:
		return true
	default:
		// empty
		return false
	}
}

func valueExists(value string) bool {
	return value != "" && value != ghwutil.UNKNOWN
}

func convertMountStr(mountOP NeedMountUpdateOP) string {
	switch mountOP {
	case NeedMountUpdateNoOp:
		return "No-Op"
	case NeedMountUpdateMount:
		return "Mount"
	case NeedMountUpdateUnmount:
		return "Unmount"
	default:
		return "Unknown OP"
	}
}

// ResolvePersistentDevPath tries to determine the currently active short
// device path (e.g. "/dev/sda") for a given block device.  When the scanner
// first finds a new block device, device.Spec.DevPath is set to the short
// device path at that time, and device.Status.DeviceStatus.Details is filled
// in with data that uniquely identifies the device (e.g.: WWN).  It's possible
// that on subsequent reboots, the short path will change, for example if
// devices are added or removed, so we have this function to try to figure
// out the _current_ short device path based on the unique identifying
// information in device.Status.DeviceStatus.Details.
func ResolvePersistentDevPath(device *diskv1.BlockDevice) (string, error) {
	switch device.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		// The following closure is the original implementation to resolve disk
		// device paths, but there is a problem: it multipathd has taken over a
		// device, the calls to filepath.EvalSymlinks() with "/dev/disk/by-id/"
		// or "/dev/disk/by-uuid" paths will actually return a "/dev/dm-*"
		// device, which we don't want.  We want the real underlying device
		// (e.g. "/dev/sda").  If we take a "/dev/dm-*" path and later update
		// the blockdevice CR with it, we lose all the interesting DeviceStatus
		// information, like the WWN.
		path, err := func() (string, error) {
			// Disk naming priority.
			// #1 WWN (REF: https://en.wikipedia.org/wiki/World_Wide_Name#Formats)
			// #2 filesystem UUID (UUID) (REF: https://wiki.archlinux.org/title/Persistent_block_device_naming#by-uuid)
			// #3 partition table UUID (PTUUID) (DEPRECATED)
			// #4 PtUUID as UUID to query disk info (DEPRECATED)
			//    (NDM might reuse PtUUID as UUID to format a disk)
			if wwn := device.Status.DeviceStatus.Details.WWN; valueExists(wwn) {
				if device.Status.DeviceStatus.Details.StorageController == string(diskv1.StorageControllerNVMe) {
					return filepath.EvalSymlinks("/dev/disk/by-id/nvme-" + wwn)
				}
				return filepath.EvalSymlinks("/dev/disk/by-id/wwn-" + wwn)
			}
			if fsUUID := device.Status.DeviceStatus.Details.UUID; valueExists(fsUUID) {
				path, err := filepath.EvalSymlinks("/dev/disk/by-uuid/" + fsUUID)
				if err == nil {
					return path, nil
				}
				if !errors.Is(err, os.ErrNotExist) {
					return "", err
				}
			}

			if ptUUID := device.Status.DeviceStatus.Details.PtUUID; valueExists(ptUUID) {
				path, err := block.GetDevPathByPTUUID(ptUUID)
				if err != nil {
					return "", err
				}
				if path != "" {
					return path, nil
				}
				return filepath.EvalSymlinks("/dev/disk/by-uuid/" + ptUUID)
			}

			// If we haven't resolved a path by now, it means the device has no WWN,
			// no FSUUID and no PTUUID, but there is still one more thing we can try...
			return "", nil
		}()

		if err != nil {
			return "", err
		}

		// ...at this point, if there's no error, we've either got the device we're
		// interested in (e.g. "/dev/sda", "/dev/nvme0n1", etc.), _or_ we've got a
		// "/dev/dm-*" device, _or_ we've got no path...
		// if it's a multipath device, we should also return the path directly.
		if path != "" {
			if !strings.HasPrefix(path, "/dev/dm-") {
				// Not a multipath device or longhorn v2 device, we can use the path directly
				logrus.Debugf("Resolved device path %s for %s", path, device.Name)
				return path, nil
			}

			if _, err := utils.IsMultipathDevice(path); err == nil {
				logrus.Debugf("Resolved device path %s for %s", path, device.Name)
				return path, nil
			}
		}
		// ...in the latter two cases, we can try to resolve via "/dev/disk/by-path/...",
		// which works for devices that don't have a WWN, and also in the dm case will
		// return the path to the underlying device that we're actually interested in.
		if busPath := device.Status.DeviceStatus.Details.BusPath; valueExists(busPath) {
			path, err = filepath.EvalSymlinks("/dev/disk/by-path/" + busPath)
			if err == nil {
				logrus.Debugf("Resolved BusPath %s to %s for %s", busPath, path, device.Name)
				return path, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}

		return "", fmt.Errorf("WWN/UUID/PTUUID/BusPath was not found on device %s", device.Name)
	case diskv1.DeviceTypePart:
		partUUID := device.Status.DeviceStatus.Details.PartUUID
		if partUUID == "" {
			return "", fmt.Errorf("PARTUUID was not found on device %s", device.Name)
		}
		return filepath.EvalSymlinks("/dev/disk/by-partuuid/" + partUUID)
	default:
		return "", fmt.Errorf("failed to resolve persistent dev path for block device %s", device.Name)
	}
}
