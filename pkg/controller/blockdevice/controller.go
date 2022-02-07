package blockdevice

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	lhtypes "github.com/longhorn/longhorn-manager/types"
	lhutil "github.com/longhorn/longhorn-manager/util"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/disk"
	"github.com/harvester/node-disk-manager/pkg/filter"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	blockDeviceHandlerName  = "harvester-block-device-handler"
	defaultRescanInterval   = 30 * time.Second
	forceFormatPollInterval = 3 * time.Second
	forceFormatPollTimeout  = 30 * time.Second
	enqueueDelay            = 10 * time.Second
)

type Controller struct {
	Namespace string
	NodeName  string

	NodeCache ctllonghornv1.NodeCache
	Nodes     ctllonghornv1.NodeClient

	Blockdevices     ctldiskv1.BlockDeviceController
	BlockdeviceCache ctldiskv1.BlockDeviceCache
	BlockInfo        block.Info
	ExcludeFilters   []*filter.Filter

	AutoProvisionFilters []*filter.Filter
}

// Register register the block device CRD controller
func Register(
	ctx context.Context,
	nodes ctllonghornv1.NodeController,
	bds ctldiskv1.BlockDeviceController,
	block block.Info,
	opt *option.Option,
	excludeFilters []*filter.Filter,
	autoProvisionFilters []*filter.Filter,
) error {
	controller := &Controller{
		Namespace:            opt.Namespace,
		NodeName:             opt.NodeName,
		NodeCache:            nodes.Cache(),
		Nodes:                nodes,
		Blockdevices:         bds,
		BlockdeviceCache:     bds.Cache(),
		BlockInfo:            block,
		ExcludeFilters:       excludeFilters,
		AutoProvisionFilters: autoProvisionFilters,
	}

	if err := controller.ScanBlockDevicesOnNode(); err != nil {
		return err
	}

	// Rescan devices on the node periodically.
	rescanInterval := defaultRescanInterval
	if opt.RescanInterval > 0 {
		rescanInterval = time.Duration(opt.RescanInterval) * time.Second
	}
	go func() {
		ticker := time.NewTicker(rescanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := controller.ScanBlockDevicesOnNode(); err != nil {
					logrus.Errorf("Failed to rescan block devices on node %s: %v", controller.NodeName, err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	bds.OnChange(ctx, blockDeviceHandlerName, controller.OnBlockDeviceChange)
	bds.OnRemove(ctx, blockDeviceHandlerName, controller.OnBlockDeviceDelete)
	return nil
}

// ScanBlockDevicesOnNode scans block devices on the node, and it will either create or update them.
func (c *Controller) ScanBlockDevicesOnNode() error {
	logrus.Infof("Scan block devices of node: %s", c.NodeName)
	newBds := make([]*diskv1.BlockDevice, 0)

	autoProvisionedMap := make(map[string]bool, 0)

	// list all the block devices
	for _, disk := range c.BlockInfo.GetDisks() {
		// ignore block device by filters
		if c.ApplyExcludeFiltersForDisk(disk) {
			continue
		}

		logrus.Debugf("Found a disk block device /dev/%s", disk.Name)

		bd := GetDiskBlockDevice(disk, c.NodeName, c.Namespace)

		if bd == nil {
			logrus.Infof("Skip adding non-identifiable block device %s", bd.Spec.DevPath)
			continue
		}

		if c.ApplyAutoProvisionFiltersForDisk(disk) {
			autoProvisionedMap[bd.Name] = true
		}

		newBds = append(newBds, bd)

		for _, part := range disk.Partitions {
			// ignore block device by filters
			if c.ApplyExcludeFiltersForPartition(part) {
				continue
			}
			logrus.Debugf("Found a partition block device /dev/%s", part.Name)
			bd := GetPartitionBlockDevice(part, c.NodeName, c.Namespace)
			if bd.Name == "" {
				logrus.Infof("Skip adding non-identifiable block device %s", bd.Spec.DevPath)
				continue
			}
			newBds = append(newBds, bd)
		}
	}

	oldBdList, err := c.Blockdevices.List(c.Namespace, v1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", corev1.LabelHostname, c.NodeName),
	})
	if err != nil {
		return err
	}

	oldBds := convertBlockDeviceListToMap(oldBdList)

	// either create or update the block device
	for _, bd := range newBds {
		bd, err := c.SaveBlockDevice(bd, oldBds, autoProvisionedMap[bd.Name])
		if err != nil {
			return err
		}
		// remove blockdevice from old device so we can delete missing devices afterward
		delete(oldBds, bd.Name)
	}

	// This oldBds are leftover after running SaveBlockDevice.
	// Clean up all previous registered block devices.
	for _, oldBd := range oldBds {
		if err := c.Blockdevices.Delete(oldBd.Namespace, oldBd.Name, &metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// OnBlockDeviceChange watch the block device CR on change and performing disk operations
// like mounting the disks to a desired path via ext4
func (c *Controller) OnBlockDeviceChange(key string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device == nil || device.DeletionTimestamp != nil || device.Spec.NodeName != c.NodeName {
		return nil, nil
	}

	var shouldEnqueue bool

	deviceCpy := device.DeepCopy()
	fs := *deviceCpy.Spec.FileSystem
	fsStatus := *deviceCpy.Status.DeviceStatus.FileSystem

	if fs.ForceFormatted && fsStatus.LastFormattedAt == nil {
		// perform disk force-format operation
		updateFormattingErrorCondition := func(bd *diskv1.BlockDevice, err error) (*diskv1.BlockDevice, error) {
			error := fmt.Errorf("failed to force format the device %s, %s", device.Spec.DevPath, err.Error())
			logrus.Error(error)
			diskv1.DeviceFormatting.SetError(bd, "", error)
			diskv1.DeviceFormatting.SetStatusBool(bd, false)
			return c.Blockdevices.Update(bd)
		}

		switch device.Status.DeviceStatus.Details.DeviceType {
		case diskv1.DeviceTypeDisk:
			if newDevice, err := c.forceFormatDisk(deviceCpy); err != nil {
				return updateFormattingErrorCondition(deviceCpy, err)
			} else if newDevice.Name != deviceCpy.Name {
				// forceFormatDisk may return a new device with new GPT label
				// If in that case, just return instead of proceeding following update.
				return newDevice, nil
			}
		case diskv1.DeviceTypePart:
			if deviceCpy, err := c.forceFormatPartition(deviceCpy); err != nil {
				return updateFormattingErrorCondition(deviceCpy, err)
			}
		}
	}

	// mount device by path, and skip mount partitioned device
	if !deviceCpy.Status.DeviceStatus.Partitioned && fs.MountPoint != fsStatus.MountPoint {
		if err := updateDeviceMount(deviceCpy.Spec.DevPath, fs.MountPoint, fsStatus.MountPoint); err != nil {
			err := fmt.Errorf("failed to mount the device %s to path %s, error: %s", device.Spec.DevPath, fs.MountPoint, err.Error())
			logrus.Error(err)
			diskv1.DeviceMounted.SetError(deviceCpy, "", err)
			diskv1.DeviceMounted.SetStatusBool(deviceCpy, false)
			return c.Blockdevices.Update(deviceCpy)
		}
	}

	if err := c.updateFileSystemStatus(deviceCpy); err != nil {
		return device, err
	}

	if device.Status.DeviceStatus.Details.DeviceType == diskv1.DeviceTypePart {
		switch {
		case fs.MountPoint != "" && fs.Provisioned && device.Status.ProvisionPhase == diskv1.ProvisionPhaseUnprovisioned:
			if err := c.provisionDeviceToNode(deviceCpy); err != nil {
				err := fmt.Errorf("failed to provision device %s to node %s on path %s: %w", device.Name, c.NodeName, device.Spec.FileSystem.MountPoint, err)
				logrus.Error(err)
				diskv1.DiskAddedToNode.SetError(deviceCpy, "", err)
				diskv1.DiskAddedToNode.SetStatusBool(deviceCpy, false)
				shouldEnqueue = true
			}
		case fs.MountPoint == "" || !fs.Provisioned:
			if device.Status.ProvisionPhase != diskv1.ProvisionPhaseUnprovisioned {
				if err := c.unprovisionDeviceFromNode(deviceCpy); err != nil {
					err := fmt.Errorf("failed to stop provisioning device %s to node %s on path %s: %w", device.Name, c.NodeName, device.Spec.FileSystem.MountPoint, err)
					logrus.Error(err)
					diskv1.DiskAddedToNode.SetError(deviceCpy, "", err)
					diskv1.DiskAddedToNode.SetStatusBool(deviceCpy, false)
					shouldEnqueue = true
				}
			}
		}
	}

	if !reflect.DeepEqual(device, deviceCpy) {
		if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
			return device, err
		}
	}
	if shouldEnqueue {
		c.Blockdevices.EnqueueAfter(c.Namespace, device.Name, enqueueDelay)
	}

	return nil, nil
}

func convertBlockDeviceListToMap(bdList *diskv1.BlockDeviceList) map[string]*diskv1.BlockDevice {
	bds := make([]*diskv1.BlockDevice, 0, len(bdList.Items))
	for _, bd := range bdList.Items {
		bd := bd
		bds = append(bds, &bd)
	}
	return ConvertBlockDevicesToMap(bds)
}

// ConvertBlockDevicesToMap converts a BlockDeviceList to a map with GUID (Name) as keys.
func ConvertBlockDevicesToMap(bds []*diskv1.BlockDevice) map[string]*diskv1.BlockDevice {
	bdMap := make(map[string]*diskv1.BlockDevice, len(bds))
	for _, bd := range bds {
		bdMap[bd.Name] = bd
	}
	return bdMap
}

func (c *Controller) updateFileSystemStatus(device *diskv1.BlockDevice) error {
	// fetch the latest device filesystem info
	uuid := device.Status.DeviceStatus.Details.UUID
	filesystem := c.BlockInfo.GetFileSystemInfoByFsUUID(uuid)
	mountPoint := device.Spec.FileSystem.MountPoint
	device.Status.DeviceStatus.FileSystem.Type = filesystem.Type
	device.Status.DeviceStatus.FileSystem.IsReadOnly = filesystem.IsReadOnly
	device.Status.DeviceStatus.FileSystem.MountPoint = filesystem.MountPoint

	if filesystem.MountPoint != "" && filesystem.Type != "" {
		err := isValidFileSystem(device.Spec.FileSystem, device.Status.DeviceStatus.FileSystem)
		mounted := err == nil && filesystem.MountPoint != ""
		diskv1.DeviceMounted.SetError(device, "", err)
		diskv1.DeviceMounted.SetStatusBool(device, mounted)
	} else if mountPoint != "" && device.Status.DeviceStatus.Partitioned {
		diskv1.DeviceMounted.SetError(device, "", fmt.Errorf("cannot mount parent device with partitions"))
		diskv1.DeviceMounted.SetStatusBool(device, false)
	} else if mountPoint == "" && mountPoint == filesystem.MountPoint {
		existingMount := device.Status.DeviceStatus.FileSystem.MountPoint
		if existingMount != "" {
			if err := disk.UmountDisk(existingMount); err != nil {
				return err
			}
		}
		diskv1.DeviceMounted.SetError(device, "", nil)
		diskv1.DeviceMounted.SetStatusBool(device, false)
	}

	return nil
}

func updateDeviceMount(devPath, mountPoint, existingMount string) error {
	// umount the previous path if exist
	if existingMount != "" {
		logrus.Debugf("start unmounting dev %s from path %s", devPath, mountPoint)
		if err := disk.UmountDisk(existingMount); err != nil {
			return err
		}
	}

	if mountPoint != "" {
		logrus.Debugf("start mounting disk %s to %s", devPath, mountPoint)
		if err := disk.MountDisk(devPath, mountPoint); err != nil {
			return err
		}
	}
	return nil
}

// forceFormatPartition simply formats the partition to ext4 filesystem
//
// - umount the block device if it is mounted
// - create ext4 filesystem formatting of the single partition
func (c *Controller) forceFormatPartition(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device.Status.DeviceStatus.Details.DeviceType != diskv1.DeviceTypePart {
		return device, fmt.Errorf("device %s is not a partition", device.Name)
	}

	filesystem := device.Spec.FileSystem
	fsStatus := device.Status.DeviceStatus.FileSystem
	logrus.Infof("performing format operation of disk %s, mount path %s", device.Spec.DevPath, filesystem.MountPoint)

	// umount the disk if it is mounted
	if fsStatus.MountPoint != "" {
		if err := disk.UmountDisk(fsStatus.MountPoint); err != nil {
			return device, err
		}
	}

	// make ext4 filesystem format of the partition disk
	logrus.Debugf("make ext4 filesystem format of disk %s", device.Spec.DevPath)
	if err := disk.MakeExt4DiskFormatting(device.Spec.DevPath); err != nil {
		return device, err
	}
	diskv1.DeviceFormatting.SetError(device, "", nil)
	diskv1.DeviceFormatting.SetStatusBool(device, false)
	diskv1.DeviceFormatting.Message(device, "Done device ext4 filesystem formatting")
	device.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
	return device, nil
}

// forceFormatDisk will be called when the user chooses to force formatting the
// block device, the block device can only be force-formatted once only. And the
// controller may reconcile this method multiple times if the process is failed.
//
// - umount the block device if it is mounted
// - create a GUID partition table with a single linux partition of the block device
// - if needed, remove old raw block device and create a new one
func (c *Controller) forceFormatDisk(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device.Status.DeviceStatus.Details.DeviceType != diskv1.DeviceTypeDisk {
		return device, fmt.Errorf("device %s is not a disk", device.Name)
	}

	filesystem := device.Spec.FileSystem
	fsStatus := device.Status.DeviceStatus.FileSystem
	logrus.Infof("performing format operation of disk %s, mount path %s", device.Spec.DevPath, filesystem.MountPoint)

	// umount the disk if it is mounted
	if fsStatus.MountPoint != "" {
		if err := disk.UmountDisk(fsStatus.MountPoint); err != nil {
			return device, err
		}
	}

	// create a GUID partition table with a single partition only
	if err := disk.MakeGPTPartition(device.Spec.DevPath); err != nil {
		return device, err
	}

	// Polling for newly added partition block device (from udev monitoring)
	var bd *diskv1.BlockDevice
	poll := func() (bool, error) {
		var err error
		devPath := util.GetDiskPartitionPath(device.Spec.DevPath, 1)
		part := c.BlockInfo.GetPartitionByDevPath(device.Spec.DevPath, devPath)
		name := block.GeneratePartitionGUID(part, c.NodeName)
		bd, err = c.BlockdeviceCache.Get(device.Namespace, name)
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		logrus.Debugf("polling for single partition %s, found: %t", devPath, bd != nil)
		return bd != nil, nil
	}
	if err := wait.PollImmediate(forceFormatPollInterval, forceFormatPollTimeout, poll); err != nil {
		return device, err
	}

	// Update the single partition block device to start force formatting
	toUpdate := bd.DeepCopy()
	toUpdate.Spec.FileSystem.Provisioned = filesystem.Provisioned
	toUpdate.Spec.FileSystem.MountPoint = filesystem.MountPoint
	toUpdate.Spec.FileSystem.ForceFormatted = true
	diskv1.DeviceFormatting.SetStatusBool(toUpdate, true)
	diskv1.DeviceFormatting.Message(toUpdate, fmt.Sprintf("formatting disk partition %s with ext4 filesystem", toUpdate.Spec.DevPath))
	if _, err := c.Blockdevices.Update(toUpdate); err != nil {
		return device, err
	}

	// if needed, remove old raw block device and create a new one
	blockDisk := c.BlockInfo.GetDiskByDevPath(device.Spec.DevPath)
	newDiskDevice := GetDiskBlockDevice(blockDisk, c.NodeName, c.Namespace)
	if newDiskDevice.Name == device.Name {
		diskv1.DeviceFormatting.SetError(device, "", nil)
		diskv1.DeviceFormatting.SetStatusBool(device, false)
		device.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
		device.Spec.FileSystem.MountPoint = ""
		device.Spec.FileSystem.Provisioned = false
		device.Status.DeviceStatus.Partitioned = true
		return device, nil
	}

	if err := c.Blockdevices.Delete(c.Namespace, device.Name, &metav1.DeleteOptions{}); err != nil {
		return device, err
	}
	diskv1.DeviceFormatting.SetError(newDiskDevice, "", nil)
	diskv1.DeviceFormatting.SetStatusBool(newDiskDevice, false)
	newDiskDevice.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
	newDiskDevice.Spec.FileSystem.ForceFormatted = true
	return c.Blockdevices.Create(newDiskDevice)
}

// provisionDeviceToNode adds a device to longhorn node as an additional disk.
func (c *Controller) provisionDeviceToNode(device *diskv1.BlockDevice) error {
	uuid := device.Status.DeviceStatus.Details.UUID
	filesystem := c.BlockInfo.GetFileSystemInfoByFsUUID(uuid)
	if filesystem == nil || filesystem.MountPoint == "" {
		// No mount point. Skipping...
		return nil
	}

	node, err := c.Nodes.Get(c.Namespace, c.NodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	updateDeviceStatus := func() {
		msg := fmt.Sprintf("Added disk %s to longhorn node `%s` as an additional disk", device.Name, node.Name)
		device.Status.ProvisionPhase = diskv1.ProvisionPhaseProvisioned
		diskv1.DiskAddedToNode.SetError(device, "", nil)
		diskv1.DiskAddedToNode.SetStatusBool(device, true)
		diskv1.DiskAddedToNode.Message(device, msg)
	}

	mountPoint := device.Spec.FileSystem.MountPoint
	if disk, ok := node.Spec.Disks[device.Name]; ok && disk.Path == mountPoint {
		// Device exists and with the same mount point. No need to update the node.
		updateDeviceStatus()
		return nil
	}

	nodeCpy := node.DeepCopy()
	diskSpec := lhtypes.DiskSpec{
		Path:              mountPoint,
		AllowScheduling:   true,
		EvictionRequested: false,
		StorageReserved:   0,
		Tags:              []string{},
	}
	nodeCpy.Spec.Disks[device.Name] = diskSpec
	if _, err = c.Nodes.Update(nodeCpy); err != nil {
		return err
	}

	updateDeviceStatus()
	return nil
}

// unprovisionDeviceFromNode removes a device from a longhorn node.
func (c *Controller) unprovisionDeviceFromNode(device *diskv1.BlockDevice) error {
	node, err := c.Nodes.Get(c.Namespace, c.NodeName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Skip since the node is not there.
			return nil
		}
		return err
	}

	updateProvisionPhaseUnprovisioned := func() {
		msg := fmt.Sprintf("Disk not in longhorn node `%s`", c.NodeName)
		// To prevent user from mistaking unprovisioning from umount, NDM umount
		// for the device as well while unprovisioning it.
		device.Spec.FileSystem.MountPoint = ""
		device.Status.ProvisionPhase = diskv1.ProvisionPhaseUnprovisioned
		diskv1.DiskAddedToNode.SetError(device, "", nil)
		diskv1.DiskAddedToNode.SetStatusBool(device, false)
		diskv1.DiskAddedToNode.Message(device, msg)
	}

	diskToRemove, ok := node.Spec.Disks[device.Name]
	if !ok {
		logrus.Errorf("disk %s not in disks of longhorn node %s/%s", device.Name, c.Namespace, c.NodeName)
		updateProvisionPhaseUnprovisioned()
		return nil
	}

	isUnprovisioning := false
	for _, tag := range diskToRemove.Tags {
		if tag == util.DiskRemoveTag {
			isUnprovisioning = true
			break
		}
	}

	if isUnprovisioning {
		if status, ok := node.Status.DiskStatus[device.Name]; ok && len(status.ScheduledReplica) == 0 {
			// Unprovision finished. Remove the disk.
			nodeCpy := node.DeepCopy()
			delete(nodeCpy.Spec.Disks, device.Name)
			if _, err := c.Nodes.Update(nodeCpy); err != nil {
				return err
			}
			updateProvisionPhaseUnprovisioned()
			logrus.Debugf("device %s is unprovisioned", device.Name)
		} else {
			// Still unprovisioning
			c.Blockdevices.EnqueueAfter(c.Namespace, device.Name, enqueueDelay)
			logrus.Debugf("device %s is unprovisioning", device.Name)
		}
	} else {
		// Start unprovisioing
		diskToRemove.AllowScheduling = false
		diskToRemove.EvictionRequested = true
		diskToRemove.Tags = append(diskToRemove.Tags, util.DiskRemoveTag)
		nodeCpy := node.DeepCopy()
		nodeCpy.Spec.Disks[device.Name] = diskToRemove
		if _, err := c.Nodes.Update(nodeCpy); err != nil {
			return err
		}
		msg := fmt.Sprintf("Stop provisioning device %s to longhorn node `%s`", device.Name, c.NodeName)
		device.Status.ProvisionPhase = diskv1.ProvisionPhaseUnprovisioning
		diskv1.DiskAddedToNode.SetError(device, "", nil)
		diskv1.DiskAddedToNode.SetStatusBool(device, false)
		diskv1.DiskAddedToNode.Message(device, msg)
	}

	return nil
}

func isValidFileSystem(fs *diskv1.FilesystemInfo, fsStatus *diskv1.FilesystemStatus) error {
	if len(fs.MountPoint) > 1 {
		fs.MountPoint = strings.TrimSuffix(fs.MountPoint, "/")
	}

	if fs.MountPoint != fsStatus.MountPoint {
		return fmt.Errorf("current mountPoint %s does not match the specified path: %s", fsStatus.MountPoint, fs.MountPoint)
	}

	if !lhutil.IsSupportedFileSystem(fsStatus.Type) {
		return fmt.Errorf("unsupported filesystem type %s", fsStatus.Type)
	}

	return nil
}

// SaveBlockDevice persists the blockedevice information. If oldBds contains a
// blockedevice under the same name (GUID), it will only do an update, otherwise
// create a new one.
//
// Note that this method also activate the device if it's previously inactive.
func (c *Controller) SaveBlockDevice(
	bd *diskv1.BlockDevice,
	oldBds map[string]*diskv1.BlockDevice,
	autoProvisioned bool,
) (*diskv1.BlockDevice, error) {
	provision := func(bd *diskv1.BlockDevice) {
		bd.Spec.FileSystem.ForceFormatted = true
		bd.Spec.FileSystem.Provisioned = true
		bd.Spec.FileSystem.MountPoint = fmt.Sprintf("/var/lib/harvester/extra-disks/%s", bd.Name)
	}

	if oldBd, ok := oldBds[bd.Name]; ok {
		newStatus := bd.Status.DeviceStatus
		oldStatus := oldBd.Status.DeviceStatus
		lastFormatted := oldStatus.FileSystem.LastFormattedAt
		if lastFormatted != nil && newStatus.FileSystem.LastFormattedAt == nil {
			newStatus.FileSystem.LastFormattedAt = lastFormatted
		}

		// Only disk hasn't yet been formatted can be auto-provisioned.
		autoProvisioned = autoProvisioned && lastFormatted == nil

		if autoProvisioned || !reflect.DeepEqual(oldStatus, newStatus) || oldBd.Status.State != diskv1.BlockDeviceActive {
			logrus.Infof("Update existing block device status %s with devPath: %s", oldBd.Name, oldBd.Spec.DevPath)
			toUpdate := oldBd.DeepCopy()
			toUpdate.Status.State = diskv1.BlockDeviceActive
			toUpdate.Status.DeviceStatus = newStatus
			if autoProvisioned {
				provision(toUpdate)
			}
			return c.Blockdevices.Update(toUpdate)
		}
		return oldBd, nil
	}

	if autoProvisioned {
		provision(bd)
	}
	logrus.Infof("Add new block device %s with device: %s", bd.Name, bd.Spec.DevPath)
	return c.Blockdevices.Create(bd)
}

// OnBlockDeviceDelete will delete the block devices that belongs to the same parent device
func (c *Controller) OnBlockDeviceDelete(key string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device == nil {
		return nil, nil
	}

	bds, err := c.BlockdeviceCache.List(c.Namespace, labels.SelectorFromSet(map[string]string{
		corev1.LabelHostname: c.NodeName,
		ParentDeviceLabel:    device.Name,
	}))
	if err != nil {
		return device, err
	}

	if len(bds) == 0 {
		return nil, nil
	}

	// Remove dangling blockdevice partitions
	for _, bd := range bds {
		if err := c.Blockdevices.Delete(c.Namespace, bd.Name, &metav1.DeleteOptions{}); err != nil {
			return device, err
		}
	}

	// Clean disk from related longhorn node
	node, err := c.Nodes.Get(c.Namespace, c.NodeName, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return device, err
	}
	if node == nil {
		logrus.Debugf("node %s is not there. Skip disk deletion from node", c.NodeName)
		return nil, nil
	}
	nodeCpy := node.DeepCopy()
	for _, bd := range bds {
		if _, ok := nodeCpy.Spec.Disks[bd.Name]; !ok {
			logrus.Debugf("disk %s not found in disks of longhorn node %s/%s", bd.Name, c.Namespace, c.NodeName)
			continue
		}
		existingMount := bd.Status.DeviceStatus.FileSystem.MountPoint
		if existingMount != "" {
			if err := disk.UmountDisk(existingMount); err != nil {
				logrus.Warnf("cannot umount disk %s from mount point %s, err: %s", bd.Name, existingMount, err.Error())
			}
		}
		delete(nodeCpy.Spec.Disks, bd.Name)
	}
	if _, err := c.Nodes.Update(nodeCpy); err != nil {
		return device, err
	}

	return nil, nil
}

// ApplyExcludeFiltersForPartition check the status of disk for every
// registered exclude filters. If the disk meets one of the criteria, it
// returns true.
func (c *Controller) ApplyExcludeFiltersForDisk(disk *block.Disk) bool {
	for _, filter := range c.ExcludeFilters {
		if filter.ApplyDiskFilter(disk) {
			logrus.Debugf("block device /dev/%s ignored by %s", disk.Name, filter.Name)
			return true
		}
	}
	return false
}

// ApplyExcludeFiltersForPartition check the status of partition for every
// registered exclude filters. If the partition meets one of the criteria, it
// returns true.
func (c *Controller) ApplyExcludeFiltersForPartition(part *block.Partition) bool {
	for _, filter := range c.ExcludeFilters {
		if filter.ApplyPartFilter(part) {
			logrus.Debugf("block device /dev/%s ignored by %s", part.Name, filter.Name)
			return true
		}
	}
	return false
}

// ApplyAutoProvisionFiltersForDisk check the status of disk for every
// registered auto-provision filters. If the disk meets one of the criteria, it
// returns true.
func (c *Controller) ApplyAutoProvisionFiltersForDisk(disk *block.Disk) bool {
	for _, filter := range c.AutoProvisionFilters {
		if filter.ApplyDiskFilter(disk) {
			logrus.Debugf("block device /dev/%s is promoted to auto-provision by %s", disk.Name, filter.Name)
			return true
		}
	}
	return false
}
