package blockdevice

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

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
	fs := *deviceCpy.Spec.FileSystem
	fsStatus := *deviceCpy.Status.DeviceStatus.FileSystem

	if fs.ForceFormatted && fsStatus.LastFormattedAt == nil {
		// perform disk force-format operation
		if deviceCpy, err := c.forceFormatDisk(deviceCpy); err != nil {
			logrus.Errorf("failed to force format the device %s, %s", device.Spec.DevPath, err.Error())
			diskv1.DeviceFormatting.SetStatusBool(deviceCpy, true)
			diskv1.DeviceFormatting.SetError(deviceCpy, "", fmt.Errorf("failed to force format the block device %s, error: %s",
				device.Spec.DevPath, err.Error()))
			return c.Blockdevices.Update(deviceCpy)
		}

		diskv1.DeviceFormatting.SetStatusBool(deviceCpy, false)
		diskv1.DeviceFormatting.SetError(deviceCpy, "", nil)
		deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
	}

	// mount device by path, and skip mount partitioned device
	if !deviceCpy.Status.DeviceStatus.Partitioned && fs.MountPoint != fsStatus.MountPoint {
		if err := updateDeviceMount(deviceCpy.Spec.DevPath, fs.MountPoint, fsStatus.MountPoint); err != nil {
			diskv1.DeviceMounted.SetStatusBool(deviceCpy, false)
			diskv1.DeviceMounted.SetError(deviceCpy, "", fmt.Errorf("failed to mount the device %s to path %s, error: %s",
				device.Spec.DevPath, fs.MountPoint, err.Error()))
			return c.Blockdevices.Update(deviceCpy)
		}
	}

	// fetch the latest device filesystem info
	filesystem := c.BlockInfo.GetFileSystemInfoByDevPath(deviceCpy.Spec.DevPath)
	deviceCpy.Status.DeviceStatus.FileSystem.Type = filesystem.Type
	deviceCpy.Status.DeviceStatus.FileSystem.MountPoint = fs.MountPoint

	if filesystem.MountPoint != "" && filesystem.Type != "" {
		err := isValidFileSystem(deviceCpy.Spec.FileSystem, deviceCpy.Status.DeviceStatus.FileSystem)
		mounted := err == nil && filesystem.MountPoint != ""
		diskv1.DeviceMounted.SetStatusBool(deviceCpy, mounted)
		diskv1.DeviceMounted.SetError(deviceCpy, "", err)
		if err != nil {
			diskv1.DeviceMounted.Message(deviceCpy, err.Error())
		}
	} else if fs.MountPoint != "" && deviceCpy.Status.DeviceStatus.Partitioned {
		diskv1.DeviceMounted.SetStatusBool(deviceCpy, false)
		diskv1.DeviceMounted.SetError(deviceCpy, "", fmt.Errorf("cannot mount parent device with partitions"))
	} else if fs.MountPoint == "" && fs.MountPoint == filesystem.MountPoint {
		existingMount := deviceCpy.Status.DeviceStatus.FileSystem.MountPoint
		if existingMount != "" {
			if err := disk.UmountDisk(existingMount); err != nil {
				return device, err
			}
		}
		diskv1.DeviceMounted.SetStatus(deviceCpy, "False")
		diskv1.DeviceMounted.SetError(deviceCpy, "", nil)
	}

	if !reflect.DeepEqual(device, deviceCpy) {
		if _, err := c.Blockdevices.Update(deviceCpy); err != nil {
			return device, err
		}
	}

	return nil, nil
}

func updateDeviceMount(devPath, mountPoint, existingMount string) error {
	logrus.Infof("mount dev %s to path %s", devPath, mountPoint)
	// umount the previous path if exist
	if existingMount != "" {
		if err := disk.UmountDisk(existingMount); err != nil {
			return err
		}
	}

	if mountPoint != "" {
		logrus.Debugf("mount disk %s to %s", devPath, mountPoint)
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
		if err := disk.MakeGPTPartition(device.Spec.DevPath, device.Name); err != nil {
			return device, err
		}

		// create the single partition block device
		part := c.BlockInfo.GetPartitionByDevPath(device.Spec.DevPath, device.Spec.DevPath+"1")
		partitionBlockDevice := GetPartitionBlockDevice(part, device, c.nodeName)
		partitionBlockDevice.Spec.FileSystem.MountPoint = filesystem.MountPoint
		partitionBlockDevice.Spec.FileSystem.ForceFormatted = true
		bd, err := c.BlockdeviceCache.Get(device.Namespace, partitionBlockDevice.Name)
		diskv1.DeviceFormatting.SetStatus(partitionBlockDevice, "True")
		diskv1.DeviceFormatting.Message(partitionBlockDevice, fmt.Sprintf("formatting disk partition %s with ext4 filesystem", partitionBlockDevice.Spec.DevPath))
		if err != nil && !errors.IsNotFound(err) {
			return device, err
		}

		if bd == nil {
			if _, err := c.Blockdevices.Create(partitionBlockDevice); err != nil {
				logrus.Errorf("failed to create partion block device %s, %s", bd.Name, err.Error())
				return device, err
			}
		} else {
			toUpdate := bd.DeepCopy()
			toUpdate.Spec = partitionBlockDevice.Spec
			toUpdate.Status = partitionBlockDevice.Status
			diskv1.DeviceFormatting.SetStatus(toUpdate, "True")
			diskv1.DeviceFormatting.Message(partitionBlockDevice, fmt.Sprintf("formatting disk partition %s with ext4 filesystem", toUpdate.Spec.DevPath))
			fmt.Printf("debug to update %s: %+v\n", bd.Name, bd.Spec.FileSystem)
			if _, err := c.Blockdevices.Update(toUpdate); err != nil {
				logrus.Errorf("failed to update partion block device %s, %s", bd.Name, err.Error())
				return device, err
			}

		}
		device.Spec.FileSystem.MountPoint = ""
		device.Status.DeviceStatus.Partitioned = true
	}

	// make ext4 filesystem format of the partition disk
	if device.Status.DeviceStatus.Details.DeviceType == diskv1.DeviceTypePart {
		logrus.Debugf("make ext4 filesystem format of disk %s", device.Spec.DevPath)
		if err := disk.MakeExt4DiskFormatting(device.Spec.DevPath); err != nil {
			return device, err
		}
		diskv1.DeviceFormatting.SetStatus(device, "False")
		diskv1.DeviceFormatting.SetError(device, "", nil)
		diskv1.DeviceFormatting.Message(device, "Done device ext4 filesystem formatting")
	}

	return device, nil
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
				//toUpdate.Spec = blockDevice.Spec
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
				lastFormatted := existingBD.Status.DeviceStatus.FileSystem.LastFormattedAt
				if lastFormatted != nil {
					toUpdate.Status.DeviceStatus.FileSystem.LastFormattedAt = lastFormatted
				}
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
