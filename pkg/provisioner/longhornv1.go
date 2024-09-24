package provisioner

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	gocommon "github.com/harvester/go-common"
	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta2"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type LonghornV1Provisioner struct {
	*provisioner
	nodeObj          *longhornv1.Node
	nodesClientCache ctllonghornv1.NodeCache
	nodesClient      ctllonghornv1.NodeClient

	cacheDiskTags *DiskTags
	semaphoreObj  *Semaphore
}

func NewLHV1Provisioner(
	device *diskv1.BlockDevice,
	block block.Info,
	nodeObj *longhornv1.Node,
	nodesClient ctllonghornv1.NodeClient,
	nodesClientCache ctllonghornv1.NodeCache,
	cacheDiskTags *DiskTags,
	semaphore *Semaphore,
) (Provisioner, error) {
	baseProvisioner := &provisioner{
		name:      TypeLonghornV1,
		blockInfo: block,
		device:    device,
	}
	provisioner := &LonghornV1Provisioner{
		provisioner:      baseProvisioner,
		nodeObj:          nodeObj,
		nodesClient:      nodesClient,
		nodesClientCache: nodesClientCache,
		cacheDiskTags:    cacheDiskTags,
		semaphoreObj:     semaphore,
	}

	if !cacheDiskTags.Initialized() {
		return nil, errors.New(ErrorCacheDiskTagsNotInitialized)
	}
	return provisioner, nil
}

func (p *LonghornV1Provisioner) GetProvisionerName() string {
	return p.name
}

func (p *LonghornV1Provisioner) Provision() (bool, error) {
	logrus.Infof("%s provisioning Longhorn block device %s", p.name, p.device.Name)

	nodeObjCpy := p.nodeObj.DeepCopy()
	tags := []string{}
	if p.device.Spec.Tags != nil {
		tags = p.device.Spec.Tags
	}
	diskSpec := longhornv1.DiskSpec{
		Type:              longhornv1.DiskTypeFilesystem,
		Path:              extraDiskMountPoint(p.device),
		AllowScheduling:   true,
		EvictionRequested: false,
		StorageReserved:   0,
		Tags:              tags,
	}

	// checked the case that longhorn node updated but blockdevice CRD is not updated
	synced := false
	provisioned := false
	if disk, found := p.nodeObj.Spec.Disks[p.device.Name]; found {
		synced = reflect.DeepEqual(disk, diskSpec)
		if !synced {
			logrus.Warnf("The disk spec should not differ between longhorn node and blockdevice CRD, disk: %+v, diskSpec: %+v", disk, diskSpec)
		}
	}
	if !synced {
		logrus.Debugf("Try to sync disk %s to Longhorn node %s/%s again", p.device.Name, p.nodeObj.Namespace, p.nodeObj.Name)
		nodeObjCpy.Spec.Disks[p.device.Name] = diskSpec
		if _, err := p.nodesClient.Update(nodeObjCpy); err != nil {
			return true, err
		}
		provisioned = true
	}

	if (synced && !diskv1.DiskAddedToNode.IsTrue(p.device)) || provisioned {
		logrus.Debugf("Set blockdevice CRD (%v) to provisioned", p.device)
		msg := fmt.Sprintf("Added disk %s to longhorn node `%s` as an additional disk", p.device.Name, p.nodeObj.Name)
		setCondDiskAddedToNodeTrue(p.device, msg, diskv1.ProvisionPhaseProvisioned)
	}

	p.cacheDiskTags.UpdateDiskTags(p.device.Name, p.device.Spec.Tags)
	return false, nil
}

