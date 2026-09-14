package udev

import (
	"context"
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/harvester/node-disk-manager/pkg/utils"
)

// hostMountInfo is the mount table of the host. NDM runs in a container with
// its own mount namespace, so /proc/self/mountinfo would only show the mounts
// of the container. The host's /proc is mounted at /host/proc (see
// daemonset.yaml) and PID 1 lives in the mount namespace of the host.
const hostMountInfo = utils.HostProcPath + "/1/mountinfo"

// pollTimeout bounds how long watchMounts blocks, so it notices a cancelled context.
const pollTimeout = 1000 // milliseconds

// watchMounts wakes the scanner whenever the mount table of the host changes.
//
// proc_pid_mounts(5), which applies to mountinfo as well: "a change in this
// file (i.e., a filesystem mount or unmount) causes select(2) to mark the file
// descriptor as having an exceptional condition, and poll(2) and epoll_wait(2)
// mark the file as having a priority event (POLLPRI)". The kernel
// (mounts_poll in fs/proc_namespace.c) remembers the last mount event seen per
// open file and reports POLLERR|POLLPRI once after one or more changes, so a
// change between two polls is never lost and nothing has to be re-read.
// POLLERR is not an I/O error here. inotify does not report changes of procfs files.
func (u *Udev) watchMounts(ctx context.Context) error {
	fd, err := unix.Open(u.mountInfoPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", u.mountInfoPath, err)
	}
	defer unix.Close(fd)

	logrus.WithField("node", u.nodeName).Infof("Start watching %s for mount table changes", u.mountInfoPath)
	u.scanner.Wake()

	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLPRI}} //nolint:gosec // file descriptors are small
	for ctx.Err() == nil {
		n, err := unix.Poll(fds, pollTimeout)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("poll %s: %w", u.mountInfoPath, err)
		}
		if n == 0 {
			continue
		}
		if fds[0].Revents&(unix.POLLNVAL|unix.POLLHUP) != 0 {
			return fmt.Errorf("poll %s: unexpected events 0x%x", u.mountInfoPath, fds[0].Revents)
		}

		logrus.WithField("node", u.nodeName).Info("Mount table changed")
		u.scanner.Wake()
	}
	return nil
}
