package udev

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/controller/blockdevice"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/udev/netlink"
)

func TestUdev_InjectError(t *testing.T) {
	opt := &option.Option{
		Namespace:              "default",
		NodeName:               "test-node",
		InjectUdevMonitorError: true,
	}
	blockInfo, err := block.New()
	require.NoError(t, err)
	scanner := &blockdevice.Scanner{
		BlockInfo: blockInfo,
		Cond:      sync.NewCond(&sync.Mutex{}),
	}
	u := NewUdev(opt, scanner)

	errChan := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	u.monitor(ctx, errChan)

	select {
	case err := <-errChan:
		require.Error(t, err)
		assert.Equal(t, "testing error", err.Error())
		assert.False(t, u.injectError)
	case <-ctx.Done():
		t.Fatal("timed out waiting for injected error")
	}
}

func TestUdev_ActionHandler_IgnoreNonDisk(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	blockInfo, err := block.New()
	require.NoError(t, err)
	scanner := &blockdevice.Scanner{
		BlockInfo: blockInfo,
		Cond:      sync.NewCond(&sync.Mutex{}),
	}
	u := NewUdev(opt, scanner)
	u.ActionHandler(nil)

	// Non-disk event (e.g., partition or cdrom).
	nonDiskEvent := &netlink.UEvent{
		Action: netlink.ActionAdd,
		Env: map[string]string{
			"DEVNAME": "/dev/sda1",
			"DEVTYPE": "partition",
		},
	}
	u.ActionHandler(nonDiskEvent)
}

func TestUdev_ActionHandler_RemoveDisk(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	blockInfo, err := block.New()
	require.NoError(t, err)
	scanner := &blockdevice.Scanner{
		BlockInfo: blockInfo,
		Cond:      sync.NewCond(&sync.Mutex{}),
	}
	u := NewUdev(opt, scanner)

	removeEvent := &netlink.UEvent{
		Action: netlink.ActionRemove,
		Env: map[string]string{
			"DEVNAME":   "/dev/sdz",
			"DEVTYPE":   "disk",
			"ID_MODEL":  "TestModel",
			"ID_VENDOR": "TestVendor",
			"ID_SERIAL": "TestSerial123",
		},
	}
	// ActionHandler should handle remove gracefully without panicking.
	u.ActionHandler(removeEvent)

	// Remove multipath dm- device (triggers dm- branch).
	removeDMEvent := &netlink.UEvent{
		Action: netlink.ActionRemove,
		Env: map[string]string{
			"DEVNAME": "/dev/dm-0",
			"DEVTYPE": "disk",
		},
	}
	u.ActionHandler(removeDMEvent)

	// Action other than add/remove (e.g. change) should be ignored.
	changeEvent := &netlink.UEvent{
		Action: netlink.ActionChange,
		Env: map[string]string{
			"DEVNAME": "/dev/sda",
			"DEVTYPE": "disk",
		},
	}
	u.ActionHandler(changeEvent)

	// ActionAdd event.
	addEvent := &netlink.UEvent{
		Action: netlink.ActionAdd,
		Env: map[string]string{
			"DEVNAME": "/dev/sda",
			"DEVTYPE": "disk",
		},
	}
	u.ActionHandler(addEvent)
}

func TestUdev_ActionHandler_EdgeCases(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	u := NewUdev(opt, nil)

	// Event with empty dev path should be skipped without panic.
	emptyPathEvent := &netlink.UEvent{
		Action: netlink.ActionAdd,
		Env: map[string]string{
			"DEVTYPE": "disk",
		},
	}
	u.ActionHandler(emptyPathEvent)

	// Event with scanner == nil should not panic.
	addEvent := &netlink.UEvent{
		Action: netlink.ActionAdd,
		Env: map[string]string{
			"DEVNAME": "/dev/sda",
			"DEVTYPE": "disk",
		},
	}
	u.ActionHandler(addEvent)
}

func TestUdev_ActionHandler_Rescan(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	var (
		mu      sync.Mutex
		waiting bool
	)
	cond := sync.NewCond(&mu)
	scanner := &blockdevice.Scanner{
		Cond: cond,
	}
	u := NewUdev(opt, scanner)

	ready := make(chan struct{})
	woken := make(chan struct{})

	go func() {
		mu.Lock()
		waiting = true
		close(ready)
		cond.Wait()
		mu.Unlock()
		close(woken)
	}()

	// 1. Wait until the goroutine acquires the lock.
	<-ready

	// 2. Acquiring the lock guarantees that cond.Wait() has released it and is waiting for Signal().
	mu.Lock()
	assert.True(t, waiting)
	mu.Unlock()

	// 3. Trigger rescan action, which must signal cond.
	rescanEvent := &netlink.UEvent{
		Action: netlink.ActionRescan,
	}
	u.ActionHandler(rescanEvent)

	// 4. Deterministically wait for the goroutine to be woken by Signal().
	select {
	case <-woken:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scanner to be woken by ActionRescan")
	}
}

func TestUdev_SpawnMonitor_Cancel(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	scanner := &blockdevice.Scanner{
		Cond: sync.NewCond(&sync.Mutex{}),
	}
	u := NewUdev(opt, scanner)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)

	done := make(chan struct{})
	go func() {
		u.spawnMonitor(ctx, errChan)
		close(done)
	}()

	// Cancelling context should make spawnMonitor return cleanly.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for spawnMonitor to exit")
	}
}