func (p *LonghornV1Provisioner) UnProvision() (bool, error) {
	logrus.Infof("%s unprovisioning Longhorn block device %s", p.name, p.device.Name)

	// inner functions
	updateProvisionPhaseUnprovisioned := func() {
		msg := fmt.Sprintf("Disk not in longhorn node `%s`", p.nodeObj.Name)
		setCondDiskAddedToNodeFalse(p.device, msg, diskv1.ProvisionPhaseUnprovisioned)
	}

	removeDiskFromNode := func() error {
		nodeCpy := p.nodeObj.DeepCopy()
		delete(nodeCpy.Spec.Disks, p.device.Name)
		if _, err := p.nodesClient.Update(nodeCpy); err != nil {
			return err
		}
		return nil
	}

	isValidateToDelete := func(lhDisk longhornv1.DiskSpec) bool {
		return !lhDisk.AllowScheduling
	}

	diskToRemove, ok := p.nodeObj.Spec.Disks[p.device.Name]
	if !ok {
		logrus.Infof("disk %s not in disks of longhorn node %s/%s", p.device.Name, p.nodeObj.Namespace, p.nodeObj.Name)
		updateProvisionPhaseUnprovisioned()
		return false, nil
	}

	isUnprovisioning := false
	for _, tag := range p.device.Status.Tags {
		if tag == utils.DiskRemoveTag {
			isUnprovisioning = true
			break
		}
	}

	// for inactive/corrupted disk, we could remove it from node directly
	if isUnprovisioning && isValidateToDelete(diskToRemove) &&
		(p.device.Status.State == diskv1.BlockDeviceInactive || p.device.Status.DeviceStatus.FileSystem.Corrupted) {
		logrus.Infof("disk (%s) is inactive or corrupted, remove it from node directly", p.device.Name)
		p.unmountTheBrokenDisk()

		if err := removeDiskFromNode(); err != nil {
			return true, err
		}
		updateProvisionPhaseUnprovisioned()
		return false, nil
	}

	if isUnprovisioning {
		if status, ok := p.nodeObj.Status.DiskStatus[p.device.Name]; ok && len(status.ScheduledReplica) == 0 {
			// Unprovision finished. Remove the disk.
			if err := removeDiskFromNode(); err != nil {
				return true, err
			}
			updateProvisionPhaseUnprovisioned()
			logrus.Debugf("device %s is unprovisioned", p.device.Name)
		} else {
			// Still unprovisioning
			logrus.Debugf("device %s is unprovisioning, status: %+v, ScheduledReplica: %d", p.device.Name, p.nodeObj.Status.DiskStatus[p.device.Name], len(status.ScheduledReplica))
			return true, nil
		}
	} else {
		// Start unprovisioing
		if err := p.excludeTheDisk(diskToRemove); err != nil {
			return true, err
		}
		msg := fmt.Sprintf("Stop provisioning device %s to longhorn node `%s`", p.device.Name, p.nodeObj.Name)
		setCondDiskAddedToNodeFalse(p.device, msg, diskv1.ProvisionPhaseUnprovisioning)
	}

	return false, nil

}

func (p *LonghornV1Provisioner) unmountTheBrokenDisk() {
	filesystem := p.blockInfo.GetFileSystemInfoByDevPath(p.device.Status.DeviceStatus.DevPath)
	if filesystem != nil && filesystem.MountPoint != "" {
		if err := utils.ForceUmountWithTimeout(filesystem.MountPoint, 30*time.Second); err != nil {
			logrus.Warnf("Force umount %v error: %v", filesystem.MountPoint, err)
		}
		// reset related fields
		p.updateDeviceFileSystem(p.device, p.device.Status.DeviceStatus.DevPath)
		p.device.Spec.Tags = []string{}
		p.device.Status.Tags = []string{}
	}
}

func (p *LonghornV1Provisioner) excludeTheDisk(targetDisk longhornv1.DiskSpec) error {
	logrus.Debugf("Setup device %s to start unprovision", p.device.Name)
	targetDisk.AllowScheduling = false
	targetDisk.EvictionRequested = true
	targetDisk.Tags = append(targetDisk.Tags, utils.DiskRemoveTag)
	nodeCpy := p.nodeObj.DeepCopy()
	nodeCpy.Spec.Disks[p.device.Name] = targetDisk
	if _, err := p.nodesClient.Update(nodeCpy); err != nil {
		return err
	}
	return nil
}

