package filter

import (
	"testing"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/stretchr/testify/assert"
)

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
				disk:    &block.Disk{Vendor: "yourvendor", BusPath: "longhorn"},
				vendors: []string{"longhorn"},
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
