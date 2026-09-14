//go:build linux

package udev

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/harvester/node-disk-manager/pkg/utils"
)

// NDM mounts the host's /proc at /host/proc (see daemonset.yaml), so we watch
// /host/proc/1/mountinfo (host PID 1 = init) to observe the host mount namespace.
//
// Linux Mount Namespace & Procfs Background:
//
//  1. Containers and Mount Namespaces:
//     NDM runs inside a container with its own private mount namespace. Inspecting
//     /proc/self/mountinfo would only reveal the container's private overlay/tmpfs mounts.
//     By mounting the host root filesystem's /proc at /host/proc and opening PID 1's
//     mountinfo (PID 1 is the host init/systemd), NDM directly tracks the host node's mounts.
//
//  2. Why poll() instead of inotify?
//     Pseudo-filesystems (procfs, sysfs) do not generate standard inotify events (IN_MODIFY)
//     because their contents are generated dynamically by kernel callbacks on read().
//
//  3. The POLLERR Mechanism (Linux VFS mount_event):
//     Since Linux 2.6.26, the kernel VFS maintains a per-mount-namespace counter (`mount_event`).
//     Whenever mount(), umount(), or pivot_root() succeeds, the kernel increments this counter.
//     When userspace polls an open mountinfo file descriptor with poll() or epoll(), the kernel's
//     fs/namespace.c:mountinfo_poll() detects the counter change and returns POLLERR | POLLPRI.
//     In this context, POLLERR does NOT indicate an I/O error; it is the kernel's documented
//     notification signal that the mount table has changed!
const procMountInfo = "/host/proc/1/mountinfo"

// getProcMountInfoPath resolves the mountinfo file path to monitor.
// In the DaemonSet container, the host's /proc is mounted at /host/proc.
// If /host/proc/1/mountinfo is not present (e.g. unit tests, dev environments,
// or non-containerized deployments), it falls back to /proc/1/mountinfo and
// /proc/self/mountinfo.
func getProcMountInfoPath() string {
	candidates := []string{
		procMountInfo,
		"/proc/1/mountinfo",
		"/proc/self/mountinfo",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return procMountInfo
}

// watchMounts monitors mount table changes by polling host mountinfo for POLLERR.
// procfs has special poll semantics: the kernel raises POLLERR on an open
// mountinfo file whenever a mount or umount changes that mount namespace. It
// does not mean the file is broken in this case.
// On any unrecoverable error it sends to errChan so spawnMountWatcher can respawn it.
func (u *Udev) watchMounts(ctx context.Context, errChan chan error) {
	mountInfoPath := getProcMountInfoPath()
	logrus.Debugf("Mount watcher: opening %s", mountInfoPath)
	fd, err := unix.Open(mountInfoPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		errChan <- fmt.Errorf("failed to open %s: %w", mountInfoPath, err)
		return
	}
	logrus.Debugf("Mount watcher: opened fd=%d", fd)
	defer func() {
		logrus.Debugf("Mount watcher: closing fd=%d", fd)
		unix.Close(fd)
	}()

	if fd > math.MaxInt32 {
		errChan <- fmt.Errorf("fd %d out of int32 range", fd)
		return
	}
	fdInt32 := int32(fd) //nolint:gosec // fd is guaranteed non-negative by Open and bounded by math.MaxInt32 check above

	// POLLERR | POLLPRI fires when the kernel marks /proc/self/mountinfo dirty (mount/umount)
	fds := []unix.PollFd{{Fd: fdInt32, Events: unix.POLLERR | unix.POLLPRI}}

	logrus.Infof("Mount watcher: watching %s for mount table changes", mountInfoPath)

	for {
		select {
		case <-ctx.Done():
			logrus.Debug("Mount watcher: context cancelled, exiting")
			return
		default:
		}

		// Poll with a 1-second timeout so the loop can periodically check ctx.Done()
		// and exit cleanly during shutdown instead of blocking indefinitely.
		n, err := unix.Poll(fds, 1000)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				logrus.Debug("Mount watcher: poll interrupted by signal, retrying")
				continue
			}
			errChan <- fmt.Errorf("poll error: %w", err)
			return
		}
		if n == 0 {
			continue
		}

		if fds[0].Revents&(unix.POLLNVAL|unix.POLLHUP) != 0 {
			errChan <- fmt.Errorf("mount watcher fd closed or invalid (revents=0x%x)", fds[0].Revents)
			return
		}

		if fds[0].Revents&(unix.POLLERR|unix.POLLPRI) == 0 {
			logrus.Debugf("Mount watcher: unexpected revents 0x%x, ignoring", fds[0].Revents)
			continue
		}

		// No re-read or seek is required here: the kernel tracks the mount
		// table generation internally and re-arms POLLERR exactly once per
		// actual mount/umount, so the next poll() call blocks normally again.
		logrus.Debugf("Mount watcher: POLLERR received (revents=0x%x), mount table changed", fds[0].Revents)
		if u.scanner == nil || u.scanner.Cond == nil {
			logrus.Warn("Mount watcher: scanner or condition variable not initialized, skipping scanner wake")
			continue
		}
		utils.CallerWithCondLock(u.scanner.Cond, func() any {
			logrus.Info("Mount watcher: mount change detected, waking scanner")
			u.scanner.Cond.Signal()
			return nil
		})
	}
}
