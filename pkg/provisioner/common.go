package provisioner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	ghwutil "github.com/jaypipes/ghw/pkg/util"
	"github.com/sirupsen/logrus"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
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

func ResolvePersistentDevPath(device *diskv1.BlockDevice) (string, error) {
	switch device.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
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
		return "", fmt.Errorf("WWN/UUID/PTUUID was not found on device %s", device.Name)
	case diskv1.DeviceTypePart:
		partUUID := device.Status.DeviceStatus.Details.PartUUID
		if partUUID == "" {
			return "", fmt.Errorf("PARTUUID was not found on device %s", device.Name)
		}
		return filepath.EvalSymlinks("/dev/disk/by-partuuid/" + partUUID)
	default:
		return "", nil
	}
}
