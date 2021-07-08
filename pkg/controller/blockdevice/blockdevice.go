package blockdevice

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	longhornv1 "github.com/longhorn/node-disk-manager/pkg/apis/longhorn.io/v1beta1"
	"github.com/longhorn/node-disk-manager/pkg/block"
	"github.com/longhorn/node-disk-manager/pkg/util"
)

const (
	ParentDeviceLabel = "ndm.longhorn.io/parent-device"
	DeviceTypeLabel   = "ndm.longhorn.io/device-type"
)

func GetNewBlockDevices(disk *block.Disk, nodeName, namespace string) []*longhornv1.BlockDevice {
	bdList := make([]*longhornv1.BlockDevice, 0)
	partitioned := len(disk.Partitions) > 0
	fileSystemInfo := longhornv1.FilesystemStatus{
		MountPoint: disk.FileSystemInfo.MountPoint,
		Type:       disk.FileSystemInfo.FsType,
		IsReadOnly: disk.FileSystemInfo.IsReadOnly,
	}
	parent := &longhornv1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GetBlockDeviceName(disk.Name, nodeName),
			Namespace: namespace,
			Labels: map[string]string{
				v1.LabelHostname: nodeName,
				DeviceTypeLabel:  string(longhornv1.DeviceTypeDisk),
			},
		},
		Spec: longhornv1.BlockDeviceSpec{
			NodeName: nodeName,
			DevPath:  getFullDevPath(disk.Name),
			FileSystem: longhornv1.FilesystemInfo{
				MountPoint: fileSystemInfo.MountPoint,
			},
		},
		Status: longhornv1.BlockDeviceStatus{
			State: longhornv1.BlockDeviceActive,
			DeviceStatus: longhornv1.DeviceStatus{
				Partitioned: partitioned,
				Capacity: longhornv1.DeviceCapcity{
					SizeBytes:              disk.SizeBytes,
					PhysicalBlockSizeBytes: disk.PhysicalBlockSizeBytes,
				},
				Details: longhornv1.DeviceDetails{
					DeviceType:        longhornv1.DeviceTypeDisk,
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
	bdList = append(bdList, parent)
	bdList = append(bdList, GetPartitionBlockDevices(disk.Partitions, parent, nodeName)...)
	return bdList
}

func GetPartitionBlockDevices(partitions []*block.Partition, parentDisk *longhornv1.BlockDevice, nodeName string) []*longhornv1.BlockDevice {
	blockDevices := make([]*longhornv1.BlockDevice, 0, len(partitions))
	for _, part := range partitions {
		fileSystemInfo := longhornv1.FilesystemStatus{
			Type:       part.FileSystemInfo.FsType,
			MountPoint: part.FileSystemInfo.MountPoint,
			IsReadOnly: part.FileSystemInfo.IsReadOnly,
		}
		status := longhornv1.BlockDeviceStatus{
			DeviceStatus: longhornv1.DeviceStatus{
				Capacity: longhornv1.DeviceCapcity{
					SizeBytes:              part.SizeBytes,
					PhysicalBlockSizeBytes: parentDisk.Status.DeviceStatus.Capacity.PhysicalBlockSizeBytes,
				},
				Partitioned: false,
				Details: longhornv1.DeviceDetails{
					DeviceType:        longhornv1.DeviceTypePart,
					Label:             part.Label,
					PartUUID:          part.UUID,
					DriveType:         part.DriveType.String(),
					StorageController: part.StorageController.String(),
				},
				FileSystem:   fileSystemInfo,
				ParentDevice: parentDisk.Spec.DevPath,
			},
			State: longhornv1.BlockDeviceActive,
		}

		blockDevice := &longhornv1.BlockDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      util.GetBlockDeviceName(part.Name, nodeName),
				Namespace: parentDisk.Namespace,
				Labels: map[string]string{
					v1.LabelHostname:  nodeName,
					ParentDeviceLabel: parentDisk.Name,
					DeviceTypeLabel:   string(longhornv1.DeviceTypePart),
				},
			},
			Spec: longhornv1.BlockDeviceSpec{
				NodeName: nodeName,
				DevPath:  util.GetFullDevPath(part.Name),
				FileSystem: longhornv1.FilesystemInfo{
					MountPoint: part.FileSystemInfo.MountPoint,
				},
			},
			Status: status,
		}

		blockDevices = append(blockDevices, blockDevice)
	}
	return blockDevices
}

func getFullDevPath(shortPath string) string {
	if shortPath == "" {
		return ""
	}
	return fmt.Sprintf("/dev/%s", shortPath)
}
