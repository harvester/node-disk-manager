package blockdevice

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/longhorn/node-disk-manager/pkg/disk"

	lhutil "github.com/longhorn/longhorn-manager/util"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	diskv1 "github.com/longhorn/node-disk-manager/pkg/apis/longhorn.io/v1beta1"
	"github.com/longhorn/node-disk-manager/pkg/block"
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

	Blockdevices     ctldiskv1.BlockDeviceController
	BlockdeviceCache ctldiskv1.BlockDeviceCache
	BlockInfo        *block.Info
	Filters          []*filter.Filter
}

// Register register the block device CRD controller
func Register(ctx context.Context, bds ctldiskv1.BlockDeviceController, block *block.Info,
	opt *option.Option, filters []*filter.Filter) error {
	controller := &Controller{
		namespace:        opt.Namespace,
		nodeName:         opt.NodeName,
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
	bds := make([]*diskv1.BlockDevice, 0)

	// list all the block devices
	for _, disk := range c.BlockInfo.Disks {
		// ignore block device by filters
		if c.ApplyFilter(disk) {
			continue
		}

		logrus.Infof("Found a block device /dev/%s", disk.Name)
		blockDevices := GetNewBlockDevices(disk, c.nodeName, c.namespace)
		bds = append(bds, blockDevices...)
	}

	bdList, err := c.Blockdevices.List(c.namespace, v1.ListOptions{})
	if err != nil {
		return err
	}

	// either create or update the block device
	for _, bd := range bds {
		if err := c.SaveBlockDeviceByList(bd, bdList); err != nil {
			return err
		}
	}

	// clean up previous registered block device
	for _, existingBD := range bdList.Items {
		toDelete := true
		for _, bd := range bds {
			if existingBD.Name == bd.Name {
				toDelete = false
			}
		}
		if toDelete {
			if err := c.Blockdevices.Delete(existingBD.Namespace, existingBD.Name, &metav1.DeleteOptions{}); err != nil {
				return err
			}
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
	fs := deviceCpy.Spec.FileSystem
	fsStatus := deviceCpy.Status.DeviceStatus.FileSystem

	if fs.ForceFormatted && fsStatus.LastFormattedAt == nil {
		// - perform disk force-formatted operation
		// - partition block device CR will be updated by the udev event watcher
		if err := forceFormatDisk(device); err != nil {
			diskv1.DeviceFormatted.SetStatusBool(deviceCpy, false)
			diskv1.DeviceFormatted.SetError(deviceCpy, "", fmt.Errorf("failed to force format the block device %s, error: %s",
				device.Spec.DevPath, err.Error()))
			return c.Blockdevices.Update(deviceCpy)
		}

		// unset the parent device filesystem info since it has assigned to the single partition disk
		deviceCpy.Spec.FileSystem = nil
		deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
	} else {
		if fs.MountPoint != fsStatus.MountPoint && !deviceCpy.Status.DeviceStatus.Partitioned {
			// mount the dev by path, do not mount partitioned device
			if err := updateDeviceMount(deviceCpy.Spec.DevPath, fs.MountPoint, fsStatus.MountPoint); err != nil {
				diskv1.DeviceMounted.SetStatusBool(deviceCpy, false)
				diskv1.DeviceMounted.SetError(deviceCpy, "", fmt.Errorf("failed to mount the device %s to path %s, error: %s",
					device.Spec.DevPath, device.Spec.FileSystem.MountPoint, err.Error()))
				return c.Blockdevices.Update(deviceCpy)
			}
		}
	}

	// fetch the latest device filesystem info
	if device.Status.DeviceStatus.ParentDevice == "" {
		disk := c.BlockInfo.GetDiskByDevPath(deviceCpy.Spec.DevPath)
		deviceCpy.Status.DeviceStatus.FileSystem.Type = disk.FileSystemInfo.FsType
		deviceCpy.Status.DeviceStatus.FileSystem.MountPoint = disk.FileSystemInfo.MountPoint
	} else {
		part := c.BlockInfo.GetPartitionByDevPath(deviceCpy.Status.DeviceStatus.ParentDevice, deviceCpy.Spec.DevPath)
		deviceCpy.Status.DeviceStatus.FileSystem.Type = part.FileSystemInfo.FsType
		deviceCpy.Status.DeviceStatus.FileSystem.MountPoint = part.FileSystemInfo.MountPoint
	}

	if deviceCpy.Status.DeviceStatus.FileSystem.MountPoint != "" && deviceCpy.Status.DeviceStatus.FileSystem.Type != "" {
		err := isValidFileSystem(deviceCpy.Spec.FileSystem, deviceCpy.Status.DeviceStatus.FileSystem)
		mounted := err == nil && deviceCpy.Status.DeviceStatus.FileSystem.MountPoint != ""
		diskv1.DeviceMounted.SetStatusBool(deviceCpy, mounted)
		diskv1.DeviceMounted.SetError(deviceCpy, "", err)
		if err != nil {
			diskv1.DeviceMounted.Message(deviceCpy, err.Error())
		}
	}

	if !reflect.DeepEqual(device, deviceCpy) {
		if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
			return device, err
		}
	}

	return nil, nil
}

func updateDeviceMount(devPath, mountPoint, existingMount string) error {
	// umount the previous path if exist
	if existingMount != "" {
		if err := disk.UmountDisk(existingMount); err != nil {
			return err
		}
	}

	return disk.MountDisk(devPath, mountPoint)
}

// forceFormatDisk will be called when the user chooses to force formatting the block device, the block device can only be
// force-formatted once only. And the controller may reconcile this method multiple times if the process is failed.
//
// - umount the block device if it is mounted
// - create a GUID partition table with a single linux partition of block device only
// - create ext4 filesystem formatting of the single partition
// - mount the single partition disk to the path
func forceFormatDisk(device *diskv1.BlockDevice) error {
	filesystem := device.Spec.FileSystem
	fsStatus := device.Status.DeviceStatus.FileSystem
	logrus.Infof("performing format operation of disk %s, mount path %s", device.Spec.DevPath, filesystem.MountPoint)

	// umount the disk if it is mounted
	if fsStatus.MountPoint != "" {
		if err := disk.UmountDisk(fsStatus.MountPoint); err != nil {
			return err
		}
	}

	devPath := device.Spec.DevPath
	// create a GUID partition table with a single linux partition only
	if device.Status.DeviceStatus.Details.DeviceType == diskv1.DeviceTypeDisk {
		if err := disk.MakeGPTPartition(devPath); err != nil {
			return err
		}

		// create ext4 filesystem formatting of the partition disk
		devPath = devPath + "1"
		if err := disk.MakeDiskFormatting(devPath, "ext4"); err != nil {
			return err
		}
	}

	// mount device to the path
	if filesystem.MountPoint != "" {
		if err := disk.MountDisk(devPath, filesystem.MountPoint); err != nil {
			return err
		}
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

func (c *Controller) SaveBlockDevice(blockDevice *diskv1.BlockDevice, bds []*diskv1.BlockDevice) error {
	for _, existingBD := range bds {
		if existingBD.Name == blockDevice.Name {
			if !reflect.DeepEqual(existingBD, blockDevice) {
				logrus.Infof("Update existing block device %s with devPath: %s", existingBD.Name, existingBD.Spec.DevPath)
				toUpdate := existingBD.DeepCopy()
				toUpdate.Spec = blockDevice.Spec
				toUpdate.Status.DeviceStatus = blockDevice.Status.DeviceStatus
				if _, err := c.Blockdevices.Update(toUpdate); err != nil {
					return err
				}
			}
			return nil
		}
	}

	logrus.Infof("Add new block device %s with device: %s", blockDevice.Name, blockDevice.Spec.DevPath)
	if _, err := c.Blockdevices.Create(blockDevice); err != nil {
		return err
	}
	return nil
}

func (c *Controller) SaveBlockDeviceByList(blockDevice *diskv1.BlockDevice, bdList *diskv1.BlockDeviceList) error {
	for _, existingBD := range bdList.Items {
		if existingBD.Name == blockDevice.Name {
			if !reflect.DeepEqual(existingBD, blockDevice) {
				logrus.Infof("Update existing block device %s with device: %s", existingBD.Name, existingBD.Spec.DevPath)
				toUpdate := existingBD.DeepCopy()
				toUpdate.Status.DeviceStatus = blockDevice.Status.DeviceStatus
				if _, err := c.Blockdevices.Update(toUpdate); err != nil {
					return err
				}
			}
			return nil
		}
	}

	logrus.Infof("Add new block device %s with device: %s", blockDevice.Name, blockDevice.Spec.DevPath)
	if _, err := c.Blockdevices.Create(blockDevice); err != nil {
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
