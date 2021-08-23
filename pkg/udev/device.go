package udev

import (
	"strings"

	"github.com/longhorn/node-disk-manager/pkg/block"
)

const (
	UDEV_SYSTEM     = "disk"      // used to filter devices other than disk which udev tracks (eg. CD ROM)
	UDEV_PARTITION  = "partition" // used to filter out partitions
	LINK_NAME_INDEX = 2           // this is used to get link index from dev link

	UDEV_DEVNAME         = "DEVNAME"
	UDEV_DEVTYPE         = "DEVTYPE"
	UDEV_FS_UUID         = "ID_FS_UUID"
	UDEV_ID_PATH         = "ID_PATH"
	UDEV_MODEL           = "ID_MODEL"
	UDEV_PART_ENTRY_TYPE = "ID_PART_ENTRY_TYPE"
	UDEV_PART_ENTRY_UUID = "ID_PART_ENTRY_UUID"
	UDEV_PART_TABLE_TYPE = "ID_PART_TABLE_TYPE"
	UDEV_PART_TABLE_UUID = "ID_PART_TABLE_UUID"
	UDEV_SERIAL_NUMBER   = "ID_SERIAL"
	UDEV_TYPE            = "ID_TYPE"
	UDEV_VENDOR          = "ID_VENDOR"
	UDEV_WWN             = "ID_WWN"
)

type UdevDevice map[string]string

func InitUdevDevice(udev map[string]string) UdevDevice {
	return udev
}

func (device UdevDevice) updateDiskFromUdev(disk *block.Disk) {
	if len(device[UDEV_FS_UUID]) > 0 {
		disk.UUID = device[UDEV_FS_UUID]
	}
	if len(device[UDEV_PART_TABLE_UUID]) > 0 {
		disk.PtUUID = device[UDEV_PART_TABLE_UUID]
	}
	if len(device[UDEV_MODEL]) > 0 {
		disk.Model = device[UDEV_MODEL]
	}
	if len(UDEV_VENDOR) > 0 {
		disk.Vendor = device[UDEV_VENDOR]
	}
	if len(device[UDEV_SERIAL_NUMBER]) > 0 {
		disk.SerialNumber = device[UDEV_SERIAL_NUMBER]
	}
	if len(device[UDEV_WWN]) > 0 {
		disk.WWN = device[UDEV_WWN]
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
