package blockdevice

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
)

func TestScanner_Wake(t *testing.T) {
	s := NewScanner("node", "ns", nil, nil, nil, nil)

	// Must never block, no matter how often it is called without a consumer.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			s.Wake()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wake blocked")
	}

	// All pending requests are merged into one scan...
	assert.Len(t, s.wake, 1)
	<-s.wake
	// ...and a request after that triggers the next one.
	assert.Empty(t, s.wake)
	s.Wake()
	assert.Len(t, s.wake, 1)
}

// failingBlockDevices is a BlockDeviceController whose Update always fails.
type failingBlockDevices struct {
	ctldiskv1.BlockDeviceController
	updates atomic.Int32
}

func (f *failingBlockDevices) Update(*diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	f.updates.Add(1)
	return nil, apierrors.NewConflict(schema.GroupResource{Resource: "blockdevices"}, "bd", errors.New("the object has been modified"))
}

func setRetryDelay(t *testing.T, d time.Duration) {
	t.Helper()
	old := retryDelay
	retryDelay = d
	t.Cleanup(func() { retryDelay = old })
}

func TestScanner_RetryLater(t *testing.T) {
	setRetryDelay(t, 50*time.Millisecond)
	s := NewScanner("node", "ns", nil, nil, nil, nil)

	start := time.Now()
	s.retryLater()
	assert.Empty(t, s.wake, "the retry must not be immediate")

	select {
	case <-s.wake:
		assert.GreaterOrEqual(t, time.Since(start), retryDelay)
	case <-time.After(5 * time.Second):
		t.Fatal("no scan requested after the retry delay")
	}
}

// A failed update must not leave the BlockDevice stale until some unrelated
// event comes along: a follow-up scan has to be requested, but delayed.
func TestScanner_UpdateFailureRequestsRescan(t *testing.T) {
	setRetryDelay(t, 50*time.Millisecond)
	bds := &failingBlockDevices{}
	s := NewScanner("node", "ns", nil, bds, nil, nil)

	device := func(capacity uint64) *diskv1.BlockDevice {
		bd := &diskv1.BlockDevice{ObjectMeta: metav1.ObjectMeta{Name: "bd"}}
		bd.Status.State = diskv1.BlockDeviceActive
		bd.Status.DeviceStatus.DevPath = "/dev/sdb"
		bd.Status.DeviceStatus.FileSystem = &diskv1.FilesystemStatus{}
		bd.Status.DeviceStatus.Capacity.SizeBytes = capacity
		return bd
	}

	start := time.Now()
	s.handleExistingDev(device(1), device(2), false)
	assert.Equal(t, int32(1), bds.updates.Load())
	assert.Empty(t, s.wake, "the retry must not be immediate")

	select {
	case <-s.wake:
		assert.GreaterOrEqual(t, time.Since(start), retryDelay)
	case <-time.After(5 * time.Second):
		t.Fatal("no scan requested after a failed update")
	}
}

// With a cancelled context and a pending wake-up both ready, select picks one
// at random, so repeat until a wrong pick would have been seen many times.
func TestScanner_RunDoesNotScanAfterCancel(t *testing.T) {
	s := NewScanner("node", "ns", nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var scans atomic.Int32
	for range 200 {
		s.Wake()
		s.run(ctx, func(context.Context) error {
			scans.Add(1)
			return nil
		})
	}
	assert.Zero(t, scans.Load(), "a scan was started after cancellation")
}

func TestScanner_RunScansOnWakeAndRetriesFailures(t *testing.T) {
	setRetryDelay(t, 10*time.Millisecond)
	s := NewScanner("node", "ns", nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var scans atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.run(ctx, func(context.Context) error {
			// The first scan fails and has to be retried by itself.
			if scans.Add(1) == 1 {
				return errors.New("transient")
			}
			return nil
		})
	}()

	s.Wake()
	require.Eventually(t, func() bool { return scans.Load() == 2 }, 5*time.Second, 5*time.Millisecond, "failed scan was not retried")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
}

func TestScanner_RunNoRetryWhenFailedDuringShutdown(t *testing.T) {
	setRetryDelay(t, 10*time.Millisecond)
	s := NewScanner("node", "ns", nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())

	s.Wake()
	s.run(ctx, func(context.Context) error {
		cancel()
		return ctx.Err()
	})

	time.Sleep(10 * retryDelay)
	assert.Empty(t, s.wake, "no retry expected after a failure caused by shutdown")
}

// Many failures in one scan, and failures of the scans that follow, must not
// add up to more than one retry per delay.
func TestScanner_RetryLaterIsScheduledOnlyOnce(t *testing.T) {
	setRetryDelay(t, 50*time.Millisecond)
	s := NewScanner("node", "ns", nil, nil, nil, nil)

	for range 100 {
		s.retryLater()
	}
	<-s.wake // the one retry
	time.Sleep(3 * retryDelay)
	assert.Empty(t, s.wake, "only one retry may result from many failures")

	// After it fired, the next failure schedules a new one.
	s.retryLater()
	select {
	case <-s.wake:
	case <-time.After(5 * time.Second):
		t.Fatal("no retry after the previous one fired")
	}
}

func TestScanner_UpdateFailuresOfSeveralDevicesRequestOneRescan(t *testing.T) {
	setRetryDelay(t, 50*time.Millisecond)
	bds := &failingBlockDevices{}
	s := NewScanner("node", "ns", nil, bds, nil, nil)

	for range 20 {
		bd := func(capacity uint64) *diskv1.BlockDevice {
			d := &diskv1.BlockDevice{ObjectMeta: metav1.ObjectMeta{Name: "bd"}}
			d.Status.State = diskv1.BlockDeviceActive
			d.Status.DeviceStatus.DevPath = "/dev/sdb"
			d.Status.DeviceStatus.FileSystem = &diskv1.FilesystemStatus{}
			d.Status.DeviceStatus.Capacity.SizeBytes = capacity
			return d
		}
		s.handleExistingDev(bd(1), bd(2), false)
	}
	assert.Equal(t, int32(20), bds.updates.Load())

	<-s.wake
	time.Sleep(3 * retryDelay)
	assert.Empty(t, s.wake, "20 failed updates must result in a single retry")
}
