package blockdevice

import (
	"strings"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

const (
	// ParentDeviceLabel stores the parent device name of a device
	ParentDeviceLabel = "ndm.harvesterhci.io/parent-device"
	// DeviceTypeLabel indicates whether the device is a disk or a partition
	DeviceTypeLabel = "ndm.harvesterhci.io/device-type"
)

// GetDiskBlockDevice creates a BlockDevices from a given disk. Note that the _name_ of
// the BlockDevice retuned is not set by this function. The caller must set it before
// trying to actually create a BD CR based on this, and must take care when comparing BDs.
func GetDiskBlockDevice(disk *block.Disk, nodeName, namespace string) *diskv1.BlockDevice {
	fileSystemInfo := &diskv1.FilesystemStatus{
		MountPoint: disk.FileSystemInfo.MountPoint,
		Type:       disk.FileSystemInfo.Type,
		IsReadOnly: disk.FileSystemInfo.IsReadOnly,
	}
	devPath := utils.GetFullDevPath(disk.Name)

	// For dm-* devices, use the stable /dev/mapper/xxx path
	// This ensures device paths remain consistent across reboots
	if strings.HasPrefix(disk.Name, "dm-") {
		if mapperPath, err := utils.GetMapperDeviceFromDM(disk.Name); err == nil {
			logrus.Infof("Using stable mapper path %s for dm device %s", mapperPath, disk.Name)
			devPath = mapperPath
		} else {
			logrus.Warnf("Failed to resolve mapper path for %s, using dm path: %v", disk.Name, err)
		}
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
			DevPath:    devPath,
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
			DevPath:    devPath,
			FileSystem: &diskv1.FilesystemInfo{},
		},
		Status: status,
	}

	return bd
}
