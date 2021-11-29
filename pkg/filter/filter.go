package filter

import (
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/harvester/node-disk-manager/pkg/block"
)

type Filter struct {
	Name       string
	DiskFilter DiskFilter
	PartFilter PartFilter
}

func SetAutoProvisionFilters(devPathString string) []*Filter {
	logrus.Info("register auto provision filters")

	devPaths := strings.Split(devPathString, ",")
	devPathFilter := RegisterDevicePathFilter(devPaths...)
	return []*Filter{devPathFilter}
}

func SetExcludeFilters(vendorString, pathString, labelString string) []*Filter {
	logrus.Info("register exclude filters")

	vendors := strings.Split(vendorString, ",")
	vendors = append(vendors, defaultExcludedVendors...)
	vendorFilter := RegisterVendorFilter(vendors...)

	paths := strings.Split(pathString, ",")
	paths = append(paths, defaultExcludedPaths...)
	pathFilter := RegisterPathFilter(paths...)

	labels := strings.Split(labelString, ",")
	labelFilter := RegisterLabelFilter(labels...)

	partTypeFilters := RegisterPartTypeFilter(defaultExcludedPartTypes...)

	return []*Filter{vendorFilter, pathFilter, labelFilter, partTypeFilters}
}

type DiskFilter interface {
	// Match returns true if passing disk matches with the value
	Match(disk *block.Disk) bool
}

type PartFilter interface {
	// Match returns true if passing partition matches with the value
	Match(part *block.Partition) bool
}

func (f *Filter) ApplyDiskFilter(disk *block.Disk) bool {
	if f.DiskFilter != nil {
		return f.DiskFilter.Match(disk)
	}
	return false
}

func (f *Filter) ApplyPartFilter(part *block.Partition) bool {
	if f.PartFilter != nil {
		return f.PartFilter.Match(part)
	}
	return false
}