// Update is used to update the disk tags
func (p *LonghornV1Provisioner) Update() (bool, error) {

	DiskTagsOnNodeMissed := func(targetDisk longhornv1.DiskSpec) bool {
		for _, tag := range p.device.Spec.Tags {
			if !slices.Contains(targetDisk.Tags, tag) {
				return true
			}
		}
		return false
	}

	logrus.Infof("%s updating Longhorn block device %s", p.name, p.device.Name)
	targetDisk, found := p.nodeObj.Spec.Disks[p.device.Name]
	if !found {
		logrus.Warnf("disk %s not in disks of longhorn node, was it already provisioned?", p.device.Name)
		return false, nil
	}
	DiskTagsSynced := gocommon.SliceContentCmp(p.device.Spec.Tags, p.cacheDiskTags.GetDiskTags(p.device.Name))
	if !DiskTagsSynced || (DiskTagsSynced && DiskTagsOnNodeMissed(targetDisk)) {
		// The final tags: DiskSpec.Tags - DiskCacheTags + Device.Spec.Tags
		logrus.Debugf("Prepare to update device %s because the Tags changed, Spec: %v, CacheDiskTags: %v", p.device.Name, p.device.Spec.Tags, p.cacheDiskTags.GetDiskTags(p.device.Name))
		respectedTags := []string{}
		for _, tag := range targetDisk.Tags {
			if !slices.Contains(p.cacheDiskTags.GetDiskTags(p.device.Name), tag) {
				respectedTags = append(respectedTags, tag)
			}
		}
		targetDisk.Tags = gocommon.SliceDedupe(append(respectedTags, p.device.Spec.Tags...))
		nodeCpy := p.nodeObj.DeepCopy()
		nodeCpy.Spec.Disks[p.device.Name] = targetDisk
		if _, err := p.nodesClient.Update(nodeCpy); err != nil {
			return true, err
		}
	}
	p.cacheDiskTags.UpdateDiskTags(p.device.Name, p.device.Spec.Tags)
	return false, nil
}

func (p *LonghornV1Provisioner) Format(devPath string) (bool, bool, error) {
	logrus.Infof("%s formatting Longhorn block device %s", p.name, p.device.Name)
	var err error
	formatted := false
	requeue := false

	filesystem := p.blockInfo.GetFileSystemInfoByDevPath(devPath)
	devPathStatus := convertFSInfoToString(filesystem)
	logrus.Debugf("Get filesystem info from device %s, %s", devPath, devPathStatus)
	if p.needFormat() {
		logrus.Infof("Prepare to force format device %s", p.device.Name)
		requeue, err = p.forceFormatFS(p.device, devPath, filesystem)
		if err != nil {
			err := fmt.Errorf("failed to force format device %s: %s", p.device.Name, err.Error())
			diskv1.DeviceFormatting.SetError(p.device, "", err)
			diskv1.DeviceFormatting.SetStatusBool(p.device, false)
		}
		return formatted, requeue, err
	}

	if needMountUpdate := needUpdateMountPoint(p.device, filesystem); needMountUpdate != NeedMountUpdateNoOp {
		err := p.updateDeviceMount(p.device, devPath, filesystem, needMountUpdate)
		if err != nil {
			err := fmt.Errorf("failed to update device mount %s: %s", p.device.Name, err.Error())
			diskv1.DeviceMounted.SetError(p.device, "", err)
			diskv1.DeviceMounted.SetStatusBool(p.device, false)
		}
		return formatted, requeue, err
	}
	formatted = true
	return formatted, false, nil
}

func (p *LonghornV1Provisioner) UnFormat() (bool, error) {
	logrus.Infof("%s unformatting Longhorn block device %s", p.name, p.device.Name)
	return false, nil
}

