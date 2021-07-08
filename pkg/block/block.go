package block

import (
	"github.com/jaypipes/ghw/pkg/block"
)

// borrowed from https://github.com/jaypipes/ghw/blob/master/pkg/block/block.go

// Disk describes a single disk drive on the host system. Disk drives provide
// raw block storage resources.
type Disk struct {
	Name                   string                  `json:"name"`
	SizeBytes              uint64                  `json:"size_bytes"`
	PhysicalBlockSizeBytes uint64                  `json:"physical_block_size_bytes"`
	DriveType              block.DriveType         `json:"drive_type"`
	IsRemovable            bool                    `json:"removable"`
	StorageController      block.StorageController `json:"storage_controller"`
	UUID                   string                  `json:"uuid"`    // This would be volume UUID on macOS, UUID on linux, empty on Windows
	PtUUID                 string                  `json:"pt_uuid"` // This would be volume PtUUID on macOS, PartUUID on linux, empty on Windows
	BusPath                string                  `json:"bus_path"`
	FileSystemInfo         FileSystemInfo          `json:"file_system_info"`
	NUMANodeID             int                     `json:"numa_node_id"`
	Vendor                 string                  `json:"vendor"`
	Model                  string                  `json:"model"`
	SerialNumber           string                  `json:"serial_number"`
	WWN                    string                  `json:"wwn"`
	Partitions             []*Partition            `json:"partitions"`
}

// Partition describes a logical division of a Disk.
type Partition struct {
	Disk              *Disk                   `json:"-"`
	Name              string                  `json:"name"`
	Label             string                  `json:"label"`
	SizeBytes         uint64                  `json:"size_bytes"`
	UUID              string                  `json:"uuid"` // This would be volume UUID on macOS, PartUUID on linux, empty on Windows
	DriveType         block.DriveType         `json:"drive_type"`
	StorageController block.StorageController `json:"storage_controller"`
	FileSystemInfo    FileSystemInfo          `json:"file_system_info"`
}

type FileSystemInfo struct {
	FsType     string `json:"fs_type"`
	IsReadOnly bool   `json:"read_only"`
	MountPoint string `json:"mount_point"`
}
