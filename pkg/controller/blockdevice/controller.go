package blockdevice

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	lhtypes "github.com/longhorn/longhorn-manager/types"
	lhutil "github.com/longhorn/longhorn-manager/util"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

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
	blockDeviceHandlerName = "harvester-block-device-handler"
	defaultRescanInterval  = 30 * time.Second
	enqueueDelay           = 10 * time.Second
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
	logrus.Debugf("Scan block devices of node: %s", c.NodeName)
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
			logrus.Infof("Skip adding non-identifiable block device /dev/%s", disk.Name)
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

	oldBdList, err := c.Blockdevices.List(c.Namespace, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", corev1.LabelHostname, c.NodeName),
	})
	if err != nil {
		return err
	}

	oldBds := convertBlockDeviceListToMap(oldBdList)
	for _, bd := range newBds {
		if _, ok := oldBds[bd.Name]; ok {
			// remove blockdevice from old device so we can delete missing devices afterward
			delete(oldBds, bd.Name)
		} else {
			// persist newly detected block device
			if _, err := c.SaveBlockDevice(bd, autoProvisionedMap[bd.Name]); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	// This oldBds are leftover after running SaveBlockDevice.
	// Clean up all previous registered block devices.
	for _, oldBd := range oldBds {
		if err := c.Blockdevices.Delete(oldBd.Namespace, oldBd.Name, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
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

	deviceCpy := device.DeepCopy()
	devPath, err := resolvePersistentDevPath(device)
	if err != nil {
		return nil, err
	}
	if devPath == "" {
		return nil, fmt.Errorf("failed to resolve persistent dev path for block device %s", device.Name)
	}
	filesystem := c.BlockInfo.GetFileSystemInfoByDevPath(devPath)

	needFormat := deviceCpy.Spec.FileSystem.ForceFormatted && deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt == nil
	if needFormat {
		err := c.forceFormat(deviceCpy, devPath, filesystem)
		if err != nil {
			err := fmt.Errorf("failed to force format device %s: %s", device.Name, err.Error())
			logrus.Error(err)
			diskv1.DeviceFormatting.SetError(deviceCpy, "", err)
			diskv1.DeviceFormatting.SetStatusBool(deviceCpy, false)
		}
		if !reflect.DeepEqual(device, deviceCpy) {
			return c.Blockdevices.Update(deviceCpy)
		}
		return device, err
	}

	// mount device by path, and skip mount partitioned device
	needUpdateMount := filesystem != nil && filesystem.MountPoint != deviceCpy.Spec.FileSystem.MountPoint
	if needUpdateMount {
		err := c.updateDeviceMount(deviceCpy, devPath, filesystem)
		if err != nil {
			err := fmt.Errorf("failed to update device mount %s: %s", device.Name, err.Error())
			logrus.Error(err)
			diskv1.DeviceMounted.SetError(deviceCpy, "", err)
			diskv1.DeviceMounted.SetStatusBool(deviceCpy, false)
		}
		if !reflect.DeepEqual(device, deviceCpy) {
			return c.Blockdevices.Update(deviceCpy)
		}
		return device, err
	}

	needProvision := deviceCpy.Spec.FileSystem.MountPoint != "" && deviceCpy.Spec.FileSystem.Provisioned
	switch {
	case needProvision && device.Status.ProvisionPhase == diskv1.ProvisionPhaseUnprovisioned:
		if err := c.provisionDeviceToNode(deviceCpy, devPath); err != nil {
			err := fmt.Errorf("failed to provision device %s to node %s on path %s: %w", device.Name, c.NodeName, device.Spec.FileSystem.MountPoint, err)
			logrus.Error(err)
			diskv1.DiskAddedToNode.SetError(deviceCpy, "", err)
			diskv1.DiskAddedToNode.SetStatusBool(deviceCpy, false)
			c.Blockdevices.EnqueueAfter(c.Namespace, device.Name, enqueueDelay)
		}
	case !needProvision && device.Status.ProvisionPhase != diskv1.ProvisionPhaseUnprovisioned:
		if err := c.unprovisionDeviceFromNode(deviceCpy); err != nil {
			err := fmt.Errorf("failed to stop provisioning device %s to node %s on path %s: %w", device.Name, c.NodeName, device.Spec.FileSystem.MountPoint, err)
			logrus.Error(err)
			diskv1.DiskAddedToNode.SetError(deviceCpy, "", err)
			diskv1.DiskAddedToNode.SetStatusBool(deviceCpy, false)
			c.Blockdevices.EnqueueAfter(c.Namespace, device.Name, enqueueDelay)
		}
	}

	if !reflect.DeepEqual(device, deviceCpy) {
		return c.Blockdevices.Update(deviceCpy)
	}

	// None of the above operations have resulted in an update to the device.
	// We therefore try to update the latest device status from the OS
	if err := c.updateDeviceStatus(deviceCpy, devPath); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(device, deviceCpy) {
		return c.Blockdevices.Update(deviceCpy)
	}

	return nil, nil
}

func convertBlockDeviceListToMap(bdList *diskv1.BlockDeviceList) map[string]*diskv1.BlockDevice {
	bdMap := make(map[string]*diskv1.BlockDevice, len(bdList.Items))
	for _, bd := range bdList.Items {
		bd := bd
		bdMap[bd.Name] = &bd
	}
	return bdMap
}

func (c *Controller) updateDeviceMount(device *diskv1.BlockDevice, devPath string, filesystem *block.FileSystemInfo) error {
	expectedMountPoint := device.Spec.FileSystem.MountPoint
	if expectedMountPoint != "" && device.Status.DeviceStatus.Partitioned {
		return fmt.Errorf("cannot mount device with partitions")
	}
	// umount the previous path if exist
	if filesystem != nil && filesystem.MountPoint != "" {
		logrus.Infof("Unmount device %s from path %s", device.Name, filesystem.MountPoint)
		if err := disk.UmountDisk(filesystem.MountPoint); err != nil {
			return err
		}
		diskv1.DeviceMounted.SetError(device, "", nil)
		diskv1.DeviceMounted.SetStatusBool(device, false)
	}

	if expectedMountPoint != "" {
		logrus.Debugf("Mount deivce %s to %s", device.Name, expectedMountPoint)
		if err := disk.MountDisk(devPath, expectedMountPoint); err != nil {
			return err
		}
		diskv1.DeviceMounted.SetError(device, "", nil)
		diskv1.DeviceMounted.SetStatusBool(device, true)
	}

	return c.updateDeviceFileSystem(device, devPath)
}

func (c *Controller) updateDeviceFileSystem(device *diskv1.BlockDevice, devPath string) error {
	filesystem := c.BlockInfo.GetFileSystemInfoByDevPath(devPath)
	if filesystem == nil {
		return fmt.Errorf("failed to get filesystem info from devPath %s", devPath)
	}
	if filesystem.MountPoint != "" && filesystem.Type != "" && !lhutil.IsSupportedFileSystem(filesystem.Type) {
		return fmt.Errorf("unsupported filesystem type %s", filesystem.Type)
	}

	device.Status.DeviceStatus.FileSystem.MountPoint = filesystem.MountPoint
	device.Status.DeviceStatus.FileSystem.Type = filesystem.Type
	device.Status.DeviceStatus.FileSystem.IsReadOnly = filesystem.IsReadOnly
	return nil
}

// forceFormat simply formats the device to ext4 filesystem
//
// - umount the block device if it is mounted
// - create ext4 filesystem on the block device
func (c *Controller) forceFormat(device *diskv1.BlockDevice, devPath string, filesystem *block.FileSystemInfo) error {
	// umount the disk if it is mounted
	if filesystem != nil && filesystem.MountPoint != "" {
		logrus.Infof("unmount %s for %s", filesystem.MountPoint, device.Name)
		if err := disk.UmountDisk(filesystem.MountPoint); err != nil {
			return err
		}
	}

	// make ext4 filesystem format of the partition disk
	logrus.Debugf("make ext4 filesystem format of device %s", device.Name)
	if err := disk.MakeExt4DiskFormatting(devPath); err != nil {
		return err
	}
	if err := c.updateDeviceFileSystem(device, devPath); err != nil {
		return err
	}
	diskv1.DeviceFormatting.SetError(device, "", nil)
	diskv1.DeviceFormatting.SetStatusBool(device, false)
	diskv1.DeviceFormatting.Message(device, "Done device ext4 filesystem formatting")
	device.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
	return nil
}

// provisionDeviceToNode adds a device to longhorn node as an additional disk.
func (c *Controller) provisionDeviceToNode(device *diskv1.BlockDevice, devPath string) error {
	filesystem := c.BlockInfo.GetFileSystemInfoByDevPath(devPath)
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
		logrus.Infof("disk %s not in disks of longhorn node %s/%s", device.Name, c.Namespace, c.NodeName)
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

// SaveBlockDevice persists the blockedevice information.
func (c *Controller) SaveBlockDevice(bd *diskv1.BlockDevice, autoProvisioned bool) (*diskv1.BlockDevice, error) {
	if autoProvisioned {
		bd.Spec.FileSystem.ForceFormatted = true
		bd.Spec.FileSystem.Provisioned = true
		bd.Spec.FileSystem.MountPoint = fmt.Sprintf("/var/lib/harvester/extra-disks/%s", bd.Name)
	}
	logrus.Infof("Add new block device %s with device: %s", bd.Name, bd.Spec.DevPath)
	return c.Blockdevices.Create(bd)
}

func (c *Controller) updateDeviceStatus(device *diskv1.BlockDevice, devPath string) error {
	var newStatus diskv1.DeviceStatus
	var needAutoProvision bool

	switch device.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		disk := c.BlockInfo.GetDiskByDevPath(devPath)
		bd := GetDiskBlockDevice(disk, c.NodeName, c.Namespace)
		newStatus = bd.Status.DeviceStatus
		// Only disk can be auto-provisioned.
		needAutoProvision = c.ApplyAutoProvisionFiltersForDisk(disk)
	case diskv1.DeviceTypePart:
		parentDevPath, err := block.GetParentDevName(devPath)
		if err != nil {
			return fmt.Errorf("failed to get parent devPath for %s: %v", device.Name, err)
		}
		part := c.BlockInfo.GetPartitionByDevPath(parentDevPath, devPath)
		bd := GetPartitionBlockDevice(part, c.NodeName, c.Namespace)
		newStatus = bd.Status.DeviceStatus
	default:
		return fmt.Errorf("unknown device type %s", device.Status.DeviceStatus.Details.DeviceType)
	}

	oldStatus := device.Status.DeviceStatus
	lastFormatted := oldStatus.FileSystem.LastFormattedAt
	if lastFormatted != nil && newStatus.FileSystem.LastFormattedAt == nil {
		newStatus.FileSystem.LastFormattedAt = lastFormatted
	}

	if !reflect.DeepEqual(oldStatus, newStatus) {
		logrus.Infof("Update existing block device status %s", device.Name)
		device.Status.DeviceStatus = newStatus
		if needAutoProvision && lastFormatted == nil {
			device.Spec.FileSystem.ForceFormatted = true
			device.Spec.FileSystem.Provisioned = true
			device.Spec.FileSystem.MountPoint = fmt.Sprintf("/var/lib/harvester/extra-disks/%s", device.Name)
		}
	}
	return nil
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

func resolvePersistentDevPath(device *diskv1.BlockDevice) (string, error) {
	switch device.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		wwn := device.Status.DeviceStatus.Details.WWN
		if wwn == "" {
			return "", fmt.Errorf("WWN not found on device %s", device.Name)
		}
		if device.Status.DeviceStatus.Details.StorageController == string(diskv1.StorageControllerNVMe) {
			return filepath.EvalSymlinks("/dev/disk/by-id/nvme-" + wwn)
		}
		return filepath.EvalSymlinks("/dev/disk/by-id/wwn-" + wwn)
	case diskv1.DeviceTypePart:
		partUUID := device.Status.DeviceStatus.Details.PartUUID
		if partUUID == "" {
			return "", fmt.Errorf("PARTUUID not found on device %s", device.Name)
		}
		return filepath.EvalSymlinks("/dev/disk/by-partuuid/" + partUUID)
	default:
		return "", nil
	}
}
