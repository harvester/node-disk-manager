package filter

import (
	"strings"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	pathFilterName        = "path filter"
	pathFilterDefaultRoot = "/"
)

var (
	excludePaths         = ""
	defaultExcludedPaths = []string{pathFilterDefaultRoot}
)

type partPathFilter struct {
	excludePaths []string
}

type diskPathFilter struct {
	excludePaths []string
}

func RegisterPathFilter(filters string) *Filter {
	f := &partPathFilter{}

	// add default exclude paths
	f.excludePaths = append(f.excludePaths, defaultExcludedPaths...)

	if filters != "" {
		f.excludePaths = append(f.excludePaths, strings.Split(filters, ",")...)
	}

	return &Filter{
		Name:       pathFilterName,
		PartFilter: f,
		DiskFilter: &diskPathFilter{excludePaths: f.excludePaths},
	}
}

// Exclude returns true if mount path of the partition is matched
func (f *partPathFilter) Exclude(part *block.Partition) bool {
	if len(f.excludePaths) == 0 {
		return true
	}

	if util.ContainsIgnoredCase(f.excludePaths, part.FileSystemInfo.MountPoint) {
		return true
	}

	return false
}

// Exclude returns true if mount path of the disk is matched
func (f *diskPathFilter) Exclude(disk *block.Disk) bool {
	if len(f.excludePaths) == 0 {
		return true
	}

	if util.ContainsIgnoredCase(f.excludePaths, disk.FileSystemInfo.MountPoint) {
		return true
	}

	return false
}
