package udev

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/harvester/node-disk-manager/pkg/block"
)

func TestDevice(t *testing.T) {
	disk := Device{"ACTION": "add", "DEVTYPE": "disk", "DEVNAME": "/dev/sda"}
	assert.Equal(t, "add", disk.Action())
	assert.True(t, disk.IsDisk())
	assert.Equal(t, "/dev/sda", disk.GetDevName())

	partition := Device{"DEVTYPE": "partition", "DEVNAME": "/dev/sda1"}
	assert.False(t, partition.IsDisk())
	assert.False(t, Device{}.IsDisk())
	assert.Empty(t, Device{}.GetDevName())
	assert.Empty(t, Device{}.Action())
}

func TestDevice_UpdateDiskFromUdev(t *testing.T) {
	tests := []struct {
		name   string
		device Device
		before block.Disk
		want   block.Disk
	}{
		{
			name:   "empty device keeps the disk",
			device: Device{},
			before: block.Disk{Vendor: "v", Model: "m", SerialNumber: "s", WWN: "w"},
			want:   block.Disk{Vendor: "v", Model: "m", SerialNumber: "s", WWN: "w"},
		},
		{
			name: "all properties",
			device: Device{
				"ID_FS_UUID": "uuid", "ID_PART_TABLE_UUID": "ptuuid", "ID_MODEL": "model",
				"ID_VENDOR": "vendor", "ID_SERIAL": "serial", "ID_WWN": "wwn",
			},
			want: block.Disk{UUID: "uuid", PtUUID: "ptuuid", Model: "model", Vendor: "vendor", SerialNumber: "serial", WWN: "wwn"},
		},
		{
			name:   "ID_SERIAL_SHORT wins over ID_SERIAL and DM_SERIAL",
			device: Device{"ID_SERIAL_SHORT": "short", "ID_SERIAL": "long", "DM_SERIAL": "dm"},
			want:   block.Disk{SerialNumber: "short"},
		},
		{
			name:   "ID_SERIAL wins over DM_SERIAL",
			device: Device{"ID_SERIAL": "long", "DM_SERIAL": "dm"},
			want:   block.Disk{SerialNumber: "long"},
		},
		{
			name:   "multipath identity",
			device: Device{"DM_SERIAL": "dm", "DM_WWN": "dmwwn"},
			want:   block.Disk{SerialNumber: "dm", WWN: "dmwwn"},
		},
		{
			name:   "ID_WWN_WITH_EXTENSION wins over ID_WWN",
			device: Device{"ID_WWN_WITH_EXTENSION": "wwnext", "ID_WWN": "wwn"},
			want:   block.Disk{WWN: "wwnext"},
		},
		{
			name:   "ID_WWN wins over DM_WWN",
			device: Device{"ID_WWN": "wwn", "DM_WWN": "dmwwn"},
			want:   block.Disk{WWN: "wwn"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disk := tt.before
			tt.device.UpdateDiskFromUdev(&disk)
			assert.Equal(t, tt.want, disk)
		})
	}
}
