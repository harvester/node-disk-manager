package blockdevice

import (
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

func DeviceInfoFromDisk(disk *block.Disk, nodeName, namespace string) *longhornv1.BlockDevice {
	fileSystemInfo := &longhornv1.FilesystemStatus{
		MountPoint: disk.FileSystemInfo.MountPoint,
		Type:       disk.FileSystemInfo.Type,
		IsReadOnly: disk.FileSystemInfo.IsReadOnly,
	}

	status := longhornv1.BlockDeviceStatus{
		State: longhornv1.BlockDeviceActive,
		DeviceStatus: longhornv1.DeviceStatus{
			Partitioned: block.HasPartitions(disk),
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
	}

	bd := &longhornv1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Labels: map[string]string{
				v1.LabelHostname: nodeName,
				DeviceTypeLabel:  string(longhornv1.DeviceTypeDisk),
			},
		},
		Spec: longhornv1.BlockDeviceSpec{
			NodeName: nodeName,
			DevPath:  util.GetFullDevPath(disk.Name),
			FileSystem: &longhornv1.FilesystemInfo{
				MountPoint: fileSystemInfo.MountPoint,
			},
		},
		Status: status,
	}

	if guid := block.GenerateDiskGUID(disk); len(guid) > 0 {
		bd.ObjectMeta.Name = guid
	}

	return bd
}

func DeviceInfoFromPartition(part *block.Partition, nodeName, namespace string) *longhornv1.BlockDevice {
	fileSystemInfo := &longhornv1.FilesystemStatus{
		Type:       part.FileSystemInfo.Type,
		MountPoint: part.FileSystemInfo.MountPoint,
		IsReadOnly: part.FileSystemInfo.IsReadOnly,
	}
	status := longhornv1.BlockDeviceStatus{
		DeviceStatus: longhornv1.DeviceStatus{
			Capacity: longhornv1.DeviceCapcity{
				SizeBytes:              part.SizeBytes,
				PhysicalBlockSizeBytes: part.Disk.PhysicalBlockSizeBytes,
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
			ParentDevice: util.GetFullDevPath(part.Disk.Name),
		},
		State: longhornv1.BlockDeviceActive,
	}

	bd := &longhornv1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Labels: map[string]string{
				v1.LabelHostname: nodeName,
				DeviceTypeLabel:  string(longhornv1.DeviceTypePart),
			},
		},
		Spec: longhornv1.BlockDeviceSpec{
			NodeName: nodeName,
			DevPath:  util.GetFullDevPath(part.Name),
			FileSystem: &longhornv1.FilesystemInfo{
				MountPoint: part.FileSystemInfo.MountPoint,
			},
		},
		Status: status,
	}

	if parentDeviceName := block.GenerateDiskGUID(part.Disk); len(parentDeviceName) > 0 {
		bd.ObjectMeta.Labels[ParentDeviceLabel] = parentDeviceName
	}

	if guid := block.GeneratePartitionGUID(part); len(guid) > 0 {
		bd.ObjectMeta.Name = guid
	}

	return bd
}
