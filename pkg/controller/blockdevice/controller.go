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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	diskv1 "github.com/longhorn/node-disk-manager/pkg/apis/longhorn.io/v1beta1"
	"github.com/longhorn/node-disk-manager/pkg/block"
	"github.com/longhorn/node-disk-manager/pkg/disk"
	"github.com/longhorn/node-disk-manager/pkg/filter"
	ctldiskv1 "github.com/longhorn/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/longhorn/node-disk-manager/pkg/option"
)

const (
	blockDeviceHandlerName = "longhorn-block-device-handler"
)

type Controller struct {
	namespace string
	nodeName  string

	nodeCache ctldiskv1.NodeCache
	nodes     ctldiskv1.NodeClient

	Blockdevices     ctldiskv1.BlockDeviceController
	BlockdeviceCache ctldiskv1.BlockDeviceCache
	BlockInfo        *block.Info
	Filters          []*filter.Filter
}

// Register register the block device CRD controller
func Register(ctx context.Context, nodes ctldiskv1.NodeController, bds ctldiskv1.BlockDeviceController, block *block.Info,
	opt *option.Option, filters []*filter.Filter) error {
	controller := &Controller{
		namespace:        opt.Namespace,
		nodeName:         opt.NodeName,
		nodeCache:        nodes.Cache(),
		nodes:            nodes,
		Blockdevices:     bds,
		BlockdeviceCache: bds.Cache(),
		BlockInfo:        block,
		Filters:          filters,
	}

	if err := controller.RegisterNodeBlockDevices(); err != nil {
		return err
	}

	bds.OnChange(ctx, blockDeviceHandlerName, controller.OnBlockDeviceChange)
	bds.OnRemove(ctx, blockDeviceHandlerName, controller.OnBlockDeviceDelete)
	return nil
}

