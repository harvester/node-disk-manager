package netlink

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	// UdevMulticastGroup is the Netlink multicast group carrying events that udevd
	// has already processed (rule evaluation completed, properties populated).
	// In the Netlink sockaddr_nl bitmask, group 2 corresponds to (1 << 1).
	// Group 1 delivers raw kernel events (too early, missing udev properties).
	UdevMulticastGroup = 2

	// socketBufferSize sets the SO_RCVBUF / SO_RCVBUFFORCE size (4 MB).
	// High device-churn events (e.g. storage scan bursts or hotplugging multiple disks)
	// can flood Netlink. The default Linux buffer (~212 KB) easily overflows, triggering ENOBUFS.
	socketBufferSize = 4 * 1024 * 1024

	// readBufferSize is the memory allocated for receiving each Netlink datagram.
	// Most udev datagrams are 2–8 KB; 128 KB provides ample headroom even for devices
	// with extensive ID_* properties, partition tables, and by-path/by-id symlinks.
	readBufferSize = 128 * 1024
)

// Conn represents an AF_NETLINK socket connection listening for uevents.
type Conn struct {
	file      *os.File
	closeOnce sync.Once
	closeErr  error
}

// DialUdev connects to the netlink kobject uevent socket and subscribes to
// udev-processed events (multicast group 2).
func DialUdev() (*Conn, error) {
	// Step 1: Create the AF_NETLINK raw socket.
	// - SOCK_RAW: Required for receiving raw Netlink packets.
	// - SOCK_CLOEXEC: Prevents the file descriptor from being inherited by child processes (e.g. lsblk, smartctl).
	// - SOCK_NONBLOCK: Enables non-blocking operation, required for Go's runtime netpoller.
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil, fmt.Errorf("failed to open netlink socket: %w", err)
	}

	// Step 2: Enable sender authentication (anti-spoofing).
	// SO_PASSCRED makes the kernel attach SCM_CREDENTIALS containing
	// struct ucred { pid, uid, gid } to each datagram. This follows systemd's
	// sd-device monitor behavior and lets us reject forged local messages.
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to enable netlink sender credentials: %w", err)
	}

	// Step 3: Enlarge receive buffer to absorb event bursts without dropping packets (ENOBUFS).
	// SO_RCVBUFFORCE bypasses the system-wide /proc/sys/net/core/rmem_max limit (requires CAP_NET_ADMIN).
	// If unprivileged, fallback gracefully to standard SO_RCVBUF (capped at rmem_max).
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, socketBufferSize); err != nil {
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, socketBufferSize)
	}

	// Step 4: Bind the socket to the udev processed multicast group.
	sa := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: UdevMulticastGroup,
	}

	if err := unix.Bind(fd, sa); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to bind netlink socket to udev group: %w", err)
	}

	// Step 5: Wrap the file descriptor in an *os.File.
	// In Go, wrapping a non-blocking socket descriptor with os.NewFile registers it
	// with the Go runtime's internal epoll-based network poller. This allows concurrent
	// reads to park the goroutine with 0% CPU and unblock immediately when data arrives
	// or when Close() is called, without tying up operating system threads.
	file := os.NewFile(uintptr(fd), "netlink-uevent")
	return &Conn{file: file}, nil
}

// Close closes the underlying socket. It is safe to call concurrently and repeatedly.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		if c.file != nil {
			c.closeErr = c.file.Close()
		}
	})
	return c.closeErr
}

