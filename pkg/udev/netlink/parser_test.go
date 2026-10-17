package netlink

import (
	"encoding/binary"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildPacket creates a datagram in the format udevd publishes.
func buildPacket(properties map[string]string) []byte {
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var payload []byte
	for _, k := range keys {
		payload = append(payload, k+"="+properties[k]+"\x00"...)
	}

	packet := make([]byte, headerLen, headerLen+len(payload))
	header := packet
	copy(header, prefix)
	binary.BigEndian.PutUint32(header[magicOffset:], magic)
	binary.NativeEndian.PutUint32(header[12:], headerLen)
	binary.NativeEndian.PutUint32(header[propsOffOffset:], headerLen)
	binary.NativeEndian.PutUint32(header[20:], uint32(len(payload))) //nolint:gosec // test data
	return append(packet, payload...)
}

// TestParse_RealDatagram decodes a datagram that was captured from
// systemd-udevd 255.4 (Ubuntu 24.04) on the udev multicast group while a loop
// device was set up with udisksctl. The expected properties are what
// "udevadm monitor --udev --property" printed for the same event. Unlike the
// packets built by buildPacket this does not depend on our reading of the format.
func TestParse_RealDatagram(t *testing.T) {
	packet, err := os.ReadFile("testdata/udevd-change-loop0.bin")
	require.NoError(t, err)

	properties, err := parse(packet)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"UDEV_DATABASE_VERSION":  "1", // not shown by udevadm
		"ACTION":                 "change",
		"DEVPATH":                "/devices/virtual/block/loop0",
		"SUBSYSTEM":              "block",
		"DEVNAME":                "/dev/loop0",
		"DEVTYPE":                "disk",
		"DISKSEQ":                "10",
		"SEQNUM":                 "6927",
		"MAJOR":                  "7",
		"MINOR":                  "0",
		"USEC_INITIALIZED":       "1142034",
		"TAGS":                   ":systemd:",
		"CURRENT_TAGS":           ":systemd:",
		"ID_LOOP_BACKING_DEVICE": "259:1",
		"ID_LOOP_BACKING_INODE":  "39720268",
		"DEVLINKS":               "/dev/disk/by-diskseq/10 /dev/disk/by-loop-inode/259:1-39720268",
	}, properties)
}

func TestParse(t *testing.T) {
	properties, err := parse(buildPacket(map[string]string{
		"ACTION":     "add",
		"DEVNAME":    "/dev/sdb",
		"DEVTYPE":    "disk",
		"ID_FS_UUID": "a=b=c", // values may contain '='
		"ID_MODEL":   "",      // empty values are kept
	}))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"ACTION":     "add",
		"DEVNAME":    "/dev/sdb",
		"DEVTYPE":    "disk",
		"ID_FS_UUID": "a=b=c",
		"ID_MODEL":   "",
	}, properties)
}

func TestParse_SkipsMalformedEntries(t *testing.T) {
	packet := buildPacket(nil)
	packet = append(packet, "noequal\x00=nokey\x00\x00A=1"...) // last entry without NUL
	properties, err := parse(packet)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "1"}, properties)
}

func TestParse_Invalid(t *testing.T) {
	valid := buildPacket(map[string]string{"ACTION": "add"})

	withMagic := func(m uint32) []byte {
		b := append([]byte(nil), valid...)
		binary.BigEndian.PutUint32(b[magicOffset:], m)
		return b
	}
	withOffset := func(off uint32) []byte {
		b := append([]byte(nil), valid...)
		binary.NativeEndian.PutUint32(b[propsOffOffset:], off)
		return b
	}

	tests := map[string][]byte{
		"empty":                nil,
		"too short":            valid[:headerLen],
		"kernel event":         []byte("add@/devices/virtual/block/loop0\x00ACTION=add\x00SEQNUM=1\x00"),
		"wrong magic":          withMagic(0xdeadbeef),
		"offset inside header": withOffset(headerLen - 1),
		"offset behind packet": withOffset(uint32(len(valid))), //nolint:gosec // test data
		"offset way off":       withOffset(0xffffffff),
		"prefix without NUL":   append([]byte("libudevX"), valid[8:]...),
	}
	for name, packet := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parse(packet)
			assert.ErrorIs(t, err, errInvalid)
		})
	}
}
