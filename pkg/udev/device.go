package udev

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/longhorn/node-disk-manager/pkg/apis/longhorn.io/v1beta1"
	"github.com/longhorn/node-disk-manager/pkg/block"
	"github.com/longhorn/node-disk-manager/pkg/controller/blockdevice"
	"github.com/longhorn/node-disk-manager/pkg/util"
)

const (
	UDEV_SYSTEM     = "disk"      // used to filter devices other than disk which udev tracks (eg. CD ROM)
	UDEV_PARTITION  = "partition" // used to filter out partitions
	LINK_NAME_INDEX = 2           // this is used to get link index from dev link

	UDEV_ID_PATH = "ID_PATH" // udev attribute to get device id path
	UDEV_TYPE    = "ID_TYPE" // udev attribute to get device option
	UDEV_DEVTYPE = "DEVTYPE" // udev attribute to get the device type
	UDEV_DEVNAME = "DEVNAME" // udev attribute contain disk name given by kernel
)

type UdevDevice map[string]string

func InitUdevDevice(udev map[string]string) UdevDevice {
	return udev
}

func (device UdevDevice) DeviceInfoFromUdevDisk(disk *block.Disk, nodeName, namespace string) *v1beta1.BlockDevice {
	fileSystemInfo := &v1beta1.FilesystemStatus{
		MountPoint: disk.FileSystemInfo.MountPoint,
		Type:       disk.FileSystemInfo.Type,
		IsReadOnly: disk.FileSystemInfo.IsReadOnly,
	}

	return &v1beta1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GetBlockDeviceName(disk.Name, nodeName),
			Namespace: namespace,
			Labels: map[string]string{
				v1.LabelHostname:            nodeName,
				blockdevice.DeviceTypeLabel: string(v1beta1.DeviceTypeDisk),
			},
		},
		Spec: v1beta1.BlockDeviceSpec{
			NodeName: nodeName,
			DevPath:  util.GetFullDevPath(disk.Name),
			FileSystem: &v1beta1.FilesystemInfo{
				MountPoint: fileSystemInfo.MountPoint,
			},
		},
		Status: v1beta1.BlockDeviceStatus{
			State: v1beta1.BlockDeviceActive,
			DeviceStatus: v1beta1.DeviceStatus{
				Partitioned: block.HasPartitions(disk),
				Capacity: v1beta1.DeviceCapcity{
					SizeBytes:              disk.SizeBytes,
					PhysicalBlockSizeBytes: disk.PhysicalBlockSizeBytes,
				},
				Details: v1beta1.DeviceDetails{
					DeviceType:        v1beta1.DeviceTypeDisk,
					DriveType:         disk.DriveType.String(),
					IsRemovable:       disk.IsRemovable,
					StorageController: disk.StorageController.String(),
					UUID:              disk.UUID,
					PtUUID:            disk.PtUUID,
					BusPath:           disk.BusPath,
					Model:             disk.Model,
					Vendor:            disk.Vendor,
					SerialNumber:      disk.SerialNumber,
					NUMANodeID:        disk.NUMANodeID,
					WWN:               disk.WWN,
				},
				FileSystem: fileSystemInfo,
			},
		},
	}
}

func (device UdevDevice) DeviceInfoFromUdevPartition(part *block.Partition, nodeName, namespace string) *v1beta1.BlockDevice {
	fileSystemInfo := &v1beta1.FilesystemStatus{
		Type:       part.FileSystemInfo.Type,
		MountPoint: part.FileSystemInfo.MountPoint,
		IsReadOnly: part.FileSystemInfo.IsReadOnly,
	}
	status := v1beta1.BlockDeviceStatus{
		DeviceStatus: v1beta1.DeviceStatus{
			Capacity: v1beta1.DeviceCapcity{
				SizeBytes:              part.SizeBytes,
				PhysicalBlockSizeBytes: part.Disk.PhysicalBlockSizeBytes,
			},
			Partitioned: false,
			Details: v1beta1.DeviceDetails{
				DeviceType:        v1beta1.DeviceTypePart,
				Label:             part.Label,
				PartUUID:          part.UUID,
				DriveType:         part.DriveType.String(),
				StorageController: part.StorageController.String(),
			},
			FileSystem:   fileSystemInfo,
			ParentDevice: util.GetFullDevPath(part.Disk.Name),
		},
		State: v1beta1.BlockDeviceActive,
	}

	return &v1beta1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GetBlockDeviceName(part.Name, nodeName),
			Namespace: namespace,
			Labels: map[string]string{
				v1.LabelHostname:              nodeName,
				blockdevice.ParentDeviceLabel: util.GetBlockDeviceName(part.Disk.Name, nodeName),
				blockdevice.DeviceTypeLabel:   string(v1beta1.DeviceTypePart),
			},
		},
		Spec: v1beta1.BlockDeviceSpec{
			NodeName: nodeName,
			DevPath:  util.GetFullDevPath(part.Name),
			FileSystem: &v1beta1.FilesystemInfo{
				MountPoint: part.FileSystemInfo.MountPoint,
			},
		},
		Status: status,
	}
}

// IsDisk check if device is a disk
func (device UdevDevice) IsDisk() bool {
	return device[UDEV_DEVTYPE] == UDEV_SYSTEM
}

// IsPartition check if device is a partition
func (device UdevDevice) IsPartition() bool {
	return device[UDEV_DEVTYPE] == UDEV_PARTITION
}

// GetDevName returns the path of device in /dev directory
func (device UdevDevice) GetDevName() string {
	return device[UDEV_DEVNAME]
}

// GetShortName returns the short device name of the /dev directory, e.g /dev/sda will return the name sda
func (device UdevDevice) GetShortName() string {
	name := device[UDEV_DEVNAME]
	parts := strings.Split(name, "/")
	if len(parts) < LINK_NAME_INDEX+1 {
		return ""
	}
	return parts[LINK_NAME_INDEX]
}

// GetIDPath returns the device id path
func (device UdevDevice) GetIDPath() string {
	return device[UDEV_ID_PATH]
}

func (device UdevDevice) GetIDType() string {
	return device[UDEV_TYPE]
}

func (device UdevDevice) GetDevType() string {
	return device[UDEV_DEVTYPE]
}
