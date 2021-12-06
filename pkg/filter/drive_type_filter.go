package filter

import (
	ghwblock "github.com/jaypipes/ghw/pkg/block"

	"github.com/harvester/node-disk-manager/pkg/block"
)

const (
	driveTypeFilterName = "driver type filter"
)

// partDriveTypeFilter filters disk if any of its partitions matches given part type.
type partDriveTypeFilter struct{}

// diskDriveTypeFilter filters disk if any of its partitions matches given part type.
type diskDriveTypeFilter struct{}

func RegisterDriveTypeFilter() *Filter {
	return &Filter{
		Name:       driveTypeFilterName,
		PartFilter: &partDriveTypeFilter{},
		DiskFilter: &diskDriveTypeFilter{},
	}
}

// Match returns true if drive type is neither HDD nor SSD.
func (f *partDriveTypeFilter) Match(part *block.Partition) bool {
	return neitherHddNorSsd(part.DriveType)
}

// Match returns true if drive type is neither HDD nor SSD.
func (f *diskDriveTypeFilter) Match(disk *block.Disk) bool {
	return neitherHddNorSsd(disk.DriveType)
}

func neitherHddNorSsd(driveType ghwblock.DriveType) bool {
	return driveType != ghwblock.DRIVE_TYPE_HDD && driveType != ghwblock.DRIVE_TYPE_SSD
}
