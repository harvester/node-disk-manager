package filter

import (
	"strings"

	"github.com/longhorn/node-disk-manager/pkg/block"
	"github.com/longhorn/node-disk-manager/pkg/util"
)

const (
	vendorFilterName            = "vendor filter"
	vendorFilterDefaultLonghorn = "longhorn"
)

var (
	excludeVendors         = ""
	defaultExcludedVendors = []string{vendorFilterDefaultLonghorn}
)

type vendorFilter struct {
	excludeVendors []string
}

func RegisterVendorFilter(filters string) *Filter {
	vf := &vendorFilter{}

	// add default exclude vendors
	vf.excludeVendors = append(vf.excludeVendors, defaultExcludedVendors...)

	if filters != "" {
		vf.excludeVendors = append(vf.excludeVendors, strings.Split(filters, ",")...)
	}

	return &Filter{
		Name:      vendorFilterName,
		Interface: vf,
	}
}

// Exclude returns true if vendor of the disk is matched
func (vf *vendorFilter) Exclude(blockDevice *block.Disk) bool {
	if len(vf.excludeVendors) == 0 {
		return true
	}

	return util.ContainsIgnoredCase(vf.excludeVendors, blockDevice.Vendor) ||
		strings.Contains(blockDevice.BusPath, vendorFilterDefaultLonghorn)
}