func (p *LonghornV1Provisioner) updateDeviceMount(device *diskv1.BlockDevice, devPath string, filesystem *block.FileSystemInfo, needMountUpdate NeedMountUpdateOP) error {
	logrus.Infof("Prepare to try %s", convertMountStr(needMountUpdate))
	if device.Status.DeviceStatus.Partitioned {
		return fmt.Errorf("partitioned device is not supported, please use raw block device instead")
	}
	if needMountUpdate.Has(NeedMountUpdateUnmount) {
		logrus.Infof("Unmount device %s from path %s", device.Name, filesystem.MountPoint)
		if err := utils.UmountDisk(filesystem.MountPoint); err != nil {
			return err
		}
		diskv1.DeviceMounted.SetError(device, "", nil)
		diskv1.DeviceMounted.SetStatusBool(device, false)
	}
	if needMountUpdate.Has(NeedMountUpdateMount) {
		expectedMountPoint := extraDiskMountPoint(device)
		logrus.Infof("Mount deivce %s to %s", device.Name, expectedMountPoint)
		if err := utils.MountDisk(devPath, expectedMountPoint); err != nil {
			if utils.IsFSCorrupted(err) {
				logrus.Errorf("Target device may be corrupted, update FS info.")
				device.Status.DeviceStatus.FileSystem.Corrupted = true
				device.Spec.FileSystem.Repaired = false
			}
			return err
		}
		diskv1.DeviceMounted.SetError(device, "", nil)
		diskv1.DeviceMounted.SetStatusBool(device, true)
	}
	device.Status.DeviceStatus.FileSystem.Corrupted = false
	return p.updateDeviceFileSystem(device, devPath)
}

func (p *LonghornV1Provisioner) needFormat() bool {
	return p.device.Spec.FileSystem.ForceFormatted &&
		(p.device.Status.DeviceStatus.FileSystem.Corrupted || p.device.Status.DeviceStatus.FileSystem.LastFormattedAt == nil)
}

// forceFormat simply formats the device to ext4 filesystem
//
// - umount the block device if it is mounted
// - create ext4 filesystem on the block device
func (p *LonghornV1Provisioner) forceFormatFS(device *diskv1.BlockDevice, devPath string, filesystem *block.FileSystemInfo) (bool, error) {
	if !p.semaphoreObj.acquire() {
		logrus.Infof("Hit maximum concurrent count. Requeue device %s", device.Name)
		return true, nil
	}

	defer p.semaphoreObj.release()

	// before format, we need to unmount the device if it is mounted
	if filesystem != nil && filesystem.MountPoint != "" {
		logrus.Infof("Unmount %s for %s", filesystem.MountPoint, device.Name)
		if err := utils.UmountDisk(filesystem.MountPoint); err != nil {
			return false, err
		}
	}

	// ***TODO***: we should let people to use ext4 or xfs, but now...
	// make ext4 filesystem format of the partition disk
	logrus.Debugf("Make ext4 filesystem format of device %s", device.Name)

	// Reuse UUID if possible to make the filesystem UUID more stable.
	//
	// **NOTE**: We should highly depends on the WWN, the filesystem UUID
	// is not stable because it store in the filesystem.
	//
	// The reason filesystem UUID needs to be stable is that if a disk
	// lacks WWN, NDM then needs a UUID to determine the unique identity
	// of the blockdevice CR.
	//
	// We don't reuse WWN as UUID here because we assume that WWN is
	// stable and permanent for a disk. Thefore, even if the underlying
	// device gets formatted and the filesystem UUID changes, it still
	// won't affect then unique identity of the blockdevice.
	var uuid string
	if !valueExists(device.Status.DeviceStatus.Details.WWN) {
		uuid = device.Status.DeviceStatus.Details.UUID
		if !valueExists(uuid) {
			uuid = device.Status.DeviceStatus.Details.PtUUID
		}
		if !valueExists(uuid) {
			// Reset the UUID to prevent "unknown" being passed down.
			uuid = ""
		}
	}
	if err := utils.MakeExt4DiskFormatting(devPath, uuid); err != nil {
		return false, err
	}

	// HACK: Update the UUID if it is reused.
	//
	// This makes the controller able to find then device after
	// a PtUUID is reused in `mkfs.ext4` as filesystem UUID.
	//
	// If the UUID is not updated within one-stop, the next
	// `OnBlockDeviceChange` is not able to find the device
	// because `status.DeviceStatus.Details.UUID` is missing.
	if uuid != "" {
		device.Status.DeviceStatus.Details.UUID = uuid
	}

	if err := p.updateDeviceFileSystem(device, devPath); err != nil {
		return false, err
	}
	diskv1.DeviceFormatting.SetError(device, "", nil)
	diskv1.DeviceFormatting.SetStatusBool(device, false)
	diskv1.DeviceFormatting.Message(device, "Done device ext4 filesystem formatting")
	device.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
	device.Status.DeviceStatus.Partitioned = false
	device.Status.DeviceStatus.FileSystem.Corrupted = false
	return false, nil
}

