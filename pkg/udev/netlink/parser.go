package netlink

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Header of a message published by udevd, struct monitor_netlink_header in
// systemd's src/libsystemd/sd-device/device-monitor-private.h:
//
//	char     prefix[8]               "libudev\0"
//	unsigned magic                   0xfeedcafe, network byte order
//	unsigned header_size             size of this header (40)
//	unsigned properties_off          offset of the properties, host byte order
//	unsigned properties_len          length of the properties, host byte order
//	unsigned filter_subsystem_hash   \
//	unsigned filter_devtype_hash      } hashes for in-kernel socket filters,
//	unsigned filter_tag_bloom_hi      } not used here
//	unsigned filter_tag_bloom_lo     /
//
// The properties follow as "KEY=VALUE\0" strings.
const (
	headerLen      = 40
	magicOffset    = 8
	propsOffOffset = 16
	magic          = 0xfeedcafe
)

var (
	prefix     = []byte("libudev\x00")
	errInvalid = errors.New("not a udev event")
)

// parse decodes a datagram published by udevd. Like sd-device it uses
// properties_off and reads the properties up to the end of the datagram.
// Kernel events of group 1 ("add@/devices/...") are not supported and rejected.
func parse(b []byte) (map[string]string, error) {
	if len(b) <= headerLen || !bytes.HasPrefix(b, prefix) ||
		binary.BigEndian.Uint32(b[magicOffset:]) != magic {
		return nil, errInvalid
	}
	off := int(binary.NativeEndian.Uint32(b[propsOffOffset:]))
	if off < headerLen || off >= len(b) {
		return nil, errInvalid
	}

	properties := make(map[string]string)
	for _, entry := range bytes.Split(b[off:], []byte{0}) {
		// Cut at the first '=' only, values may contain '=' themselves.
		if key, value, ok := bytes.Cut(entry, []byte{'='}); ok && len(key) > 0 {
			properties[string(key)] = string(value)
		}
	}
	return properties, nil
}
