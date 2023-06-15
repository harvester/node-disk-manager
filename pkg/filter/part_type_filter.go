package filter

import (
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

const (
	partTypeFilterName = "parttype filter"
	partTypeBIOSBoot   = "21686148-6449-6E6F-744E-656564454649"
)

var (
	defaultExcludedPartTypes = []string{partTypeBIOSBoot}
)

// partPartTypeFilter filters disk based on given part type, e.g., BIOS boot.
type partPartTypeFilter struct {
	partType []string
}

// diskPartTypeFilter filters disk if any of its partitions matches given part type.
type diskPartTypeFilter struct {
	filter *partPartTypeFilter
}

func RegisterPartTypeFilter(filters ...string) *Filter {
	f := &partPartTypeFilter{}
	for _, filter := range filters {
		if filter != "" {
			f.partType = append(f.partType, filter)
		}
	}
	return &Filter{
		Name:       partTypeFilterName,
		PartFilter: f,
		DiskFilter: &diskPartTypeFilter{filter: f},
	}
}

// Match returns true if partition matches given part types.
func (f *partPartTypeFilter) Match(part *block.Partition) bool {
	if part.PartType == "" {
		return false
	}
	return utils.MatchesIgnoredCase(f.partType, part.PartType)
}

// Match returns true if any of its partitions matches.
func (f *diskPartTypeFilter) Match(disk *block.Disk) bool {
	for _, part := range disk.Partitions {
		if f.filter.Match(part) {
			return true
		}
	}
	return false
}
