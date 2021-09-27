package filter

import (
	"github.com/sirupsen/logrus"

	"github.com/harvester/node-disk-manager/pkg/block"
)

type Filter struct {
	Name       string
	DiskFilter DiskFilter
	PartFilter PartFilter
}

func SetNDMFilters(vendorString, pathString, labelString string) []*Filter {
	logrus.Info("register ndm filters")
	listFilter := make([]*Filter, 0)

	vendorFilter := RegisterVendorFilter(vendorString)
	pathFilter := RegisterPathFilter(pathString)
	labelFilter := RegisterLabelFilter(labelString)
	listFilter = append(listFilter, vendorFilter, pathFilter, labelFilter)

	return listFilter
}

type DiskFilter interface {
	// Exclude returns true if passing disk does not match with exclude value
	Exclude(disk *block.Disk) bool
}

type PartFilter interface {
	// Exclude returns true if passing partition does not match with exclude value
	Exclude(part *block.Partition) bool
}

func (f *Filter) ApplyDiskFilter(disk *block.Disk) bool {
	if f.DiskFilter != nil {
		return f.DiskFilter.Exclude(disk)
	}
	return false
}

func (f *Filter) ApplyPartFilter(part *block.Partition) bool {
	if f.PartFilter != nil {
		return f.PartFilter.Exclude(part)
	}
	return false
}
