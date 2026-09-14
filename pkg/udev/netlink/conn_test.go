package netlink

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// newTestConn returns a Conn that reads from a datagram socketpair, as
// unprivileged tests can not send to a netlink multicast group.
func newTestConn(t *testing.T) (*Conn, *os.File) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	require.NoError(t, err)
	rx, tx := os.NewFile(uintptr(fds[0]), "rx"), os.NewFile(uintptr(fds[1]), "tx")
	t.Cleanup(func() { _ = rx.Close(); _ = tx.Close() })
	raw, err := rx.SyscallConn()
	require.NoError(t, err)
	return &Conn{file: rx, raw: raw, buf: make([]byte, readBufferSize)}, tx
}

type readResult struct {
	properties map[string]string
	err        error
}

// readAsync calls Read in the background so a test can not hang.
func readAsync(c *Conn) <-chan readResult {
	ch := make(chan readResult, 1)
	go func() {
		p, err := c.Read()
		ch <- readResult{p, err}
	}()
	return ch
}

func receive(t *testing.T, ch <-chan readResult) readResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Read")
		return readResult{}
	}
}

func TestConn_Read(t *testing.T) {
	conn, tx := newTestConn(t)
	ch := readAsync(conn)

	// Datagrams that are not udev events are skipped, the next valid one is returned.
	_, err := tx.Write([]byte("add@/devices/virtual/block/loop0\x00ACTION=add\x00"))
	require.NoError(t, err)
	_, err = tx.Write([]byte("garbage"))
	require.NoError(t, err)
	_, err = tx.Write(buildPacket(map[string]string{"ACTION": "add", "DEVNAME": "/dev/sdb"}))
	require.NoError(t, err)

	r := receive(t, ch)
	require.NoError(t, r.err)
	assert.Equal(t, map[string]string{"ACTION": "add", "DEVNAME": "/dev/sdb"}, r.properties)
}

func TestConn_Read_TruncatedDatagramLosesEvents(t *testing.T) {
	conn, tx := newTestConn(t)
	ch := readAsync(conn)

	_, err := tx.Write(make([]byte, readBufferSize+1))
	require.NoError(t, err)
	assert.ErrorIs(t, receive(t, ch).err, ErrEventsLost)

	// The connection stays usable.
	ch = readAsync(conn)
	_, err = tx.Write(buildPacket(map[string]string{"ACTION": "remove"}))
	require.NoError(t, err)
	r := receive(t, ch)
	require.NoError(t, r.err)
	assert.Equal(t, "remove", r.properties["ACTION"])
}

func TestConn_Close_UnblocksRead(t *testing.T) {
	conn, _ := newTestConn(t)
	ch := readAsync(conn)

	// Close may run before or after Read blocked, both must end Read.
	require.NoError(t, conn.Close())
	r := receive(t, ch)
	assert.Error(t, r.err)
	assert.NotErrorIs(t, r.err, ErrEventsLost)

	_, err := conn.Read()
	assert.Error(t, err)
}

func TestDial(t *testing.T) {
	conn, err := Dial()
	if err != nil {
		t.Skipf("netlink uevent socket not available: %v", err)
	}
	require.NoError(t, conn.Close())
}

// dialInPrivateNetns returns a connection from Dial and a socket to send to
// the udev multicast group, both in a new network namespace. That keeps the
// test away from the real udev events of the host, even when run as root.
// It needs CAP_SYS_ADMIN, e.g. "unshare -Ur go test ...".
func dialInPrivateNetns(t *testing.T) (*Conn, int) {
	t.Helper()
	type result struct {
		conn *Conn
		fd   int
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// The thread is never unlocked, so it ends together with this
		// goroutine instead of returning to the pool in the foreign namespace.
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			ch <- result{err: err}
			return
		}
		conn, err := Dial()
		if err != nil {
			ch <- result{err: err}
			return
		}
		fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_KOBJECT_UEVENT)
		if err != nil {
			_ = conn.Close()
		}
		ch <- result{conn, fd, err}
	}()

	r := <-ch
	if r.err != nil {
		t.Skipf("not allowed to create a network namespace: %v", r.err)
	}
	t.Cleanup(func() { _ = r.conn.Close(); _ = unix.Close(r.fd) })
	return r.conn, r.fd
}

// TestDial_ReceivesUdevEvent sends from a real netlink socket to the udev
// multicast group, like udevd does, and receives the datagrams with Dial.
func TestDial_ReceivesUdevEvent(t *testing.T) {
	conn, fd := dialInPrivateNetns(t)
	send := func(packet []byte) {
		t.Helper()
		require.NoError(t, unix.Sendto(fd, packet, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: udevGroup}))
	}

	ch := readAsync(conn)
	// Datagrams that are not udev events are skipped.
	send([]byte("not a udev event"))
	send(buildPacket(map[string]string{"ACTION": "remove", "DEVNAME": "/dev/sdb", "DEVTYPE": "disk"}))

	r := receive(t, ch)
	require.NoError(t, r.err)
	assert.Equal(t, map[string]string{"ACTION": "remove", "DEVNAME": "/dev/sdb", "DEVTYPE": "disk"}, r.properties)
}

// TestDial_ReportsOverflow fills the receive queue of a real netlink socket
// and expects ErrEventsLost (ENOBUFS), followed by the remaining events.
func TestDial_ReportsOverflow(t *testing.T) {
	conn, fd := dialInPrivateNetns(t)
	// Shrink the queue, so a few events are enough to overflow it.
	var sockErr error
	require.NoError(t, conn.raw.Control(func(fd uintptr) {
		sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF, 4096)
	}))
	require.NoError(t, sockErr)
	packet := buildPacket(map[string]string{"ACTION": "add", "DEVNAME": "/dev/sdb", "DEVTYPE": "disk"})
	for range 200 {
		require.NoError(t, unix.Sendto(fd, packet, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: udevGroup}))
	}

	_, err := conn.Read()
	assert.ErrorIs(t, err, ErrEventsLost)
	properties, err := conn.Read()
	require.NoError(t, err)
	assert.Equal(t, "add", properties["ACTION"])
}
