package blockdevice

import (
	"context"
	"fmt"
	"reflect"
	"time"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	lhtypes "github.com/longhorn/longhorn-manager/types"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	defaultRescanInterval  = 60 * time.Second
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
		if bd.ObjectMeta.Name == "" {
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

func (c *Controller) OnBlockDeviceChange(key string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device == nil || device.DeletionTimestamp != nil || device.Spec.NodeName != c.NodeName {
		return nil, nil
	}

	switch device.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		return c.onDiskChange(device)
	case diskv1.DeviceTypePart:
		return c.onPartitionChange(device)
	}

	return nil, nil
}

func (c *Controller) onDiskChange(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	fs := *device.Spec.FileSystem

	var err error

	if !fs.ForceFormatted {
		// Disk only cares about force formatting itself to a single root partition.
		return nil, nil
	}

	if diskv1.DevicePartitioning.IsTrue(device) {
		logrus.Infof("Await GPT partition for %s", device.Name)
		return nil, nil
	}

	if diskv1.DevicePartitioned.IsTrue(device) {
		logrus.Infof("Finsihed GPT partition for %s", device.Name)
		devPath := util.GetDiskPartitionPath(device.Spec.DevPath, 1)
		part := c.BlockInfo.GetPartitionByDevPath(device.Spec.DevPath, devPath)
		name := block.GeneratePartitionGUID(part, c.NodeName)
		bd, err := c.BlockdeviceCache.Get(device.Namespace, name)
		if err != nil {
			// TODO: Should consider not found??
			return nil, err
		}
		bdCpy := bd.DeepCopy()
		bdCpy.Spec.FileSystem.MountPoint = device.Spec.FileSystem.MountPoint
		bdCpy.Spec.FileSystem.ForceFormatted = true
		if reflect.DeepEqual(bdCpy.Spec.FileSystem, bd.Spec.FileSystem) {
			return nil, nil
		}
		return c.Blockdevices.Update(bdCpy)
	}

	logrus.Infof("Start GPT partition for %s", device.Name)
	deviceCpy := device.DeepCopy()
	setDevicePartitioningCondition(deviceCpy, corev1.ConditionTrue, "")
	device, err = c.Blockdevices.Update(deviceCpy)
	if err != nil {
		return nil, err
	}
	// TODO: unmount first?
	go c.makeGPTPartition(device)

	return nil, nil
}

func (c *Controller) onPartitionChange(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	fs := *device.Spec.FileSystem

	var err error

	if diskv1.DeviceFormatting.IsTrue(device) {
		logrus.Infof("Await partition formating for %s", device.Name)
		return nil, nil
	}
	if diskv1.DeviceMounting.IsTrue(device) {
		logrus.Infof("Await partition mounting for %s", device.Name)
		return nil, nil
	}
	if diskv1.DeviceUnmounting.IsTrue(device) {
		logrus.Infof("Await partition unmounting for %s", device.Name)
		return nil, nil
	}
	if diskv1.DeviceUnprovisioning.IsTrue(device) {
		logrus.Infof("Await partition unprovisioning for %s", device.Name)
		// May enqueue device if disk on longhorn node haven't yet evicted all replicas
		c.unprovisionDevice(device.DeepCopy())
		return nil, nil
	}

	if fs.ForceFormatted && !diskv1.DeviceFormatted.IsTrue(device) {
		logrus.Infof("Start formatting for %s", device.Name)
		deviceCpy := device.DeepCopy()
		setDeviceFormattingCondition(deviceCpy, corev1.ConditionTrue, "")
		device, err = c.Blockdevices.Update(deviceCpy)
		if err != nil {
			return nil, err
		}
		// TODO: umount first?
		go c.formatPartition(device)
		return nil, nil
	}

	filesystem := c.BlockInfo.GetFileSystemInfoByDevPath(device.Spec.DevPath)

	if fs.MountPoint != filesystem.MountPoint {
		deviceCpy := device.DeepCopy()
		if fs.MountPoint != "" {
			logrus.Infof("Start mounting %s on %s", device.Name, fs.MountPoint)
			setDeviceMountingCondition(deviceCpy, corev1.ConditionTrue, "")
			device, err = c.Blockdevices.Update(deviceCpy)
			if err != nil {
				return nil, err
			}
			go c.updateFilesystemMount(device, filesystem)
			return nil, nil
		}
		if diskv1.DeviceProvisioned.IsTrue(device) {
			logrus.Infof("Start unprovisioing %s from %s", device.Name, fs.MountPoint)
			setDeviceUnprovisioningCondition(deviceCpy, corev1.ConditionTrue, "")
			device, err = c.Blockdevices.Update(deviceCpy)
			if err != nil {
				return nil, err
			}
			err := c.unprovisionDevice(device.DeepCopy())
			return nil, err
		}

		logrus.Infof("Start unmounting %s on %s", device.Name, fs.MountPoint)
		setDeviceUnmountingCondition(deviceCpy, corev1.ConditionTrue, "")
		device, err = c.Blockdevices.Update(deviceCpy)
		if err != nil {
			return nil, err
		}
		go c.unmountFilesystem(device, filesystem)
		return nil, nil
	}

	if fs.MountPoint != "" && !diskv1.DeviceProvisioned.IsTrue(device) {
		deviceCpy := device.DeepCopy()
		logrus.Infof("Start provisioning %s on %s", device.Name, fs.MountPoint)
		if err := c.provisionDevice(device, filesystem); err != nil {
			return nil, err
		}
		setDeviceProvisionedCondition(deviceCpy, corev1.ConditionTrue, "")
		_, err = c.Blockdevices.Update(deviceCpy)
		if err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// Run as goroutiune
func (c *Controller) makeGPTPartition(device *diskv1.BlockDevice) {
	cmdErr := disk.MakeGPTPartition(device.Spec.DevPath)
	device, err := c.BlockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		// Backwards compatible for LastFormattedAt
		deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
		setDevicePartitioningCondition(deviceCpy, corev1.ConditionFalse, "")
		setDevicePartitionedCondition(deviceCpy, corev1.ConditionTrue, "")
	} else {
		setDevicePartitioningCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
		setDevicePartitionedCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
	}
}

func (c *Controller) formatPartition(device *diskv1.BlockDevice) {
	cmdErr := disk.MakeExt4DiskFormatting(device.Spec.DevPath)
	device, err := c.BlockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
		return
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		// Backwards compatible for LastFormattedAt
		deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
		setDeviceFormattingCondition(deviceCpy, corev1.ConditionFalse, "")
		setDeviceFormattedCondition(deviceCpy, corev1.ConditionTrue, "")
	} else {
		setDeviceFormattingCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
		setDeviceFormattedCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
		return
	}
}

func (c *Controller) updateFilesystemMount(device *diskv1.BlockDevice, fs *block.FileSystemInfo) {
	if fs.MountPoint == "" {
		c.mountFilesystem(device, fs)
	} else {
		c.unmountFilesystem(device, fs)
	}
}

func (c *Controller) unmountFilesystem(device *diskv1.BlockDevice, fs *block.FileSystemInfo) {
	cmdErr := disk.UmountDisk(device.Spec.FileSystem.MountPoint)
	device, err := c.BlockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
		return
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		setDeviceUnmountingCondition(deviceCpy, corev1.ConditionFalse, "")
		setDeviceMountedCondition(deviceCpy, corev1.ConditionFalse, "")
	} else {
		setDeviceUnmountingCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
		return
	}
}