func TestNetlinkLibudevMagic(t *testing.T) {
	// Verify byte ordering assumptions for libudev magic number.
	magic := make([]byte, 4)
	binary.BigEndian.PutUint32(magic, 0xfeedcafe)
	assert.Equal(t, uint32(0xfeedcafe), binary.BigEndian.Uint32(magic))
}

func TestUpdateDiskFromUdev(t *testing.T) {
	t.Run("populate all fields and serial precedence", func(t *testing.T) {
		disk := &block.Disk{}
		dev := Device{
			UdevFsUUID:         "fs-uuid-1",
			UdevPartTableUUID:  "pt-uuid-1",
			UdevModel:          "ModelX",
			UdevVendor:         "VendorY",
			UdevSerialShort:    "SHORT123",
			UdevSerialNumber:   "LONG123",
			UdevDMSerialNumber: "DM123",
			UdevWWN:            "WWN_PRI",
			UdevDMWWN:          "WWN_SEC",
		}
		dev.UpdateDiskFromUdev(disk)

		assert.Equal(t, "fs-uuid-1", disk.UUID)
		assert.Equal(t, "pt-uuid-1", disk.PtUUID)
		assert.Equal(t, "ModelX", disk.Model)
		assert.Equal(t, "VendorY", disk.Vendor)
		assert.Equal(t, "SHORT123", disk.SerialNumber)
		assert.Equal(t, "WWN_PRI", disk.WWN)
	})

	t.Run("serial fallback precedence", func(t *testing.T) {
		disk := &block.Disk{}
		dev := Device{
			UdevSerialNumber:   "LONG123",
			UdevDMSerialNumber: "DM123",
			UdevDMWWN:          "WWN_SEC",
		}
		dev.UpdateDiskFromUdev(disk)
		assert.Equal(t, "LONG123", disk.SerialNumber)
		assert.Equal(t, "WWN_SEC", disk.WWN)

		// DM serial fallback.
		disk2 := &block.Disk{}
		dev2 := Device{
			UdevDMSerialNumber: "DM123",
		}
		dev2.UpdateDiskFromUdev(disk2)
		assert.Equal(t, "DM123", disk2.SerialNumber)
	})

	t.Run("missing vendor does not overwrite existing vendor", func(t *testing.T) {
		disk := &block.Disk{
			Vendor: "PreservedVendor",
			Model:  "PreservedModel",
		}
		dev := Device{
			UdevModel: "NewModel",
		}
		dev.UpdateDiskFromUdev(disk)
		assert.Equal(t, "PreservedVendor", disk.Vendor)
		assert.Equal(t, "NewModel", disk.Model)
	})

	t.Run("nil disk does not panic", func(t *testing.T) {
		dev := Device{
			UdevModel: "ModelX",
		}
		assert.NotPanics(t, func() {
			dev.UpdateDiskFromUdev(nil)
		})
	})
}

func TestDevice_GetDevName(t *testing.T) {
	t.Run("with DEVNAME", func(t *testing.T) {
		dev := Device{
			UdevDevname: "/dev/sda",
			UdevDevpath: "/devices/pci0000:00/block/sda",
		}
		assert.Equal(t, "/dev/sda", dev.GetDevName())
	})

	t.Run("DEVNAME without /dev/ prefix", func(t *testing.T) {
		dev := Device{
			UdevDevname: "sdb",
			UdevDevpath: "/devices/pci0000:00/block/sdb",
		}
		assert.Equal(t, "/dev/sdb", dev.GetDevName())
	})

	t.Run("fallback to DEVPATH when DEVNAME is empty", func(t *testing.T) {
		dev := Device{
			UdevDevpath: "/devices/pci0000:00/0000:00:1f.2/ata1/host0/target0:0:0/0:0:0:0/block/nvme0n1",
		}
		assert.Equal(t, "/dev/nvme0n1", dev.GetDevName())
	})

	t.Run("empty both", func(t *testing.T) {
		dev := Device{}
		assert.Equal(t, "", dev.GetDevName())
	})
}

func TestGetProcMountInfoPath(t *testing.T) {
	path := getProcMountInfoPath()
	assert.NotEmpty(t, path)
	assert.Contains(t, []string{
		procMountInfo,
		"/proc/1/mountinfo",
		"/proc/self/mountinfo",
	}, path)
}

func TestUdev_SpawnMountWatcher_Cancel(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	scanner := &blockdevice.Scanner{
		Cond: sync.NewCond(&sync.Mutex{}),
	}
	u := NewUdev(opt, scanner)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)

	done := make(chan struct{})
	go func() {
		u.spawnMountWatcher(ctx, errChan)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for spawnMountWatcher to exit")
	}
}

func TestUdev_WatchMounts_Cancel(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	scanner := &blockdevice.Scanner{
		Cond: sync.NewCond(&sync.Mutex{}),
	}
	u := NewUdev(opt, scanner)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)

	done := make(chan struct{})
	go func() {
		u.watchMounts(ctx, errChan)
		close(done)
	}()

	// Cancelling context should make watchMounts exit cleanly within poll timeout (1s).
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watchMounts to exit on cancel")
	}

	// Verify no error was sent on clean cancel.
	select {
	case err := <-errChan:
		t.Fatalf("unexpected error on cancel: %v", err)
	default:
	}
}

func TestUdev_WatchMounts_NilScanner(t *testing.T) {
	opt := &option.Option{
		Namespace: "default",
		NodeName:  "test-node",
	}
	u := NewUdev(opt, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)

	done := make(chan struct{})
	go func() {
		u.watchMounts(ctx, errChan)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watchMounts with nil scanner to exit")
	}
}
