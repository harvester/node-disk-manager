//go:build linux

package udev

import (
	"context"
	"math"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/harvester/node-disk-manager/pkg/utils"
)

// NDM mounts the host's /proc at /host/proc (see daemonset.yaml), so we watch
// /host/proc/1/mountinfo (host PID 1 = init) to observe the host mount namespace.
// Using /proc/self/mountinfo would only reflect the container's own namespace.
const procMountInfo = "/host/proc/1/mountinfo"

// watchMounts monitors mount table changes by polling /proc/self/mountinfo for POLLERR.
// The kernel raises POLLERR on this fd whenever any mount or umount occurs.
func (u *Udev) watchMounts(ctx context.Context) {
	logrus.Debugf("mount watcher: opening %s", procMountInfo)
	fd, err := unix.Open(procMountInfo, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		logrus.Errorf("mount watcher: failed to open %s: %v", procMountInfo, err)
		return
	}
	logrus.Debugf("mount watcher: opened fd=%d", fd)
	defer func() {
		logrus.Debugf("mount watcher: closing fd=%d", fd)
		unix.Close(fd)
	}()

	if fd > math.MaxInt32 {
		logrus.Errorf("mount watcher: fd %d out of int32 range", fd)
		return
	}
	fdInt32 := int32(fd) //nolint:gosec // fd is guaranteed non-negative by Open and bounded by math.MaxInt32 check above

	// POLLERR fires when the kernel marks /proc/self/mountinfo dirty (mount/umount)
	fds := []unix.PollFd{{Fd: fdInt32, Events: unix.POLLERR}}

	logrus.Infof("mount watcher: watching %s for mount table changes", procMountInfo)

	for {
		select {
		case <-ctx.Done():
			logrus.Debug("mount watcher: context cancelled, exiting")
			return
		default:
		}

		logrus.Debugf("mount watcher: polling fd=%d for POLLERR, timeout=5s", fd)
		n, err := unix.Poll(fds, 5*1000)
		if err != nil {
			if err == unix.EINTR {
				logrus.Debug("mount watcher: poll interrupted by signal, retrying")
				continue
			}
			logrus.Errorf("mount watcher: poll error: %v", err)
			return
		}
		if n == 0 {
			logrus.Debug("mount watcher: poll timeout, no mount changes")
			continue
		}

		logrus.Debugf("mount watcher: POLLERR received (revents=0x%x), mount table changed", fds[0].Revents)
		utils.CallerWithCondLock(u.scanner.Cond, func() any {
			logrus.Info("mount watcher: mount change detected, waking scanner")
			u.scanner.Cond.Signal()
			return nil
		})
	}
}
