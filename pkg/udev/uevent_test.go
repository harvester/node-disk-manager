package udev

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/option"
)

type fakeScanner struct {
	wakes    atomic.Int32
	excluded map[string]bool
}

func (s *fakeScanner) Wake() { s.wakes.Add(1) }

func (s *fakeScanner) ApplyExcludeFiltersForDisk(disk *block.Disk) bool {
	return s.excluded[disk.Name]
}

type fakeBlockInfo struct {
	block.Info
	mu    sync.Mutex
	disks map[string]*block.Disk
	calls []string
}

func (b *fakeBlockInfo) GetDiskByDevPath(name string) *block.Disk {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, name)
	return b.disks[name]
}

func newTestUdev(opt *option.Option) (*Udev, *fakeScanner, *fakeBlockInfo) {
	scanner := &fakeScanner{excluded: map[string]bool{}}
	blockInfo := &fakeBlockInfo{disks: map[string]*block.Disk{
		"/dev/sdb":  {Name: "sdb"},
		"/dev/sdx":  {Name: "sdx"},
		"/dev/dm-0": {Name: "dm-0"},
	}}
	scanner.excluded["sdx"] = true
	return NewUdev(opt, scanner, blockInfo), scanner, blockInfo
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, 5*time.Millisecond, msg)
}

func TestUdev_Handle(t *testing.T) {
	disk := func(action, name string) Device {
		return Device{"ACTION": action, "DEVTYPE": "disk", "DEVNAME": name}
	}
	tests := []struct {
		name      string
		device    Device
		wantWakes int32
	}{
		{"disk added", disk("add", "/dev/sdb"), 1},
		{"disk removed", disk("remove", "/dev/sdb"), 1},
		{"removed disk is not looked up", disk("remove", "/dev/gone"), 1},
		{"added disk is excluded", disk("add", "/dev/sdx"), 0},
		{"added disk can not be queried", disk("add", "/dev/unknown"), 0},
		{"change is ignored", disk("change", "/dev/sdb"), 0},
		{"bind is ignored", disk("bind", "/dev/sdb"), 0},
		{"no action", Device{"DEVTYPE": "disk", "DEVNAME": "/dev/sdb"}, 0},
		{"partition added", Device{"ACTION": "add", "DEVTYPE": "partition", "DEVNAME": "/dev/sdb1"}, 0},
		{"partition removed", Device{"ACTION": "remove", "DEVTYPE": "partition", "DEVNAME": "/dev/sdb1"}, 0},
		{"no device name", Device{"ACTION": "add", "DEVTYPE": "disk"}, 0},
		{"empty", Device{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, scanner, blockInfo := newTestUdev(&option.Option{})
			u.handle(tt.device)
			assert.Equal(t, tt.wantWakes, scanner.wakes.Load())
			if tt.device.Action() == ActionRemove {
				assert.Empty(t, blockInfo.calls, "a removed device can not be queried")
			}
		})
	}
}

func TestUdev_Handle_MultipathAddIsDelayed(t *testing.T) {
	old := multipathSettleDelay
	multipathSettleDelay = 50 * time.Millisecond
	defer func() { multipathSettleDelay = old }()

	u, scanner, blockInfo := newTestUdev(&option.Option{})
	start := time.Now()
	u.handle(Device{"ACTION": "add", "DEVTYPE": "disk", "DEVNAME": "/dev/dm-0"})

	// handle must not block and must not touch the device before the delay.
	assert.Less(t, time.Since(start), multipathSettleDelay)
	assert.Zero(t, scanner.wakes.Load())
	blockInfo.mu.Lock()
	assert.Empty(t, blockInfo.calls)
	blockInfo.mu.Unlock()

	eventually(t, func() bool { return scanner.wakes.Load() == 1 }, "scanner was not woken")
	assert.GreaterOrEqual(t, time.Since(start), multipathSettleDelay)
}

func TestUdev_Handle_MultipathRemoveIsNotDelayed(t *testing.T) {
	u, scanner, _ := newTestUdev(&option.Option{})
	u.handle(Device{"ACTION": "remove", "DEVTYPE": "disk", "DEVNAME": "/dev/dm-0"})
	assert.Equal(t, int32(1), scanner.wakes.Load())
}

func TestUdev_Monitor_InjectedErrorFailsOnlyOnce(t *testing.T) {
	u, _, _ := newTestUdev(&option.Option{InjectUdevMonitorError: true})

	err := u.monitor(context.Background())
	assert.EqualError(t, err, "testing error")
	assert.False(t, u.injectError.Load())
}

func TestUdev_Monitor_WakesAfterConnectAndStopsOnCancel(t *testing.T) {
	u, scanner, _ := newTestUdev(&option.Option{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- u.monitor(ctx) }()

	// Events may have been missed before the socket was bound: resync.
	select {
	case err := <-errCh:
		t.Skipf("netlink uevent socket not available: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	eventually(t, func() bool { return scanner.wakes.Load() == 1 }, "no wake-up after connect")

	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not return after cancellation")
	}
}

func TestUdev_Run_RestartsFailedWatcher(t *testing.T) {
	old := restartDelay
	restartDelay = 10 * time.Millisecond
	defer func() { restartDelay = old }()

	u, _, _ := newTestUdev(&option.Option{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		u.run(ctx, "test", func(context.Context) error {
			runs.Add(1)
			return os.ErrInvalid
		})
	}()

	eventually(t, func() bool { return runs.Load() >= 3 }, "watcher was not restarted")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
}

func TestUdev_WatchMounts(t *testing.T) {
	u, scanner, _ := newTestUdev(&option.Option{})

	t.Run("fails if the mount table can not be opened", func(t *testing.T) {
		u.mountInfoPath = "/does/not/exist"
		err := u.watchMounts(context.Background())
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Zero(t, scanner.wakes.Load())
	})

	t.Run("wakes after start and stops on cancel", func(t *testing.T) {
		u.mountInfoPath = "/proc/self/mountinfo"
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() { errCh <- u.watchMounts(ctx) }()

		eventually(t, func() bool { return scanner.wakes.Load() >= 1 }, "no initial wake-up")
		cancel()
		select {
		case err := <-errCh:
			assert.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("watchMounts did not return after cancellation")
		}
	})
}

// TestUdev_WatchMounts_DetectsMount needs CAP_SYS_ADMIN, e.g. "unshare -Urm go test ...".
func TestUdev_WatchMounts_DetectsMount(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mount("none", dir, "tmpfs", 0, ""); err != nil {
		t.Skipf("not allowed to mount: %v", err)
	}
	require.NoError(t, unix.Unmount(dir, 0))

	u, scanner, _ := newTestUdev(&option.Option{})
	u.mountInfoPath = "/proc/self/mountinfo"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = u.watchMounts(ctx) }()
	eventually(t, func() bool { return scanner.wakes.Load() == 1 }, "no initial wake-up")

	// Each change of the mount table wakes the scanner, mount as well as umount.
	require.NoError(t, unix.Mount("none", dir, "tmpfs", 0, ""))
	eventually(t, func() bool { return scanner.wakes.Load() == 2 }, "mount was not detected")
	require.NoError(t, unix.Unmount(dir, 0))
	eventually(t, func() bool { return scanner.wakes.Load() == 3 }, "umount was not detected")
}
