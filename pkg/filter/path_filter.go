package filter

import (
	"strings"

	"github.com/longhorn/node-disk-manager/pkg/block"
	"github.com/longhorn/node-disk-manager/pkg/util"
)

const (
	pathFilterName        = "path filter"
	pathFilterDefaultRoot = "/"
)

var (
	excludePaths         = ""
	defaultExcludedPaths = []string{pathFilterDefaultRoot}
)

type pathFilter struct {
	excludePaths []string
}

func RegisterPathFilter(filters string) *Filter {
	vf := &pathFilter{}

	// add default exclude paths
	vf.excludePaths = append(vf.excludePaths, defaultExcludedPaths...)

	if filters != "" {
		vf.excludePaths = append(vf.excludePaths, strings.Split(filters, ",")...)
	}
	return &Filter{
		Name:      pathFilterName,
		Interface: vf,
	}
}

// Exclude returns true if mount path of the disk or partitions is matched
func (pf *pathFilter) Exclude(disk *block.Disk) bool {
	if len(pf.excludePaths) == 0 {
		return true
	}

	if util.ContainsIgnoredCase(pf.excludePaths, disk.FileSystemInfo.MountPoint) {
		return true
	}

	for _, part := range disk.Partitions {
		if util.ContainsIgnoredCase(pf.excludePaths, part.FileSystemInfo.MountPoint) {
			return true
		}
	}

	return false
}