// Monitor continuously reads uevents from the netlink socket until ctx is cancelled or an error occurs.
// Incoming events are sent to eventChan.
func (c *Conn) Monitor(ctx context.Context, eventChan chan<- *UEvent) error {
	if c == nil || c.file == nil {
		return errors.New("netlink connection is nil or closed")
	}

	// Acquire access to Go's low-level raw network connection (integrating with runtime epoll).
	rawConn, err := c.file.SyscallConn()
	if err != nil {
		return fmt.Errorf("failed to get raw netlink connection: %w", err)
	}

	// Use an internal cancelable context to guarantee the watchdog goroutine below
	// terminates immediately whenever Monitor returns (e.g. on socket read error).
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-monitorCtx.Done()
		_ = c.Close()
	}()

	buf := make([]byte, readBufferSize)
	oob := make([]byte, 2*unix.CmsgSpace(unix.SizeofUcred))
	for {
		var (
			n, oobn, flags int
			from           unix.Sockaddr
			recvErr        error
		)

		// rawConn.Read parks the goroutine in Go's runtime epoll poller until the socket is readable.
		// Returning false tells the runtime to wait again if we encountered EAGAIN/EWOULDBLOCK.
		err := rawConn.Read(func(fd uintptr) bool {
			n, oobn, flags, from, recvErr = unix.Recvmsg(int(fd), buf, oob, 0)
			return !errors.Is(recvErr, unix.EAGAIN) && !errors.Is(recvErr, unix.EWOULDBLOCK)
		})
		if err != nil {
			// Normal shutdown via context cancellation.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("netlink read error: %w", err)
		}

		if recvErr != nil {
			// Normal shutdown via context cancellation.
			if ctx.Err() != nil {
				return nil
			}
			// Buffer overflow: Kernel dropped packets because userspace didn't read fast enough.
			// Emit a synthetic ActionRescan event to force the scanner to run a full reconciliation,
			// ensuring any dropped add/remove events are recovered.
			if errors.Is(recvErr, unix.ENOBUFS) {
				logrus.Warn("Udev netlink socket buffer overrun (ENOBUFS), forcing full reconciliation")
				select {
				case eventChan <- &UEvent{Action: ActionRescan}:
				case <-ctx.Done():
					return nil
				}
				continue
			}
			// Interrupted system call (EINTR) -> retry.
			if errors.Is(recvErr, unix.EINTR) {
				continue
			}
			return fmt.Errorf("netlink receive error: %w", recvErr)
		}

		// MSG_TRUNC means a valid add/remove event may have been lost. Request a
		// full reconciliation to recover any state change represented by the
		// incomplete packet.
		if flags&unix.MSG_TRUNC != 0 {
			logrus.Warn("Udev netlink datagram exceeded buffer, forcing full reconciliation")
			select {
			case eventChan <- &UEvent{Action: ActionRescan}:
			case <-ctx.Done():
				return nil
			}
			continue
		}

		// MSG_CTRUNC means credentials cannot be validated, so drop the packet.
		if flags&unix.MSG_CTRUNC != 0 {
			logrus.Warn("Udev netlink credentials exceeded buffer and were dropped")
			continue
		}

		// Verify sender credentials (anti-spoofing).
		if err := verifySender(from, oob[:oobn]); err != nil {
			logrus.WithError(err).Debug("Dropping uevent from untrusted sender")
			continue
		}

		// Decode the packet bytes into an Action and property map.
		raw := buf[:n]
		uevent, err := ParseUEvent(raw)
		if err != nil {
			logrus.WithError(err).Debug("Failed to parse uevent datagram, skipping")
			continue
		}

		// Early subsystem filtering:
		// Udev multicast group 2 carries events for all kernel subsystems (net, tty, usb, pci, etc.).
		// Discard non-block events immediately to avoid downstream queue and processing overhead.
		if uevent.Subsystem != "" && uevent.Subsystem != "block" {
			continue
		}

		// Forward decoded event to caller.
		select {
		case eventChan <- uevent:
		case <-ctx.Done():
			return nil
		}
	}
}

// verifySender ensures incoming messages originate from multicast group 2 and
// were sent by the kernel or a privileged process such as systemd-udevd.
func verifySender(from unix.Sockaddr, oob []byte) error {
	sender, ok := from.(*unix.SockaddrNetlink)
	if !ok {
		// Allow unix socket pairs used in unit testing.
		if _, isUnix := from.(*unix.SockaddrUnix); isUnix || from == nil {
			return nil
		}
		return fmt.Errorf("%w: invalid socket address type %T", ErrUntrustedSender, from)
	}

	if sender.Groups != UdevMulticastGroup {
		return fmt.Errorf("%w: message was not multicast on udev group %d (got %d)", ErrUntrustedSender, UdevMulticastGroup, sender.Groups)
	}

	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return fmt.Errorf("%w: unable to parse socket control message: %w", ErrUntrustedSender, err)
	}

	for i := range messages {
		if messages[i].Header.Level != unix.SOL_SOCKET || messages[i].Header.Type != unix.SCM_CREDENTIALS {
			continue
		}
		credentials, err := unix.ParseUnixCredentials(&messages[i])
		if err != nil {
			return fmt.Errorf("%w: unable to parse unix credentials: %w", ErrUntrustedSender, err)
		}
		// The Linux kernel itself reports PID 0; systemd-udevd runs as root (UID 0).
		// In user namespaces, UID may appear translated, so only positively unprivileged
		// senders (PID != 0 && UID != 0) are rejected.
		if credentials.Pid != 0 && credentials.Uid != 0 {
			return fmt.Errorf("%w: unprivileged sender pid=%d uid=%d", ErrUntrustedSender, credentials.Pid, credentials.Uid)
		}
		return nil
	}

	return fmt.Errorf("%w: no SCM_CREDENTIALS attached to message", ErrUntrustedSender)
}
