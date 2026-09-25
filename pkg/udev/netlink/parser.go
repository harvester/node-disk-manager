package netlink

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
)

const (
	// libudevMagic is the magic number in network byte order defined by libudev
	// (0xfeedcafe in big endian).
	libudevMagic uint32 = 0xfeedcafe

	// libudevHeaderLen is the minimum size of struct udev_monitor_netlink_header (40 bytes).
	libudevHeaderLen = 40

	libudevMagicOffset   = 8
	libudevHeaderSizeOff = 12
	libudevPayloadOff    = 16
	libudevPropsLenOff   = 20
)

var (
	libudevPrefix     = []byte("libudev\x00")
	errPacketTooShort = errors.New("packet is too short")
	errMagicMismatch  = errors.New("udev magic number mismatch")
	errInvalidOffset  = errors.New("invalid udev properties offset")
	errMissingAction  = errors.New("uevent missing ACTION property")
	errMissingDevPath = errors.New("uevent missing DEVPATH property")
)

// ParseUEvent decodes raw bytes received from a netlink kobject uevent socket.
// It detects the message format automatically:
// - libudev format: Starts with "libudev\0" and contains an enriched property header.
// - kernel format: Starts with "<action>@<devpath>\0..." emitted directly by the Linux kernel.
func ParseUEvent(raw []byte) (*UEvent, error) {
	if len(raw) == 0 {
		return nil, errPacketTooShort
	}

	// Packets prefixed with "libudev\0" are processed events from systemd-udevd (Multicast Group 2).
	if bytes.HasPrefix(raw, libudevPrefix) {
		return parseLibudevEvent(raw)
	}

	// Fallback: raw kernel events (Multicast Group 1).
	return parseKernelEvent(raw)
}

// parseLibudevEvent decodes a processed event sent by udevd.
//
// Wire format layout (struct udev_monitor_netlink_header, 40 bytes minimum):
//
//	Offset  0..7:   Prefix "libudev\0"
//	Offset  8..11:  Magic 0xfeedcafe (big endian / network byte order)
//	Offset 12..15:  header_size (native host endian, typically 40)
//	Offset 16..19:  properties_off (native host endian, where key-values start)
//	Offset 20..23:  properties_len (native host endian, byte length of key-values)
//	Offset 24..39:  filter hashes / bloom filter (unused here)
//	Offset properties_off ... properties_off+properties_len:
//	                Payload: "KEY1=VALUE1\0KEY2=VALUE2\0..." (NUL-separated)
func parseLibudevEvent(raw []byte) (*UEvent, error) {
	if len(raw) < libudevHeaderLen {
		return nil, errPacketTooShort
	}

	// 1. Verify magic number:
	// In systemd-udevd's libudev-monitor.c, the magic is written with:
	//   nlh->magic = htonl(UDEV_MONITOR_MAGIC); // 0xfeedcafe
	// Because of htonl(), the magic number is strictly stored in Network Byte Order
	// (Big Endian), regardless of CPU architecture. Checking this guarantees that the
	// message is a genuine udev monitor packet and not an alien Netlink datagram.
	magic := binary.BigEndian.Uint32(raw[libudevMagicOffset : libudevMagicOffset+4])
	if magic != libudevMagic {
		return nil, errMagicMismatch
	}

	// 2. Validate header size:
	// Unlike the magic, systemd writes header_size, properties_off, and properties_len
	// directly in native host byte order (binary.NativeEndian).
	// header_size allows future systemd versions to extend struct udev_monitor_netlink_header
	// without breaking backwards compatibility. It must be at least 40 bytes and fit in raw.
	headerSize := int(binary.NativeEndian.Uint32(raw[libudevHeaderSizeOff : libudevHeaderSizeOff+4]))
	if headerSize < libudevHeaderLen || headerSize > len(raw) {
		return nil, errInvalidOffset
	}

	// 3. Validate properties offset:
	// properties_off specifies where the NUL-separated "KEY=VALUE" payload starts.
	// It must begin at or after the header structure and stay within packet bounds.
	propsOffset := int(binary.NativeEndian.Uint32(raw[libudevPayloadOff : libudevPayloadOff+4]))
	if propsOffset < headerSize || propsOffset >= len(raw) {
		return nil, errInvalidOffset
	}

	// 4. Calculate payload boundary:
	// If properties_len is specified (> 0), clamp payloadEnd to avoid reading trailing
	// filter hashes or bloom filter bytes (systemd appends 16 bytes of bloom filters at the end).
	// Using subtraction (propsLen <= len(raw)-propsOffset) prevents integer wrap-around.
	propsLen := int(binary.NativeEndian.Uint32(raw[libudevPropsLenOff : libudevPropsLenOff+4]))
	payloadEnd := len(raw)
	if propsLen > 0 && propsLen <= len(raw)-propsOffset {
		payloadEnd = propsOffset + propsLen
	}

	// 5. Parse the NUL-separated "KEY=VALUE" strings into a map.
	env := parseNullSeparatedKeyValues(raw[propsOffset:payloadEnd])
	actionStr, ok := env["ACTION"]
	if !ok || actionStr == "" {
		return nil, errMissingAction
	}
	devPath := env["DEVPATH"]
	if devPath == "" {
		return nil, errMissingDevPath
	}

	return &UEvent{
		Action:    Action(strings.ToLower(actionStr)),
		DevPath:   devPath,
		Subsystem: env["SUBSYSTEM"],
		DevName:   env["DEVNAME"],
		Env:       env,
	}, nil
}

