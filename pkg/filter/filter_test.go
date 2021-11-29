package filter

import (
	"testing"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/stretchr/testify/assert"
)

func Test_devicePathFilter(t *testing.T) {
	type input struct {
		disk     *block.Disk
		devPaths []string
	}
	var testCases = []struct {
		name     string
		given    input
		expected bool
	}{
		{
			name: "valid disk and matched device path",
			given: input{
				disk: &block.Disk{
					Name: "sda",
				},
				devPaths: []string{"/dev/sda"},
			},
			expected: true,
		},
		{
			name: "empty disk and matched device path",
			given: input{
				disk:     &block.Disk{},
				devPaths: []string{"/dev/sda"},
			},
			expected: false,
		},
		{
			name: "valid disk and empty device path",
			given: input{
				disk: &block.Disk{
					Name: "sda",
				},
				devPaths: nil,
			},
			expected: false,
		},
		{
			name: "valid disk and valid device path but mismatch",
			given: input{
				disk: &block.Disk{
					Name: "sda",
				},
				devPaths: []string{"/dev/nvme0n1"},
			},
			expected: false,
		},
		{
			name: "glob",
			given: input{
				disk: &block.Disk{
					Name: "sda",
				},
				devPaths: []string{"/dev/sd*"},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RegisterDevicePathFilter(tc.given.devPaths...)
			result := filter.ApplyDiskFilter(tc.given.disk)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_labelFilter(t *testing.T) {
	type input struct {
		part   *block.Partition
		labels []string
	}
	var testCases = []struct {
		name     string
		given    input
		expected bool
	}{
		{
			name: "valid partition and matched label",
			given: input{
				part: &block.Partition{
					Label: "match",
				},
				labels: []string{"match"},
			},
			expected: true,
		},
		{
			name: "empty partition and matched label",
			given: input{
				part:   &block.Partition{},
				labels: []string{"match"},
			},
			expected: false,
		},
		{
			name: "valid partition and empty label",
			given: input{
				part: &block.Partition{
					Label: "match",
				},
				labels: nil,
			},
			expected: false,
		},
		{
			name: "valid partition and valid label but mismatch",
			given: input{
				part: &block.Partition{
					Label: "match",
				},
				labels: []string{"mismatch"},
			},
			expected: false,
		},
		{
			name: "glob",
			given: input{
				part: &block.Partition{
					Label: "match",
				},
				labels: []string{"m*c?"},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RegisterLabelFilter(tc.given.labels...)
			result := filter.ApplyPartFilter(tc.given.part)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_pathFilter(t *testing.T) {
	type input struct {
		disk  *block.Disk
		paths []string
	}
	var testCases = []struct {
		name     string
		given    input
		expected bool
	}{
		{
			name: "valid disk and valid path",
			given: input{
				disk: &block.Disk{
					FileSystemInfo: block.FileSystemInfo{
						MountPoint: "/mnt/exclude",
					},
				},
				paths: []string{"/mnt/exclude"},
			},
			expected: true,
		},
		{
			name: "empty disk and valid path",
			given: input{
				disk:  &block.Disk{},
				paths: []string{"/mnt/exclude"},
			},
			expected: false,
		},
		{
			name: "valid disk and empty path",
			given: input{
				disk: &block.Disk{
					FileSystemInfo: block.FileSystemInfo{
						MountPoint: "/mnt/exclude",
					},
				},
				paths: nil,
			},
			expected: false,
		},
		{
			name: "valid disk and valid path but not match",
			given: input{
				disk: &block.Disk{
					FileSystemInfo: block.FileSystemInfo{
						MountPoint: "/mnt/include",
					},
				},
				paths: []string{"/mnt/exclude"},
			},
			expected: false,
		},
		{
			name: "ignore cases",
			given: input{
				disk: &block.Disk{
					FileSystemInfo: block.FileSystemInfo{
						MountPoint: "/MnT/eXcLuDe",
					},
				},
				paths: []string{"/mnt/exclude"},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RegisterPathFilter(tc.given.paths...)
			result := filter.ApplyDiskFilter(tc.given.disk)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_vendorFilter(t *testing.T) {
	type input struct {
		disk    *block.Disk
		vendors []string
	}
	var testCases = []struct {
		name     string
		given    input
		expected bool
	}{
		{
			name: "valid disk and valid vendor",
			given: input{
				disk:    &block.Disk{Vendor: "myvendor"},
				vendors: []string{"myvendor"},
			},
			expected: true,
		},
		{
			name: "empty disk and valid vendor",
			given: input{
				disk:    &block.Disk{},
				vendors: []string{"myvendor"},
			},
			expected: false,
		},
		{
			name: "valid disk and empty vendor",
			given: input{
				disk:    &block.Disk{Vendor: "myvendor"},
				vendors: nil,
			},
			expected: false,
		},
		{
			name: "valid disk and valid vendor but not match",
			given: input{
				disk:    &block.Disk{Vendor: "yourvendor"},
				vendors: []string{"myvendor"},
			},
			expected: false,
		},
		{
			name: "ignore cases",
			given: input{
				disk:    &block.Disk{Vendor: "MyVendor"},
				vendors: []string{"myvendor"},
			},
			expected: true,
		},
		{
			name: "longhorn bus path",
			given: input{
				disk:    &block.Disk{BusPath: "ip-10.52.0.122:3260-iscsi-iqn.2019-10.io.longhorn:pvc-ab9af96e-60ef-400f-84f7-2f6eab68ab56-lun-1"},
				vendors: []string{"LongHorN"},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RegisterVendorFilter(tc.given.vendors...)
			result := filter.ApplyDiskFilter(tc.given.disk)
			assert.Equal(t, tc.expected, result)
		})
	}
}
func Test_partTypeFilter(t *testing.T) {
	type input struct {
		part      *block.Partition
		disk      *block.Disk
		partTypes []string
	}
	var testCases = []struct {
		name     string
		given    input
		expected bool
	}{
		{
			name: "valid partition and matched parttype",
			given: input{
				part: &block.Partition{
					PartType: "match",
				},
				partTypes: []string{"match"},
			},
			expected: true,
		},
		{
			name: "empty partition and matched parttype",
			given: input{
				part:      &block.Partition{},
				partTypes: []string{"match"},
			},
			expected: false,
		},
		{
			name: "valid partition and empty parttype",
			given: input{
				part: &block.Partition{
					PartType: "match",
				},
				partTypes: nil,
			},
			expected: false,
		},
		{
			name: "valid partition and valid parttype but mismatch",
			given: input{
				part: &block.Partition{
					PartType: "match",
				},
				partTypes: []string{"mismatch"},
			},
			expected: false,
		},
		{
			name: "valid disk with partition that matches parttype",
			given: input{
				disk:      &block.Disk{Partitions: []*block.Partition{{PartType: "match"}}},
				partTypes: []string{"match"},
			},
			expected: true,
		},
		{
			name: "valid disk with partition that mismatches parttype",
			given: input{
				disk:      &block.Disk{Partitions: []*block.Partition{{PartType: "match"}}},
				partTypes: []string{"mismatch"},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RegisterPartTypeFilter(tc.given.partTypes...)
			if tc.given.part != nil {
				result := filter.ApplyPartFilter(tc.given.part)
				assert.Equal(t, tc.expected, result)
			}
			if tc.given.disk != nil {
				result := filter.ApplyDiskFilter(tc.given.disk)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
