package util

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	// ProcPath is a vfs storing process info for Linux.
	ProcPath = "/proc"
	// HostProcPath is the convention path where host `/proc` is mounted.
	HostProcPath = "/host/proc"
)

// IsHostProcMounted checks if host's proc info `/proc` is mounted on `/host/proc`
func IsHostProcMounted() (bool, error) {
	_, err := os.Stat(HostProcPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return err == nil, nil
}

func GetFullDevPath(shortPath string) string {
	if shortPath == "" {
		return ""
	}
	return fmt.Sprintf("/dev/%s", shortPath)
}

func GetDiskPartitionPath(devicePath string, partitionNum int) string {
	partitionSep := ""
	last := devicePath[len(devicePath)-1:]
	if _, err := strconv.Atoi(last); err == nil {
		// If a disk device name ends with a digit then the Linux Kernel adds
		// the character 'p' to separate the partition number from the device name.
		// Example: /dev/nvme0n1 -> /dev/nvme0n1p1
		partitionSep = "p"
	}
	return fmt.Sprintf("%s%s%d", devicePath, partitionSep, partitionNum)
}

func ContainsIgnoredCase(s []string, k string) bool {
	for _, e := range s {
		if strings.EqualFold(e, k) {
			return true
		}
	}
	return false
}
