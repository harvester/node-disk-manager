// Package netlink provides a lightweight, pure-Go implementation for listening
// and decoding device uevents directly from Linux NETLINK_KOBJECT_UEVENT multicast
// sockets without CGO or external dependencies.
//
// Background & Architectural Context:
//   - Official libudev / sd-device libraries are written in C (LGPLv2.1+) and require
//     CGO, which prevents pure-static Go compilation (CGO_ENABLED=0) and introduces
//     glibc/systemd runtime dependencies incompatible with minimal container environments.
//   - Raw kernel events on multicast group 1 carry only minimal device metadata
//     (lacking serial numbers, WWNs, filesystem labels, etc.).
//   - Udev-processed events on multicast group 2 carry the enriched hardware properties
//     populated by udev rules. These are broadcast by systemd-udevd using an internal wire
//     format (struct udev_monitor_netlink_header) with magic 0xfeedcafe followed by
//     NUL-separated environment strings.
//   - This package implements that wire protocol directly under the Apache 2.0 license,
//     integrating with Go's runtime epoll poller for non-blocking I/O and zero-CPU idling.
package netlink

import "errors"

// ErrUntrustedSender indicates a netlink message that was not multicast by
// udev or the kernel and therefore must not be acted upon.
var ErrUntrustedSender = errors.New("untrusted udev netlink sender")

// Action represents a device event action (e.g., "add", "remove", "change").
type Action string

const (
	ActionAdd    Action = "add"
	ActionRemove Action = "remove"
	ActionChange Action = "change"
	ActionRescan Action = "rescan" // Synthetic event triggered on buffer overrun (ENOBUFS) to force full reconciliation.
)

// UEvent represents a decoded uevent received from udev or kernel.
type UEvent struct {
	Action    Action
	DevPath   string
	Subsystem string
	DevName   string
	Env       map[string]string
}
