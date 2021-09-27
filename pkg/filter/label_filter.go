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

// labelFilter filters disk based on given filesystem label patterns
type labelFilter struct {
	excludeLabels []string
}

func RegisterLabelFilter(filters string) *Filter {
	vf := &labelFilter{}

	vf.excludeLabels = []string{}

	if filters != "" {
		vf.excludeLabels = append(vf.excludeLabels, strings.Split(filters, ",")...)
	}

	return &Filter{
		Name:       labelFilterName,
		PartFilter: vf,
	}
}

// Exclude returns true if filesystem label matches the pattern
func (vf *labelFilter) Exclude(part *block.Partition) bool {
	for _, pattern := range vf.excludeLabels {
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
