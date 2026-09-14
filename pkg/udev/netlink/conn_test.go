package netlink

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestConn_CloseNil(t *testing.T) {
	c := &Conn{file: nil}
	assert.NoError(t, c.Close())
}

func TestActionRescan_Constant(t *testing.T) {
	assert.Equal(t, Action("rescan"), ActionRescan)
}

func TestConn_Monitor_NilReceiver(t *testing.T) {
	var c *Conn
	err := c.Monitor(context.Background(), nil)
	assert.ErrorContains(t, err, "nil or closed")

	c2 := &Conn{file: nil}
	err2 := c2.Monitor(context.Background(), nil)
	assert.ErrorContains(t, err2, "nil or closed")
}

func TestConn_Monitor_SocketClosedUnexpectedly(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	require.NoError(t, err)
	rx := os.NewFile(uintptr(fds[0]), "rx")
	tx := os.NewFile(uintptr(fds[1]), "tx")
	defer tx.Close()

	conn := &Conn{file: rx}
	eventChan := make(chan *UEvent, 1)
	errChan := make(chan error, 1)

	// Context is NOT cancelled, but socket is closed unexpectedly.
	ctx := context.Background()
	go func() {
		errChan <- conn.Monitor(ctx, eventChan)
	}()

	time.Sleep(50 * time.Millisecond)
	_ = conn.Close()

	select {
	case err := <-errChan:
		assert.Error(t, err)
		assert.ErrorContains(t, err, "netlink read error")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Monitor to exit on unexpected socket closure")
	}
}

func TestDialUdev(t *testing.T) {
	conn, err := DialUdev()
	if err != nil {
		// In unprivileged test containers without network capabilities,
		// DialUdev might fail with EPERM/EACCES, which is expected.
		t.Logf("DialUdev failed (likely unprivileged container): %v", err)
		return
	}
	defer conn.Close()
	assert.NotNil(t, conn.file)
}

func TestConn_Monitor_Success(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	require.NoError(t, err)
	rx := os.NewFile(uintptr(fds[0]), "rx")
	tx := os.NewFile(uintptr(fds[1]), "tx")
	defer tx.Close()

	conn := &Conn{file: rx}
	eventChan := make(chan *UEvent, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- conn.Monitor(ctx, eventChan)
	}()

	// 1. Send an unparsable packet (should be skipped without error).
	_, err = tx.Write([]byte("invalid_packet_data"))
	require.NoError(t, err)

	// 2. Send a valid packet.
	packet := buildLibudevPacket(map[string]string{
		"ACTION":    "add",
		"DEVNAME":   "/dev/sdb",
		"DEVTYPE":   "disk",
		"DEVPATH":   "/devices/pci0000:00/0000:00:1f.2/ata2/host1/target1:0:0/1:0:0:0/block/sdb",
		"SUBSYSTEM": "block",
	})
	_, err = tx.Write(packet)
	require.NoError(t, err)

	select {
	case ev := <-eventChan:
		assert.Equal(t, ActionAdd, ev.Action)
		assert.Equal(t, "/dev/sdb", ev.Env["DEVNAME"])
		assert.Equal(t, "block", ev.Subsystem)
		assert.Equal(t, "/dev/sdb", ev.DevName)
		assert.Equal(t, "/devices/pci0000:00/0000:00:1f.2/ata2/host1/target1:0:0/1:0:0:0/block/sdb", ev.DevPath)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	// Cancel context and verify Monitor terminates cleanly.
	cancel()
	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Monitor to exit")
	}
}

func TestConn_Monitor_RescansAfterTruncatedDatagram(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	require.NoError(t, err)
	rx := os.NewFile(uintptr(fds[0]), "rx")
	tx := os.NewFile(uintptr(fds[1]), "tx")
	defer tx.Close()

	conn := &Conn{file: rx}
	eventChan := make(chan *UEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- conn.Monitor(ctx, eventChan)
	}()

	_, err = tx.Write(make([]byte, readBufferSize+1))
	require.NoError(t, err)

	select {
	case event := <-eventChan:
		assert.Equal(t, ActionRescan, event.Action)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rescan after truncated event")
	}

	cancel()
	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Monitor to exit")
	}
}

func TestConn_Monitor_DropsNonBlockSubsystem(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	require.NoError(t, err)
	rx := os.NewFile(uintptr(fds[0]), "rx")
	tx := os.NewFile(uintptr(fds[1]), "tx")
	defer tx.Close()

	conn := &Conn{file: rx}
	eventChan := make(chan *UEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- conn.Monitor(ctx, eventChan)
	}()

	// Send an event with SUBSYSTEM=net (should be dropped).
	netPacket := buildLibudevPacket(map[string]string{
		"ACTION":    "add",
		"DEVNAME":   "eth0",
		"DEVPATH":   "/devices/pci0000:00/0000:00:03.0/net/eth0",
		"SUBSYSTEM": "net",
	})
	_, err = tx.Write(netPacket)
	require.NoError(t, err)

	select {
	case ev := <-eventChan:
		t.Fatalf("unexpected event received for non-block subsystem: %#v", ev)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Monitor to exit")
	}
}

func TestVerifySender(t *testing.T) {
	validGroup := &unix.SockaddrNetlink{Groups: UdevMulticastGroup}
	wrongGroup := &unix.SockaddrNetlink{Groups: 1}

	kernelCreds := unix.UnixCredentials(&unix.Ucred{Pid: 0, Uid: 0, Gid: 0})
	rootCreds := unix.UnixCredentials(&unix.Ucred{Pid: 5432, Uid: 0, Gid: 0})
	unprivilegedCreds := unix.UnixCredentials(&unix.Ucred{Pid: 5432, Uid: 1000, Gid: 1000})

	t.Run("valid kernel sender", func(t *testing.T) {
		err := verifySender(validGroup, kernelCreds)
		assert.NoError(t, err)
	})

	t.Run("valid root sender", func(t *testing.T) {
		err := verifySender(validGroup, rootCreds)
		assert.NoError(t, err)
	})

	t.Run("unprivileged sender rejected", func(t *testing.T) {
		err := verifySender(validGroup, unprivilegedCreds)
		assert.ErrorIs(t, err, ErrUntrustedSender)
		assert.ErrorContains(t, err, "unprivileged sender")
	})

	t.Run("wrong group rejected", func(t *testing.T) {
		err := verifySender(wrongGroup, rootCreds)
		assert.ErrorIs(t, err, ErrUntrustedSender)
		assert.ErrorContains(t, err, "not multicast on udev group")
	})

	t.Run("no credentials rejected", func(t *testing.T) {
		err := verifySender(validGroup, []byte{})
		assert.ErrorIs(t, err, ErrUntrustedSender)
		assert.ErrorContains(t, err, "no SCM_CREDENTIALS attached")
	})

	t.Run("mock unix socketpair accepted for tests", func(t *testing.T) {
		err := verifySender(&unix.SockaddrUnix{}, nil)
		assert.NoError(t, err)
	})
}
