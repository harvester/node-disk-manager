package netlink

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildLibudevPacket creates a synthetic byte slice following the libudev netlink wire format.
func buildLibudevPacket(env map[string]string) []byte {
	payloadSize := 0
	for k, v := range env {
		payloadSize += len(k) + 1 + len(v) + 1
	}

	packet := make([]byte, 40+payloadSize)
	copy(packet[:8], "libudev\x00")
	binary.BigEndian.PutUint32(packet[8:12], libudevMagic)
	binary.NativeEndian.PutUint32(packet[12:16], 40)
	binary.NativeEndian.PutUint32(packet[16:20], 40)
	binary.NativeEndian.PutUint32(packet[20:24], uint32(payloadSize)) //nolint:gosec // payloadSize in test is bounded

	offset := 40
	for k, v := range env {
		entry := k + "=" + v
		copy(packet[offset:], entry)
		offset += len(entry)
		packet[offset] = 0
		offset++
	}

	return packet
}

func TestParseLibudevEvent_Success(t *testing.T) {
	env := map[string]string{
		"ACTION":      "add",
		"DEVNAME":     "/dev/sdb",
		"DEVTYPE":     "disk",
		"DEVPATH":     "/devices/pci0000:00/0000:00:1f.2/ata2/host1/target1:0:0/1:0:0:0/block/sdb",
		"ID_SERIAL":   "QEMU_HARDDISK_HD12345",
		"ID_FS_LABEL": "FOO=BAR", // test value containing '='
	}

	packet := buildLibudevPacket(env)
	ev, err := ParseUEvent(packet)
	require.NoError(t, err)
	require.NotNil(t, ev)

	assert.Equal(t, ActionAdd, ev.Action)
	assert.Equal(t, "/dev/sdb", ev.Env["DEVNAME"])
	assert.Equal(t, "disk", ev.Env["DEVTYPE"])
	assert.Equal(t, "QEMU_HARDDISK_HD12345", ev.Env["ID_SERIAL"])
	assert.Equal(t, "FOO=BAR", ev.Env["ID_FS_LABEL"])
}

func TestParseLibudevEvent_Remove(t *testing.T) {
	env := map[string]string{
		"ACTION":  "remove",
		"DEVNAME": "/dev/sdc",
		"DEVTYPE": "disk",
		"DEVPATH": "/devices/virtual/block/sdc",
	}

	packet := buildLibudevPacket(env)
	ev, err := ParseUEvent(packet)
	require.NoError(t, err)
	require.NotNil(t, ev)

	assert.Equal(t, ActionRemove, ev.Action)
	assert.Equal(t, "/dev/sdc", ev.Env["DEVNAME"])
}

func TestParseLibudevEvent_Errors(t *testing.T) {
	t.Run("empty packet", func(t *testing.T) {
		_, err := ParseUEvent(nil)
		assert.ErrorIs(t, err, errPacketTooShort)
	})

	t.Run("packet too short", func(t *testing.T) {
		short := []byte("libudev\x00short")
		_, err := ParseUEvent(short)
		assert.ErrorIs(t, err, errPacketTooShort)
	})

	t.Run("invalid header size", func(t *testing.T) {
		packet := buildLibudevPacket(map[string]string{"ACTION": "add"})
		binary.NativeEndian.PutUint32(packet[12:16], 10)
		_, err := ParseUEvent(packet)
		assert.ErrorIs(t, err, errInvalidOffset)
	})

	t.Run("wrong magic", func(t *testing.T) {
		packet := buildLibudevPacket(map[string]string{"ACTION": "add"})
		binary.BigEndian.PutUint32(packet[8:12], 0xdeadbeef)
		_, err := ParseUEvent(packet)
		assert.ErrorIs(t, err, errMagicMismatch)
	})

	t.Run("invalid offset", func(t *testing.T) {
		packet := buildLibudevPacket(map[string]string{"ACTION": "add"})
		binary.NativeEndian.PutUint32(packet[16:20], 99999)
		_, err := ParseUEvent(packet)
		assert.ErrorIs(t, err, errInvalidOffset)
	})

	t.Run("missing action", func(t *testing.T) {
		packet := buildLibudevPacket(map[string]string{"DEVNAME": "/dev/sda", "DEVPATH": "/devices/pci/block/sda"})
		_, err := ParseUEvent(packet)
		assert.ErrorIs(t, err, errMissingAction)
	})

	t.Run("missing devpath", func(t *testing.T) {
		packet := buildLibudevPacket(map[string]string{"ACTION": "add"})
		_, err := ParseUEvent(packet)
		assert.ErrorIs(t, err, errMissingDevPath)
	})
}

func TestParseKernelEvent(t *testing.T) {
	t.Run("valid kernel event", func(t *testing.T) {
		raw := []byte("add@/devices/virtual/block/ram0\x00ACTION=add\x00DEVPATH=/devices/virtual/block/ram0\x00SUBSYSTEM=block\x00DEVNAME=ram0\x00")
		ev, err := ParseUEvent(raw)
		require.NoError(t, err)
		require.NotNil(t, ev)

		assert.Equal(t, ActionAdd, ev.Action)
		assert.Equal(t, "/devices/virtual/block/ram0", ev.Env["DEVPATH"])
		assert.Equal(t, "ram0", ev.Env["DEVNAME"])
	})

	t.Run("missing header terminator", func(t *testing.T) {
		raw := []byte("add@/devices/virtual/block/ram0")
		_, err := ParseUEvent(raw)
		assert.ErrorContains(t, err, "missing header terminator")
	})

	t.Run("missing @ in header", func(t *testing.T) {
		raw := []byte("invalid_header\x00KEY=VAL\x00")
		_, err := ParseUEvent(raw)
		assert.ErrorContains(t, err, "missing '@'")
	})

	t.Run("key values with empty entries or missing equal", func(t *testing.T) {
		raw := []byte("add@/dev/path\x00\x00no_equal\x00=no_key\x00VALID=OK\x00")
		ev, err := ParseUEvent(raw)
		require.NoError(t, err)
		assert.Equal(t, "OK", ev.Env["VALID"])
		assert.Equal(t, "add", ev.Env["ACTION"])
		assert.Equal(t, "/dev/path", ev.Env["DEVPATH"])
	})

	t.Run("missing action", func(t *testing.T) {
		raw := []byte("@/dev/path\x00DEVPATH=/dev/path\x00")
		_, err := ParseUEvent(raw)
		assert.ErrorIs(t, err, errMissingAction)
	})

	t.Run("missing devpath", func(t *testing.T) {
		raw := []byte("add@\x00ACTION=add\x00")
		_, err := ParseUEvent(raw)
		assert.ErrorIs(t, err, errMissingDevPath)
	})
}
