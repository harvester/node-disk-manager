package filter

import (
	"path/filepath"
	"strings"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/sirupsen/logrus"
)

const (
	labelFilterName = "label filter"
)

// partLabelFilter filters disk based on given filesystem label patterns
type partLabelFilter struct {
	excludeLabels []string
}

// diskLabelFilter filters disk if all its partitions are excluded.
type diskLabelFilter struct {
	filter *partLabelFilter
}

func RegisterLabelFilter(filters string) *Filter {
	f := &partLabelFilter{}

	f.excludeLabels = []string{}

	if filters != "" {
		f.excludeLabels = append(f.excludeLabels, strings.Split(filters, ",")...)
	}

	return &Filter{
		Name:       labelFilterName,
		PartFilter: f,
		DiskFilter: &diskLabelFilter{filter: f},
	}
}

// Exclude returns true if filesystem label matches the pattern
func (f *partLabelFilter) Exclude(part *block.Partition) bool {
	for _, pattern := range f.excludeLabels {
		if pattern == "" || part.Label == "" {
			return false
		}
		ok, err := filepath.Match(pattern, part.Label)
		if err != nil {
			logrus.Errorf("failed to perform filesystem label matching on disk %s for pattern %s: %s", part.Name, pattern, err.Error())
			return true
		}
		if ok {
			return true
		}
	}
	return false
}

// Exclude returns true if all partitions of the disk are excluded.
func (f *diskLabelFilter) Exclude(disk *block.Disk) bool {
	if len(disk.Partitions) > 0 {
		for _, part := range disk.Partitions {
			if !f.filter.Exclude(part) {
				return false
			}
		}
		return true
	}
	return false
}
