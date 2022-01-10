package blockdevice

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	// ParentDeviceLabel stores the parent device name of a device
	ParentDeviceLabel = "ndm.harvesterhci.io/parent-device"
	// DeviceTypeLabel indicates whether the device is a disk or a partition
	DeviceTypeLabel = "ndm.harvesterhci.io/device-type"
)

// GetDiskBlockDevice gets a blockdevice from a given disk.
func GetDiskBlockDevice(disk *block.Disk, nodeName, namespace string) *diskv1.BlockDevice {
	fileSystemInfo := &diskv1.FilesystemStatus{
		MountPoint: disk.FileSystemInfo.MountPoint,
		Type:       disk.FileSystemInfo.Type,
		IsReadOnly: disk.FileSystemInfo.IsReadOnly,
	}

	status := diskv1.BlockDeviceStatus{
		State:          diskv1.BlockDeviceActive,
		ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
		DeviceStatus: diskv1.DeviceStatus{
			Partitioned: block.HasPartitions(disk),
			Capacity: diskv1.DeviceCapcity{
				SizeBytes:              disk.SizeBytes,
				PhysicalBlockSizeBytes: disk.PhysicalBlockSizeBytes,
			},
			Details: diskv1.DeviceDetails{
				DeviceType:        diskv1.DeviceTypeDisk,
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

	bd := &diskv1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Labels: map[string]string{
				v1.LabelHostname: nodeName,
				DeviceTypeLabel:  string(diskv1.DeviceTypeDisk),
			},
		},
		Spec: diskv1.BlockDeviceSpec{
			NodeName:   nodeName,
			DevPath:    util.GetFullDevPath(disk.Name),
			FileSystem: &diskv1.FilesystemInfo{},
		},
		Status: status,
	}

	if guid := block.GenerateDiskGUID(disk, nodeName); len(guid) > 0 {
		bd.ObjectMeta.Name = guid
	}

	return bd
}

// GetPartitionBlockDevice gets a blockdevice from a given partition.
func GetPartitionBlockDevice(part *block.Partition, nodeName, namespace string) *diskv1.BlockDevice {
	fileSystemInfo := &diskv1.FilesystemStatus{
		Type:       part.FileSystemInfo.Type,
		MountPoint: part.FileSystemInfo.MountPoint,
		IsReadOnly: part.FileSystemInfo.IsReadOnly,
	}
	status := diskv1.BlockDeviceStatus{
		State:          diskv1.BlockDeviceActive,
		ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
		DeviceStatus: diskv1.DeviceStatus{
			Capacity: diskv1.DeviceCapcity{
				SizeBytes:              part.SizeBytes,
				PhysicalBlockSizeBytes: part.Disk.PhysicalBlockSizeBytes,
			},
			Partitioned: false,
			Details: diskv1.DeviceDetails{
				DeviceType:        diskv1.DeviceTypePart,
				Label:             part.Label,
				PartUUID:          part.UUID,
				UUID:              part.FsUUID,
				DriveType:         part.DriveType.String(),
				StorageController: part.StorageController.String(),
			},
			FileSystem:   fileSystemInfo,
			ParentDevice: util.GetFullDevPath(part.Disk.Name),
		},
	}

	bd := &diskv1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Labels: map[string]string{
				v1.LabelHostname: nodeName,
				DeviceTypeLabel:  string(diskv1.DeviceTypePart),
			},
		},
		Spec: diskv1.BlockDeviceSpec{
			NodeName:   nodeName,
			DevPath:    util.GetFullDevPath(part.Name),
			FileSystem: &diskv1.FilesystemInfo{},
		},
		Status: status,
	}

	if parentDeviceName := block.GenerateDiskGUID(part.Disk, nodeName); len(parentDeviceName) > 0 {
		bd.ObjectMeta.Labels[ParentDeviceLabel] = parentDeviceName
	}

	if guid := block.GeneratePartitionGUID(part, nodeName); len(guid) > 0 {
		bd.ObjectMeta.Name = guid
	}

	return bd
}
