package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
)

func TestIsPVOwnedByVG(t *testing.T) {
	vg := &diskv1.LVMVolumeGroup{
		Spec: diskv1.VolumeGroupSpec{
			VgName:  "vg01",
			Devices: map[string]string{"bd1": "/dev/sdb"},
		},
	}

	tests := []struct {
		name     string
		pvVG     string
		vg       *diskv1.LVMVolumeGroup
		device   string
		expected bool
	}{
		{"PV in the matching VG", "vg01", vg, "bd1", true},
		{"PV in another VG", "vg02", vg, "bd1", false},
		{"PV without VG, device listed in CR (VG creation in progress)", "", vg, "bd1", true},
		{"PV without VG, device not listed in CR (leftover PV)", "", vg, "bd2", false},
		{"PV without VG, CR without devices", "", &diskv1.LVMVolumeGroup{}, "bd1", false},
		{"no CR", "vg01", nil, "bd1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isPVOwnedByVG(tt.pvVG, tt.vg, tt.device))
		})
	}
}
