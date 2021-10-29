package filter

import (
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	vendorFilterName            = "vendor filter"
	vendorFilterDefaultLonghorn = "longhorn"
)

var (
	defaultExcludedVendors = []string{vendorFilterDefaultLonghorn}
)

type vendorFilter struct {
	vendors []string
}

func RegisterVendorFilter(filters ...string) *Filter {
	vf := &vendorFilter{}
	for _, filter := range filters {
		if filter != "" {
			vf.vendors = append(vf.vendors, filter)
		}
	}
	return &Filter{
		Name:       vendorFilterName,
		DiskFilter: vf,
	}
}

// Match returns true if vendor of the disk is matched
func (vf *vendorFilter) Match(blockDevice *block.Disk) bool {
	if blockDevice.Vendor == "" && blockDevice.BusPath == "" {
		return false
	}
	return util.ContainsIgnoredCase(vf.vendors, blockDevice.Vendor) ||
		util.ContainsIgnoredCase(vf.vendors, blockDevice.BusPath)
}
