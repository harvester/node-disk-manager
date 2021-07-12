package filter

import (
	"github.com/sirupsen/logrus"

	"github.com/longhorn/node-disk-manager/pkg/block"
)

type Filter struct {
	Name      string
	Interface FilterInterface
}

func SetDNMFilters(vendorString, pathString string) []*Filter {
	logrus.Info("register ndm filters")
	listFilter := make([]*Filter, 0)

	vendorFilter := RegisterVendorFilter(vendorString)
	pathFilter := RegisterPathFilter(pathString)
	listFilter = append(listFilter, vendorFilter, pathFilter)

	return listFilter
}

type FilterInterface interface {
	Filters
}

type Filters interface {
	Exclude(disk *block.Disk) bool // exclude returns true if passing disk does not match with exclude value
}

func (f *Filter) ApplyFilter(disk *block.Disk) bool {
	return f.Interface.Exclude(disk)
}
