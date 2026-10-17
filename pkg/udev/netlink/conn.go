// Package netlink receives the device events that systemd-udevd publishes on
// the NETLINK_KOBJECT_UEVENT multicast group 2, without cgo and without
// libudev.
//
// The kernel emits "raw" uevents on group 1. They are sent before udev has
// processed them and lack everything udev rules add (ID_SERIAL, ID_WWN, ...).
// systemd-udevd re-publishes every event on group 2 once its rules have run,
// prefixed with a "libudev" header, which is also what "udevadm monitor --udev"
// listens to. This package decodes that wire format, see sd-device
// (device-monitor.c in systemd) for the reference implementation.
//
// Only sockets in the initial network namespace receive device events, so the
// caller has to run in the host network namespace (hostNetwork: true) and
// udevd has to be running on the host.
package netlink

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	// udevGroup is the multicast group of events that were processed by udevd
	// (MONITOR_GROUP_UDEV in systemd), as a bind(2) group bitmask.
	udevGroup = 2

	// recvBufferSize is the requested socket receive buffer. The default of
	// ~200 KiB overflows quickly when many disks appear at once. systemd asks
	// for 128 MiB; as only block events are of interest here, 4 MiB is plenty.
	recvBufferSize = 4 * 1024 * 1024

	// readBufferSize is the maximum size of a single datagram. Typical udev
	// events are a few KiB. Larger ones are reported as ErrEventsLost.
	readBufferSize = 64 * 1024
)

// ErrEventsLost is returned by Conn.Read if one or more events were lost,
// because the kernel receive queue overflowed (ENOBUFS) or a datagram was too
// big for the read buffer. The caller can not know which devices were affected
// and has to re-read the complete device state. The connection stays usable.
var ErrEventsLost = errors.New("udev events lost")

// Conn is a connection to the udev multicast group. Read must not be called
// concurrently, Close may be called at any time.
type Conn struct {
	file *os.File
	raw  syscall.RawConn // integrates reads with the Go runtime poller
	buf  []byte
}

// Dial opens a netlink socket and subscribes to udev events.
func Dial() (*Conn, error) {
	// SOCK_NONBLOCK lets os.NewFile register the socket with the Go runtime
	// poller, so a blocked Read does not occupy a thread and Close wakes it up.
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil, fmt.Errorf("open netlink socket: %w", err)
	}

	// Try to exceed rmem_max first (needs CAP_NET_ADMIN) and accept whatever
	// the system allows otherwise. A too small buffer is not fatal, an
	// overflow is reported as ErrEventsLost.
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, recvBufferSize); err != nil {
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, recvBufferSize); err != nil {
			logrus.WithError(err).Debug("Unable to increase the udev netlink receive buffer")
		}
	}

	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: udevGroup}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind netlink socket to udev group: %w", err)
	}

	file := os.NewFile(uintptr(fd), "udev-netlink")
	raw, err := file.SyscallConn()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("access netlink socket: %w", err)
	}
	return &Conn{file: file, raw: raw, buf: make([]byte, readBufferSize)}, nil
}

// Close closes the socket. A blocked Read returns with an error.
func (c *Conn) Close() error {
	return c.file.Close()
}

// Read blocks until the next event arrives and returns its properties
// (ACTION, DEVNAME, DEVTYPE, ID_SERIAL, ...). Datagrams that are not udev
// events are dropped and logged at debug level.
//
// It returns ErrEventsLost if events have been lost, any other error is fatal
// for the connection, including the one caused by Close.
func (c *Conn) Read() (map[string]string, error) {
	for {
		var (
			n, flags int
			recvErr  error
		)
		// Parks the goroutine in the runtime poller until the socket is readable.
		err := c.raw.Read(func(fd uintptr) bool {
			n, _, flags, _, recvErr = unix.Recvmsg(int(fd), c.buf, nil, 0)
			return !errors.Is(recvErr, unix.EAGAIN)
		})
		if err != nil {
			return nil, err
		}

		switch {
		case errors.Is(recvErr, unix.EINTR):
			continue
		case errors.Is(recvErr, unix.ENOBUFS):
			return nil, ErrEventsLost
		case recvErr != nil:
			return nil, recvErr
		case flags&unix.MSG_TRUNC != 0:
			return nil, ErrEventsLost
		}

		properties, err := parse(c.buf[:n])
		if err != nil {
			logrus.WithError(err).Debug("Dropping invalid udev datagram")
			continue
		}
		return properties, nil
	}
}