func (c *Controller) mountFilesystem(device *diskv1.BlockDevice, fs *block.FileSystemInfo) {
	cmdErr := disk.MountDisk(device.Spec.DevPath, device.Spec.FileSystem.MountPoint)
	device, err := c.BlockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
		return
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		setDeviceMountingCondition(deviceCpy, corev1.ConditionFalse, "")
		setDeviceMountedCondition(deviceCpy, corev1.ConditionTrue, "")
	} else {
		setDeviceMountingCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
		setDeviceMountedCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
		return
	}
}

func (c *Controller) provisionDevice(device *diskv1.BlockDevice, filesystem *block.FileSystemInfo) error {
	node, err := c.getNode()
	if err != nil {
		return err
	}

	mountPoint := device.Spec.FileSystem.MountPoint
	if disk, ok := node.Spec.Disks[device.Name]; ok && disk.Path == mountPoint {
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

	return nil
}

func (c *Controller) unprovisionDevice(device *diskv1.BlockDevice) error {
	updateDeviceErrorCondition := func(device *diskv1.BlockDevice, err error) error {
		setDeviceUnprovisioningCondition(device, corev1.ConditionFalse, err.Error())
		_, err = c.Blockdevices.Update(device)
		return err
	}

	updateDeviceCondition := func(device *diskv1.BlockDevice) error {
		setDeviceProvisionedCondition(device, corev1.ConditionFalse, "")
		setDeviceUnprovisioningCondition(device, corev1.ConditionTrue, "")
		_, err := c.Blockdevices.Update(device)
		return err
	}

	node, err := c.getNode()
	if err != nil {
		if errors.IsNotFound(err) {
			// Skip since the node is not there.
			return updateDeviceCondition(device)
		}
		return updateDeviceErrorCondition(device, err)
	}

	diskToRemove, ok := node.Spec.Disks[device.Name]
	if !ok {
		logrus.Errorf("disk %s not in disks of longhorn node %s/%s", device.Name, c.Namespace, c.NodeName)
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
				return updateDeviceErrorCondition(device, err)
			}
			logrus.Debugf("device %s is unprovisioned", device.Name)
			return updateDeviceCondition(device)
		}
		// Still unprovisioning
		logrus.Debugf("device %s is unprovisioning", device.Name)
		c.Blockdevices.EnqueueAfter(device.Namespace, device.Name, enqueueDelay)
		return nil
	}

	// Start unprovisioing
	diskToRemove.AllowScheduling = false
	diskToRemove.EvictionRequested = true
	diskToRemove.Tags = append(diskToRemove.Tags, util.DiskRemoveTag)
	nodeCpy := node.DeepCopy()
	nodeCpy.Spec.Disks[device.Name] = diskToRemove
	if _, err := c.Nodes.Update(nodeCpy); err != nil {
		return updateDeviceErrorCondition(device, err)
	}
	return nil
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
		bd.Spec.FileSystem.MountPoint = fmt.Sprintf("/var/lib/harvester/extra-disks/%s", bd.Name)
	}

	if oldBd, ok := oldBds[bd.Name]; ok {
		newStatus := bd.Status.DeviceStatus
		oldStatus := oldBd.Status.DeviceStatus
		lastFormatted := oldStatus.FileSystem.LastFormattedAt

		// Only disk hasn't yet been formatted can be auto-provisioned.
		autoProvisioned = autoProvisioned && lastFormatted == nil && !diskv1.DeviceFormatted.IsTrue(oldBd)

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
	node, err := c.getNode()
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
		filesystem := c.BlockInfo.GetFileSystemInfoByDevPath(bd.Spec.DevPath)
		if filesystem.MountPoint != "" {
			if err := disk.UmountDisk(filesystem.MountPoint); err != nil {
				logrus.Warnf("cannot umount disk %s from mount point %s, err: %s", bd.Name, filesystem.MountPoint, err.Error())
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

func (c *Controller) getNode() (*longhornv1.Node, error) {
	node, err := c.NodeCache.Get(c.Namespace, c.NodeName)
	if err != nil && errors.IsNotFound(err) {
		node, err = c.Nodes.Get(c.Namespace, c.NodeName, metav1.GetOptions{})
	}
	return node, err
}