func (p *LonghornV1Provisioner) updateDeviceFileSystem(device *diskv1.BlockDevice, devPath string) error {
	if device.Status.DeviceStatus.FileSystem.Corrupted {
		// do not need to update other fields, we only need to update the corrupted flag
		return nil
	}
	filesystem := p.blockInfo.GetFileSystemInfoByDevPath(devPath)
	if filesystem == nil {
		return fmt.Errorf("failed to get filesystem info from devPath %s", devPath)
	}
	if filesystem.MountPoint != "" && filesystem.Type != "" && !utils.IsSupportedFileSystem(filesystem.Type) {
		return fmt.Errorf("unsupported filesystem type %s", filesystem.Type)
	}

	device.Status.DeviceStatus.FileSystem.MountPoint = filesystem.MountPoint
	device.Status.DeviceStatus.FileSystem.Type = filesystem.Type
	device.Status.DeviceStatus.FileSystem.IsReadOnly = filesystem.IsReadOnly
	return nil
}

func extraDiskMountPoint(bd *diskv1.BlockDevice) string {
	// DEPRECATED: only for backward compatibility
	if bd.Spec.FileSystem.MountPoint != "" {
		return bd.Spec.FileSystem.MountPoint
	}

	return fmt.Sprintf("/var/lib/harvester/extra-disks/%s", bd.Name)
}

func convertFSInfoToString(fsInfo *block.FileSystemInfo) string {
	// means this device is not mounted
	if fsInfo.MountPoint == "" {
		return "device is not mounted"
	}
	return fmt.Sprintf("mountpoint: %s, fsType: %s", fsInfo.MountPoint, fsInfo.Type)
}

func needUpdateMountPoint(bd *diskv1.BlockDevice, filesystem *block.FileSystemInfo) NeedMountUpdateOP {
	if filesystem == nil {
		logrus.Debugf("Filesystem is not ready, skip the mount operation")
		return NeedMountUpdateNoOp
	}

	provisioned := bd.Spec.FileSystem.Provisioned || bd.Spec.Provision
	logrus.Debugf("Checking mount operation with FS.Provisioned %v, FS.Mountpoint %s", provisioned, filesystem.MountPoint)
	if provisioned {
		if filesystem.MountPoint == "" {
			return NeedMountUpdateMount
		}
		if filesystem.MountPoint == extraDiskMountPoint(bd) {
			logrus.Debugf("Already mounted, return no-op")
			return NeedMountUpdateNoOp
		}
		return NeedMountUpdateUnmount | NeedMountUpdateMount
	}
	if filesystem.MountPoint != "" {
		return NeedMountUpdateUnmount
	}
	return NeedMountUpdateNoOp
}