// RegisterNodeBlockDevices will scan the block devices on the node, and it will either create or update the block device
func (c *Controller) RegisterNodeBlockDevices() error {
	logrus.Infof("Register block devices of node: %s", c.nodeName)
	newBds := make([]*diskv1.BlockDevice, 0)

	// list all the block devices
	for _, disk := range c.BlockInfo.Disks {
		// ignore block device by filters
		if c.ApplyFilter(disk) {
			continue
		}

		logrus.Infof("Found a block device /dev/%s", disk.Name)
		bd := DeviceInfoFromDisk(disk, c.nodeName, c.namespace)
		newBds = append(newBds, bd)

		for _, part := range disk.Partitions {
			bd := DeviceInfoFromPartition(part, c.nodeName, c.namespace)
			newBds = append(newBds, bd)
		}
	}

	oldBdList, err := c.Blockdevices.List(c.namespace, v1.ListOptions{})
	if err != nil {
		return err
	}

	oldBds := convertBlockDeviceListToMap(oldBdList)

	// either create or update the block device
	for _, bd := range newBds {
		if err := c.SaveBlockDevice(bd, oldBds); err != nil {
			return err
		}
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
	if device == nil || device.DeletionTimestamp != nil {
		return device, nil
	}

	deviceCpy := device.DeepCopy()
	fs := *deviceCpy.Spec.FileSystem
	fsStatus := *deviceCpy.Status.DeviceStatus.FileSystem

	if fs.ForceFormatted && fsStatus.LastFormattedAt == nil {
		// perform disk force-format operation
		if deviceCpy, err := c.forceFormatDisk(deviceCpy); err != nil {
			error := fmt.Errorf("failed to force format the device %s, %s", device.Spec.DevPath, err.Error())
			logrus.Error(error)
			diskv1.DeviceFormatting.SetError(deviceCpy, "", error)
			diskv1.DeviceFormatting.SetStatusBool(deviceCpy, true)
			return c.Blockdevices.Update(deviceCpy)
		}

		diskv1.DeviceFormatting.SetError(deviceCpy, "", nil)
		diskv1.DeviceFormatting.SetStatusBool(deviceCpy, false)
		deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
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

	if device.Status.DeviceStatus.Details.DeviceType == diskv1.DeviceTypePart && fs.MountPoint != "" {
		if deviceCpy, err := c.addDeviceToNode(deviceCpy); err != nil {
			err := fmt.Errorf("failed to add device %s to node %s on path %s", device.Name, c.nodeName, device.Spec.FileSystem.MountPoint)
			logrus.Error(err)
			diskv1.DiskAddedToNode.SetError(deviceCpy, "", err)
			diskv1.DiskAddedToNode.SetStatusBool(deviceCpy, false)
			diskv1.DiskAddedToNode.Message(deviceCpy, err.Error())
			return c.Blockdevices.Update(deviceCpy)
		}
	}

	if err := c.updateFileSystemStatus(deviceCpy); err != nil {
		return device, err
	}

	if !reflect.DeepEqual(device, deviceCpy) {
		if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
			return device, err
		}
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
	filesystem := c.BlockInfo.GetFileSystemInfoByDevPath(device.Spec.DevPath)
	fs := *device.Spec.FileSystem
	device.Status.DeviceStatus.FileSystem.Type = filesystem.Type
	device.Status.DeviceStatus.FileSystem.IsReadOnly = filesystem.IsReadOnly
	device.Status.DeviceStatus.FileSystem.MountPoint = fs.MountPoint

	if filesystem.MountPoint != "" && filesystem.Type != "" {
		err := isValidFileSystem(device.Spec.FileSystem, device.Status.DeviceStatus.FileSystem)
		mounted := err == nil && filesystem.MountPoint != ""
		diskv1.DeviceMounted.SetError(device, "", err)
		diskv1.DeviceMounted.SetStatusBool(device, mounted)
		if err != nil {
			diskv1.DeviceMounted.Message(device, err.Error())
		}
	} else if fs.MountPoint != "" && device.Status.DeviceStatus.Partitioned {
		diskv1.DeviceMounted.SetError(device, "", fmt.Errorf("cannot mount parent device with partitions"))
		diskv1.DeviceMounted.SetStatusBool(device, false)
	} else if fs.MountPoint == "" && fs.MountPoint == filesystem.MountPoint {
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

// forceFormatDisk will be called when the user chooses to force formatting the block device, the block device can only be
// force-formatted once only. And the controller may reconcile this method multiple times if the process is failed.
//
// - umount the block device if it is mounted
// - create a GUID partition table with a single linux partition of the block device
// - create ext4 filesystem formatting of the single partition
func (c *Controller) forceFormatDisk(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
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
	if device.Status.DeviceStatus.Details.DeviceType == diskv1.DeviceTypeDisk {
		// should never mount the parent device, using the first partition instead
		if err := disk.MakeGPTPartition(device.Spec.DevPath); err != nil {
			return device, err
		}

		// create the single partition block device
		part := c.BlockInfo.GetPartitionByDevPath(device.Spec.DevPath, device.Spec.DevPath+"1")
		partitionBlockDevice := DeviceInfoFromPartition(part, c.nodeName, c.namespace)
		partitionBlockDevice.Spec.FileSystem.MountPoint = filesystem.MountPoint
		partitionBlockDevice.Spec.FileSystem.ForceFormatted = true
		bd, err := c.BlockdeviceCache.Get(device.Namespace, partitionBlockDevice.Name)
		diskv1.DeviceFormatting.SetStatusBool(partitionBlockDevice, true)
		diskv1.DeviceFormatting.Message(partitionBlockDevice, fmt.Sprintf("formatting disk partition %s with ext4 filesystem", partitionBlockDevice.Spec.DevPath))
		if err != nil && !errors.IsNotFound(err) {
			return device, err
		}

		if bd == nil {
			if _, err := c.Blockdevices.Create(partitionBlockDevice); err != nil {
				logrus.Errorf("failed to create partition block device %s, %s", bd.Name, err.Error())
				return device, err
			}
		} else {
			toUpdate := bd.DeepCopy()
			toUpdate.Spec = partitionBlockDevice.Spec
			toUpdate.Status = partitionBlockDevice.Status
			diskv1.DeviceFormatting.SetStatusBool(toUpdate, true)
			diskv1.DeviceFormatting.Message(partitionBlockDevice, fmt.Sprintf("formatting disk partition %s with ext4 filesystem", toUpdate.Spec.DevPath))
			fmt.Printf("debug to update %s: %+v\n", bd.Name, bd.Spec.FileSystem)
			if _, err := c.Blockdevices.Update(toUpdate); err != nil {
				logrus.Errorf("failed to update partition block device %s, %s", bd.Name, err.Error())
				return device, err
			}

		}
		device.Spec.FileSystem.MountPoint = ""
		device.Status.DeviceStatus.Partitioned = true
	}

	// make ext4 filesystem format of the partition disk
	if device.Status.DeviceStatus.Details.DeviceType == diskv1.DeviceTypePart {
		logrus.Debugf("make ext4 filesystem format of disk %s", device.Spec.DevPath)
		if err := disk.MakeExt4DiskFormatting(device.Spec.DevPath, device.Name); err != nil {
			return device, err
		}
		diskv1.DeviceFormatting.SetError(device, "", nil)
		diskv1.DeviceFormatting.SetStatusBool(device, false)
		diskv1.DeviceFormatting.Message(device, "Done device ext4 filesystem formatting")
	}

	return device, nil
}

// addDeviceToNode adds a device to longhorn node as an additional disk.
func (c *Controller) addDeviceToNode(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	node, err := c.nodeCache.Get(c.namespace, c.nodeName)
	if err != nil {
		return device, err
	}

	mountPoint := device.Spec.FileSystem.MountPoint
	if disk, ok := node.Spec.Disks[device.Name]; ok && disk.Path == mountPoint {
		// Device exists and with the same mount point. No need to update.
		return device, nil
	}

	nodeCpy := node.DeepCopy()
	diskSpec := lhtypes.DiskSpec{
		Path:              mountPoint,
		AllowScheduling:   true,
		EvictionRequested: false,
		StorageReserved:   0, // TODO: shall we expose this field to user?
		Tags:              []string{},
	}
	nodeCpy.Spec.Disks[device.Name] = diskSpec
	if _, err = c.nodes.Update(nodeCpy); err != nil {
		return device, err
	}

	msg := fmt.Sprintf("Added disk %s to longhorn node `%s` as an additional disk", device.Name, nodeCpy.Name)
	diskv1.DiskAddedToNode.SetError(device, "", nil)
	diskv1.DiskAddedToNode.SetStatusBool(device, true)
	diskv1.DiskAddedToNode.Message(device, msg)
	return device, nil
}

// removeDeviceFromNode rmoves a device from a longhorn node.
func (c *Controller) removeDeviceFromNode(device *diskv1.BlockDevice) error {
	if !diskv1.DiskAddedToNode.IsTrue(device) {
		return nil
	}
	node, err := c.nodeCache.Get(c.namespace, c.nodeName)
	if err != nil {
		if errors.IsNotFound(err) {
			// Skip since the node is not there.
			return nil
		}
		return err
	}

	if _, ok := node.Spec.Disks[device.Name]; !ok {
		logrus.Debugf("disk %s not found in disks of longhorn node %s/%s", device.Name, c.namespace, c.nodeName)
		return nil
	}
	nodeCpy := node.DeepCopy()
	delete(nodeCpy.Spec.Disks, device.Name)
	if _, err := c.nodes.Update(nodeCpy); err != nil {
		return err
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

func (c *Controller) SaveBlockDevice(bd *diskv1.BlockDevice, oldBds map[string]*diskv1.BlockDevice) error {
	if oldBd, ok := oldBds[bd.Name]; ok {
		if !reflect.DeepEqual(oldBd, bd) {
			logrus.Infof("Update existing block device %s with devPath: %s", oldBd.Name, oldBd.Spec.DevPath)
			toUpdate := oldBd.DeepCopy()
			toUpdate.Status.DeviceStatus = bd.Status.DeviceStatus
			lastFormatted := oldBd.Status.DeviceStatus.FileSystem.LastFormattedAt
			if lastFormatted != nil {
				toUpdate.Status.DeviceStatus.FileSystem.LastFormattedAt = lastFormatted
			}
			if _, err := c.Blockdevices.Update(toUpdate); err != nil {
				return err
			}
		}
		// remove blockedevice from old device so we can delete missing devices afterward
		delete(oldBds, bd.Name)
		return nil
	}

	logrus.Infof("Add new block device %s with device: %s", bd.Name, bd.Spec.DevPath)
	if _, err := c.Blockdevices.Create(bd); err != nil {
		return err
	}
	return nil
}

// OnBlockDeviceDelete will delete the block devices that belongs to the same parent device
func (c *Controller) OnBlockDeviceDelete(key string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device == nil {
		return nil, nil
	}

	bds, err := c.BlockdeviceCache.List(c.namespace, labels.SelectorFromSet(map[string]string{
		ParentDeviceLabel: device.Name,
	}))
	if err != nil {
		return device, err
	}

	for _, bd := range bds {
		// Remove disk from related node if needed
		if err := c.removeDeviceFromNode(bd); err != nil {
			return device, err
		}
		if err := c.Blockdevices.Delete(c.namespace, bd.Name, &metav1.DeleteOptions{}); err != nil {
			return device, err
		}
	}
	return nil, nil
}

// ApplyFilter check the status of every register filters if the disk meets the filter criteria it will return true else it will return false
func (c *Controller) ApplyFilter(disk *block.Disk) bool {
	for _, filter := range c.Filters {
		if filter.ApplyFilter(disk) {
			logrus.Debugf("block device /dev/%s ignored by %s", disk.Name, filter.Name)
			return true
		}
	}
	return false
}
