package block

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ParseMountEntry(t *testing.T) {
	var testCases = []struct {
		name     string
		given    string
		expected *mountEntry
	}{
		{
			name:     "empty",
			given:    "",
			expected: nil,
		},
		{
			name:     "missing leading slash",
			given:    "🤡",
			expected: nil,
		},
		{
			name:     "missing mount options",
			given:    "/dev/sda1 /mnt/sda1 ext4",
			expected: nil,
		},
		{
			name:  "simple mount entry",
			given: "/dev/sda1 /mnt/sda1 ext4 rw,relatime",
			expected: &mountEntry{
				Partition:      "/dev/sda1",
				Mountpoint:     "/mnt/sda1",
				FilesystemType: "ext4",
				Options:        []string{"rw", "relatime"},
			},
		},
		{
			name:  "whitespace encoding",
			given: "/dev/sda1 /mnt/\\011\\012\\040\\\\ ext4 rw,relatime",
			expected: &mountEntry{
				Partition:      "/dev/sda1",
				Mountpoint:     "/mnt/\t\n \\",
				FilesystemType: "ext4",
				Options:        []string{"rw", "relatime"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			entry := parseMountEntry(tc.given)
			assert.Equal(t, tc.expected, entry)
		})
	}
}

func Test_GenerateDiskGUID(t *testing.T) {
	var testCases = []struct {
		name     string
		given    *Disk
		expected string
	}{
		{
			name:     "empty",
			given:    &Disk{},
			expected: "",
		},
		{
			name: "PtUUID",
			given: &Disk{
				PtUUID: "PtUUID",
			},
			expected: "PtUUID",
		},
		{
			name: "PtUUID UUID",
			given: &Disk{
				UUID:   "UUID",
				PtUUID: "PtUUID",
			},
			expected: "UUID",
		},
		{
			name: "PtUUID UUID WWN",
			given: &Disk{
				WWN:          "WWN",
				Vendor:       "Vendor",
				Model:        "Model",
				SerialNumber: "SerialNumber",
				UUID:         "UUID",
				PtUUID:       "PtUUID",
			},
			expected: "WWNVendorModelSerialNumber",
		},
	}

	setMakeHashGUIDForTesting()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			guid := GenerateDiskGUID(tc.given)
			assert.Equal(t, tc.expected, guid)
		})
	}

	t.Cleanup(resetMakeHashGUIDForTesting)
}
func Test_GeneratePartitionGUID(t *testing.T) {
	var testCases = []struct {
		name     string
		given    *Partition
		expected string
	}{
		{
			name:     "empty",
			given:    &Partition{},
			expected: "",
		},
		{
			name: "UUID",
			given: &Partition{
				UUID: "UUID",
			},
			expected: "UUID",
		},
	}

	setMakeHashGUIDForTesting()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			guid := GeneratePartitionGUID(tc.given)
			assert.Equal(t, tc.expected, guid)
		})
	}

	t.Cleanup(resetMakeHashGUIDForTesting)
}
