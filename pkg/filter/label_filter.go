package filter

import (
	"path/filepath"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/sirupsen/logrus"
)

const (
	labelFilterName = "label filter"
)

// partLabelFilter filters disk based on given filesystem label patterns
type partLabelFilter struct {
	labels []string
}

// diskLabelFilter filters disk if all its partitions match.
type diskLabelFilter struct {
	filter *partLabelFilter
}

func RegisterLabelFilter(filters ...string) *Filter {
	f := &partLabelFilter{}
	for _, filter := range filters {
		if filter != "" {
			f.labels = append(f.labels, filter)
		}
	}
	return &Filter{
		Name:       labelFilterName,
		PartFilter: f,
		DiskFilter: &diskLabelFilter{filter: f},
	}
}

// Match returns true if filesystem label matches the pattern
func (f *partLabelFilter) Match(part *block.Partition) bool {
	for _, pattern := range f.labels {
		if pattern == "" || part.Label == "" {
			return false
		}
		ok, err := filepath.Match(pattern, part.Label)
		if err != nil {
			logrus.Errorf("failed to perform filesystem label matching on disk %s for pattern %s: %s", part.Name, pattern, err.Error())
			return false
		}
		if ok {
			return true
		}
	}
	return false
}

// Match returns true if all partitions of the disk match.
func (f *diskLabelFilter) Match(disk *block.Disk) bool {
	if len(disk.Partitions) > 0 {
		for _, part := range disk.Partitions {
			if !f.filter.Match(part) {
				return false
			}
		}
		return true
	}
	return false
}