// parseKernelEvent decodes a raw Linux kernel kobject uevent.
// Wire format:
//
//	"<action>@<devpath>\0KEY1=VAL1\0KEY2=VAL2\0..."
func parseKernelEvent(raw []byte) (*UEvent, error) {
	// Locate the NUL terminator of the initial header line ("action@devpath\0").
	idx := bytes.IndexByte(raw, 0)
	if idx == -1 {
		return nil, errors.New("malformed kernel uevent: missing header terminator")
	}

	header := string(raw[:idx])
	actionPart, devPath, found := strings.Cut(header, "@")
	if !found {
		return nil, errors.New("malformed kernel uevent header: missing '@'")
	}

	// Parse remaining NUL-separated "KEY=VALUE" variables.
	env := parseNullSeparatedKeyValues(raw[idx+1:])
	actionStr := actionPart
	if actionStr == "" {
		actionStr = env["ACTION"]
	}
	if actionStr == "" {
		return nil, errMissingAction
	}
	if _, ok := env["ACTION"]; !ok {
		env["ACTION"] = actionStr
	}
	if _, ok := env["DEVPATH"]; !ok && devPath != "" {
		env["DEVPATH"] = devPath
	}

	if env["DEVPATH"] == "" {
		return nil, errMissingDevPath
	}

	return &UEvent{
		Action:    Action(strings.ToLower(actionStr)),
		DevPath:   env["DEVPATH"],
		Subsystem: env["SUBSYSTEM"],
		DevName:   env["DEVNAME"],
		Env:       env,
	}, nil
}

// parseNullSeparatedKeyValues parses a sequence of NUL-terminated "KEY=VALUE" strings
// into a Go map. It safely splits on the first '=' character only, correctly handling
// values that themselves contain '=' (e.g. filesystem labels, UUIDs, or crypt tags).
func parseNullSeparatedKeyValues(data []byte) map[string]string {
	env := make(map[string]string, 32)
	for len(data) > 0 {
		end := bytes.IndexByte(data, 0)
		var entry []byte
		if end == -1 {
			entry = data
			data = nil
		} else {
			entry = data[:end]
			data = data[end+1:]
		}

		if len(entry) == 0 {
			continue
		}

		// Split strictly on the first '=' to preserve values with embedded '='.
		key, val, ok := bytes.Cut(entry, []byte{'='})
		if !ok || len(key) == 0 {
			continue
		}
		env[string(key)] = string(val)
	}
	return env
}
