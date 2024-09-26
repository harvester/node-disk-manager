package filter

import (
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

const (
	vendorFilterName            = "vendor filter"
	vendorFilterDefaultLonghorn = "longhorn"
	// This is a hack to make sure we skip active longhorn v2 volumes,
	// which appear as /dev/nvme* devices.  They don't have a vendor,
	// but do specify ID_MODEL=SPDK bdev Controller...
	modelFilterDefaultSPDK = "SPDK bdev Controller"
)

var (
	defaultExcludedVendors = []string{vendorFilterDefaultLonghorn, modelFilterDefaultSPDK}
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
	if blockDevice.Vendor != "" && utils.MatchesIgnoredCase(vf.vendors, blockDevice.Vendor) {
		return true
	}
	if blockDevice.BusPath != "" && utils.ContainsIgnoredCase(vf.vendors, blockDevice.BusPath) {
		return true
	}
	if blockDevice.Model != "" && utils.MatchesIgnoredCase(vf.vendors, blockDevice.Model) {
		return true
	}
	return false
}
