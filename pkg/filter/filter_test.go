package filter

import (
	"testing"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/stretchr/testify/assert"
)

func Test_pathFilter(t *testing.T) {
	type input struct {
		disk *block.Disk
		path string
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
				path: "/mnt/exclude",
			},
			expected: true,
		},
		{
			name: "empty disk and valid path",
			given: input{
				disk: &block.Disk{},
				path: "/mnt/exclude",
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
				path: "",
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
				path: "/mnt/exclude",
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
				path: "/mnt/exclude",
			},
			expected: true,
		},
		{
			name: "root exclusion",
			given: input{
				disk: &block.Disk{
					FileSystemInfo: block.FileSystemInfo{
						MountPoint: "/",
					},
				},
				path: "",
			},
			expected: true,
		},
		{
			name: "partition inclusion",
			given: input{
				disk: &block.Disk{
					Partitions: []*block.Partition{
						{
							FileSystemInfo: block.FileSystemInfo{
								MountPoint: "/mnt/include",
							},
						},
					},
				},
				path: "/mnt/exclude",
			},
			expected: false,
		},
		{
			name: "partition exclusion",
			given: input{
				disk: &block.Disk{
					Partitions: []*block.Partition{
						{
							FileSystemInfo: block.FileSystemInfo{
								MountPoint: "/mnt/exclude",
							},
						},
					},
				},
				path: "/mnt/exclude",
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RegisterPathFilter(tc.given.path)
			result := filter.ApplyFilter(tc.given.disk)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_vendorFilter(t *testing.T) {
	type input struct {
		disk   *block.Disk
		vendor string
	}
	var testCases = []struct {
		name     string
		given    input
		expected bool
	}{
		{
			name: "valid disk and valid vendor",
			given: input{
				disk:   &block.Disk{Vendor: "myvendor"},
				vendor: "myvendor",
			},
			expected: true,
		},
		{
			name: "empty disk and valid vendor",
			given: input{
				disk:   &block.Disk{},
				vendor: "myvendor",
			},
			expected: false,
		},
		{
			name: "valid disk and empty vendor",
			given: input{
				disk:   &block.Disk{Vendor: "myvendor"},
				vendor: "",
			},
			expected: false,
		},
		{
			name: "valid disk and valid vendor but not match",
			given: input{
				disk:   &block.Disk{Vendor: "yourvendor"},
				vendor: "myvendor",
			},
			expected: false,
		},
		{
			name: "ignore cases",
			given: input{
				disk:   &block.Disk{Vendor: "MyVendor"},
				vendor: "myvendor",
			},
			expected: true,
		},
		{
			name: "longhorn bus path",
			given: input{
				disk:   &block.Disk{Vendor: "yourvendor", BusPath: "longhron"},
				vendor: "myvendor",
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := RegisterVendorFilter(tc.given.vendor)
			result := filter.ApplyFilter(tc.given.disk)
			assert.Equal(t, tc.expected, result)
		})
	}
}
