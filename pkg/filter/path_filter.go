package filter

import (
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	pathFilterName        = "path filter"
	pathFilterDefaultRoot = "/"
)

var (
	defaultExcludedPaths = []string{pathFilterDefaultRoot}
)

type partPathFilter struct {
	mountPaths []string
}

type diskPathFilter struct {
	mountPaths []string
}

func RegisterPathFilter(filters ...string) *Filter {
	f := &partPathFilter{}
	for _, filter := range filters {
		if filter != "" {
			f.mountPaths = append(f.mountPaths, filter)
		}
	}
	return &Filter{
		Name:       pathFilterName,
		PartFilter: f,
		DiskFilter: &diskPathFilter{mountPaths: f.mountPaths},
	}
}

// Match returns true if mount path of the partition is matched
func (f *partPathFilter) Match(part *block.Partition) bool {
	if part.FileSystemInfo.MountPoint == "" {
		return false
	}
	return util.ContainsIgnoredCase(f.mountPaths, part.FileSystemInfo.MountPoint)
}

// Match returns true if mount path of the disk is matched
func (f *diskPathFilter) Match(disk *block.Disk) bool {
	if disk.FileSystemInfo.MountPoint == "" {
		return false
	}
	return util.ContainsIgnoredCase(f.mountPaths, disk.FileSystemInfo.MountPoint)
}
