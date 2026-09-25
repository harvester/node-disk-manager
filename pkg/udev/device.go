package udev

import (
	"path/filepath"
	"strings"

	"github.com/harvester/node-disk-manager/pkg/block"
)

// Udev event property names and expected values.
//
//  1. Linux sysfs naming volatile vs. udev ID_* stability:
//     Kernel device names like "sda", "sdb", "nvme0n1" are completely non-deterministic.
//     They depend solely on the order in which storage controllers and buses are initialized
//     by the kernel at boot. If a SATA cable is moved or a drive responds 10ms slower,
//     what was "sda" yesterday can become "sdb" today.
//     udev solves this by running probe helpers (blkid, ata_id, scsi_id, path_id) and
//     generating persistent "ID_*" properties (e.g. ID_SERIAL, ID_WWN, ID_PATH).
//
//  2. DEVTYPE hierarchy (disk vs. partition):
//     In Linux sysfs (/sys/class/block/), a physical or virtual whole drive has DEVTYPE=disk.
//     When partition tables (GPT/MBR) are scanned, the kernel registers child kobjects
//     (e.g. /sys/class/block/sda/sda1) with DEVTYPE=partition.
//     NDM strictly filters for DEVTYPE=disk to prevent treating individual partitions as
//     independent Kubernetes BlockDevices.
//
//  3. Filesystem & Partition Table UUIDs:
//     - ID_FS_UUID: Populated by blkid probe when a valid filesystem superblock (ext4, xfs)
//     is found on the device.
//     - ID_PART_TABLE_UUID: The GUID of the GPT partition table itself.
//     NDM uses these to determine whether a disk is blank and eligible for auto-provisioning
//     or if it already holds partition/filesystem data that must be protected.
const (
	// UdevSystem identifies whole block devices in udev rules (DEVTYPE=disk).
	UdevSystem = "disk"

	UdevDevname       = "DEVNAME"
	UdevDevtype       = "DEVTYPE"
	UdevDevpath       = "DEVPATH"
	UdevFsUUID        = "ID_FS_UUID"
	UdevModel         = "ID_MODEL"
	UdevModelEnc      = "ID_MODEL_ENC"
	UdevPartTableUUID = "ID_PART_TABLE_UUID"
	UdevSerialNumber  = "ID_SERIAL"

	// DM_* properties originate from device-mapper and multipathd udev rules.
	// For multipath maps (/dev/dm-*), the physical disk controllers are masked behind
	// device-mapper targets. Standard SCSI/ATA inquiry rules may not populate ID_SERIAL;
	// instead, multipath rules export DM_SERIAL and DM_WWN representing the aggregated WWID.
	UdevDMSerialNumber = "DM_SERIAL"
	UdevSerialShort    = "ID_SERIAL_SHORT"
	UdevVendor         = "ID_VENDOR"
	UdevWWN            = "ID_WWN"
	UdevDMWWN          = "DM_WWN"
)

// Device is the udev environment represented as a map for convenient property
// access. Missing properties intentionally read as empty strings: udev fields
// vary by bus, driver, device state, and whether an event is an add or remove.
type Device map[string]string

func (device Device) UpdateDiskFromUdev(disk *block.Disk) {
	if disk == nil {
		return
	}

	// This is especially important for remove events: the device has already
	// disappeared from sysfs, but udev's event still carries its last known
	// identity properties for logging and reconciliation.
	if len(device[UdevFsUUID]) > 0 {
		disk.UUID = device[UdevFsUUID]
	}
	if len(device[UdevPartTableUUID]) > 0 {
		disk.PtUUID = device[UdevPartTableUUID]
	}
	if len(device[UdevModel]) > 0 {
		disk.Model = device[UdevModel]
	} else if len(device[UdevModelEnc]) > 0 {
		disk.Model = device[UdevModelEnc]
	}
	if len(device[UdevVendor]) > 0 {
		disk.Vendor = device[UdevVendor]
	}
	// Prefer short serials (the physical device serial), then regular udev serial,
	// then the device-mapper serial. This matches block device discovery so the
	// scanner and event path identify the same disk consistently.
	if len(device[UdevSerialShort]) > 0 {
		disk.SerialNumber = device[UdevSerialShort]
	} else if len(device[UdevSerialNumber]) > 0 {
		disk.SerialNumber = device[UdevSerialNumber]
	} else if len(device[UdevDMSerialNumber]) > 0 {
		disk.SerialNumber = device[UdevDMSerialNumber]
	}

	// The WWN follows the same physical-device-first precedence.
	if len(device[UdevWWN]) > 0 {
		disk.WWN = device[UdevWWN]
	} else if len(device[UdevDMWWN]) > 0 {
		disk.WWN = device[UdevDMWWN]
	}
}

// IsDisk checks if device is a whole disk.
func (device Device) IsDisk() bool {
	return device[UdevDevtype] == UdevSystem
}

// GetDevName returns the path of device in /dev directory.
// If DEVNAME is omitted by udev (which can happen on removal events where
// the kernel unlinks the dev node before udev finishes processing), it falls
// back to reconstructing /dev/<kernel-name> from DEVPATH.
func (device Device) GetDevName() string {
	if name := device[UdevDevname]; name != "" {
		if !strings.HasPrefix(name, "/dev/") {
			return "/dev/" + name
		}
		return name
	}
	if devPath := device[UdevDevpath]; devPath != "" {
		base := filepath.Base(devPath)
		if base != "" && base != "." && base != "/" {
			return "/dev/" + base
		}
	}
	return ""
}
