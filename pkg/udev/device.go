package udev

import (
	"strings"

	"github.com/harvester/node-disk-manager/pkg/block"
)

// key and env of udev uevent.
const (
	// UdevSystem is used to filter devices other than disk which udev tracks (eg. CD ROM)
	UdevSystem = "disk"
	// UDevPartition is used to filter out partitions
	UdevPartition = "partition"
	// LinkNameIndex is used to get link index from dev link
	LinkNameIndex = 2

	UdevDevname        = "DEVNAME"
	UdevDevtype        = "DEVTYPE"
	UdevFsUUID         = "ID_FS_UUID"
	UdevIDPath         = "ID_PATH"
	UdevModel          = "ID_MODEL"
	UdevPartEntryType  = "ID_PART_ENTRY_TYPE"
	UdevPartEntryUUID  = "ID_PART_ENTRY_UUID"
	UdevPartTableType  = "ID_PART_TABLE_TYPE"
	UdevPartTableUUID  = "ID_PART_TABLE_UUID"
	UdevSerialNumber   = "ID_SERIAL"
	UdevDMSerialNumber = "DM_SERIAL" // multipath device
	UdevSerialShort    = "ID_SERIAL_SHORT"
	UdevType           = "ID_TYPE"
	UdevVendor         = "ID_VENDOR"
	UdevWWN            = "ID_WWN"
	UdevDMWWN          = "DM_WWN" // multipath device
)

type Device map[string]string

func InitUdevDevice(udev map[string]string) Device {
	return udev
}

func (device Device) UpdateDiskFromUdev(disk *block.Disk) {
	if len(device[UdevFsUUID]) > 0 {
		disk.UUID = device[UdevFsUUID]
	}
	if len(device[UdevPartTableUUID]) > 0 {
		disk.PtUUID = device[UdevPartTableUUID]
	}
	if len(device[UdevModel]) > 0 {
		disk.Model = device[UdevModel]
	}
	if len(UdevVendor) > 0 {
		disk.Vendor = device[UdevVendor]
	}
	// Match the logic from block.diskSerialNumber() to ensure
	// we get the correct serial number!
	if len(device[UdevSerialShort]) > 0 {
		disk.SerialNumber = device[UdevSerialShort]
	} else if len(device[UdevSerialNumber]) > 0 {
		disk.SerialNumber = device[UdevSerialNumber]
	} else if len(device[UdevDMSerialNumber]) > 0 {
		disk.SerialNumber = device[UdevDMSerialNumber]
	}

	if len(device[UdevWWN]) > 0 {
		disk.WWN = device[UdevWWN]
	} else if len(device[UdevDMWWN]) > 0 {
		disk.WWN = device[UdevDMWWN]
	}
}

// IsDisk check if device is a disk
func (device Device) IsDisk() bool {
	return device[UdevDevtype] == UdevSystem
}

// IsPartition check if device is a partition
func (device Device) IsPartition() bool {
	return device[UdevDevtype] == UdevPartition
}

// GetDevName returns the path of device in /dev directory
func (device Device) GetDevName() string {
	return device[UdevDevname]
}

// GetShortName returns the short device name of the /dev directory, e.g /dev/sda will return the name sda
func (device Device) GetShortName() string {
	name := device[UdevDevname]
	parts := strings.Split(name, "/")
	if len(parts) < LinkNameIndex+1 {
		return ""
	}
	return parts[LinkNameIndex]
}

// GetIDPath returns the device id path
func (device Device) GetIDPath() string {
	return device[UdevIDPath]
}

func (device Device) GetIDType() string {
	return device[UdevType]
}

func (device Device) GetDevType() string {
	return device[UdevDevtype]
}
