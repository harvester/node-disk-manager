package udev

import (
	"strings"
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
