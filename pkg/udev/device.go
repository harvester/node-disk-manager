package udev

import (
	"github.com/harvester/node-disk-manager/pkg/block"
)

// Udev event actions NDM reacts to.
const (
	ActionAdd    = "add"
	ActionRemove = "remove"
)

// UdevSystem is the DEVTYPE of whole block devices; partitions have DEVTYPE=partition.
const UdevSystem = "disk"

// Names of the udev properties NDM reads.
const (
	UdevAction = "ACTION"

	UdevDevname       = "DEVNAME"
	UdevDevtype       = "DEVTYPE"
	UdevFsUUID        = "ID_FS_UUID"
	UdevModel         = "ID_MODEL"
	UdevPartTableUUID = "ID_PART_TABLE_UUID"
	UdevSerialNumber  = "ID_SERIAL"
	UdevSerialShort   = "ID_SERIAL_SHORT"
	UdevVendor        = "ID_VENDOR"
	UdevWWN           = "ID_WWN"
	UdevWWNExtension  = "ID_WWN_WITH_EXTENSION"

	// Multipath maps (/dev/dm-*) hide the physical disks, their identity is
	// exported by the multipath udev rules as DM_*.
	UdevDMSerialNumber = "DM_SERIAL"
	UdevDMWWN          = "DM_WWN"
)

// Device are the properties of a udev event. Which of them are set depends on
// the bus, the driver and the event, a missing property reads as empty string.
type Device map[string]string

// Action returns the event action, e.g. "add" or "remove".
func (device Device) Action() string {
	return device[UdevAction]
}

// IsDisk checks if device is a whole disk.
func (device Device) IsDisk() bool {
	return device[UdevDevtype] == UdevSystem
}

// GetDevName returns the path of the device node in /dev, e.g. "/dev/sda".
func (device Device) GetDevName() string {
	return device[UdevDevname]
}

// UpdateDiskFromUdev fills the identity of disk from the event properties.
// This is all that is known about a disk in "remove" events, as the device is
// already gone from sysfs. Serial number and WWN follow the precedence of
// pkg/block (diskSerialNumber, diskWWN).
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
	if len(device[UdevVendor]) > 0 {
		disk.Vendor = device[UdevVendor]
	}
	if len(device[UdevSerialShort]) > 0 {
		disk.SerialNumber = device[UdevSerialShort]
	} else if len(device[UdevSerialNumber]) > 0 {
		disk.SerialNumber = device[UdevSerialNumber]
	} else if len(device[UdevDMSerialNumber]) > 0 {
		disk.SerialNumber = device[UdevDMSerialNumber]
	}
	if len(device[UdevWWNExtension]) > 0 {
		disk.WWN = device[UdevWWNExtension]
	} else if len(device[UdevWWN]) > 0 {
		disk.WWN = device[UdevWWN]
	} else if len(device[UdevDMWWN]) > 0 {
		disk.WWN = device[UdevDMWWN]
	}
}
