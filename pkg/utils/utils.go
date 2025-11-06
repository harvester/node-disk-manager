package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// ProcPath is a vfs storing process info for Linux.
	ProcPath = "/proc"
	// HostProcPath is the convention path where host `/proc` is mounted.
	HostProcPath = "/host/proc"
	// DiskRemoveTag indicates a Longhorn is pending to remove.
	DiskRemoveTag = "harvester-ndm-disk-remove"
	// Harvester Namespace
	HarvesterNS = "harvester-system"
	// LVMCSIDriver is the LVM CSI driver name
	LVMCSIDriver = "lvm.driver.harvesterhci.io"
	// LVMTopologyNodeKey is the key of LVM topology node
	LVMTopologyNodeKey = "topology.lvm.csi/node"
	// LonghornSystemNamespaceName is the namespace containing longhorn components
	LonghornSystemNamespaceName = "longhorn-system"
)

var CmdTimeoutError error

var ext4MountOptions = strings.Join([]string{
	"journal_checksum",
	"journal_ioprio=0",
	"barrier=1",
	"errors=remount-ro",
}, ",")

// IsHostProcMounted checks if host's proc info `/proc` is mounted on `/host/proc`
func IsHostProcMounted() (bool, error) {
	_, err := os.Stat(HostProcPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return err == nil, nil
}

// GetFullDevPath will return full path with `/dev/` prefix
func GetFullDevPath(shortPath string) string {
	if shortPath == "" {
		return ""
	}
	return fmt.Sprintf("/dev/%s", shortPath)
}

// MatchesIgnoredCase checks if the item of string slice fully match the key with case-insensitive
func MatchesIgnoredCase(s []string, k string) bool {
	for _, e := range s {
		if strings.EqualFold(e, k) {
			return true
		}
	}
	return false
}

// ContainsIgnoredCase checks if the item of string slice contains the key with case-insensitive
func ContainsIgnoredCase(s []string, k string) bool {
	k = strings.ToLower(k)
	for _, e := range s {
		if strings.Contains(k, strings.ToLower(e)) {
			return true
		}
	}
	return false
}

// MakeExt4DiskFormatting formats the specified volume device to ext4 with the specified UUID
// return error if failed
func MakeExt4DiskFormatting(devPath, uuid string) error {
	args := []string{"-F", devPath}
	if uuid != "" {
		args = append(args, "-U", uuid)
	}
	cmd := exec.Command("mkfs.ext4", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format %s. %v: %s", devPath, err,
			strings.ReplaceAll(strings.TrimSpace(string(output)), "\n", " "))
	}
	return nil
}

// MountDisk mounts the specified ext4 volume device to the specified path
func MountDisk(devPath, mountPoint string) error {
	var needMkdir bool
	if _, err := os.Stat(mountPoint); err != nil && !os.IsNotExist(err) {
		return err
	} else if os.IsNotExist(err) {
		needMkdir = true
	}

	isHostProcMounted, err := IsHostProcMounted()
	if err != nil {
		return err
	}

	if needMkdir {
		if isHostProcMounted {
			if _, err := executeOnHostNamespace("mkdir", []string{"-p", mountPoint}); err != nil {
				return err
			}
		} else {
			if err := os.MkdirAll(mountPoint, os.ModeDir); err != nil {
				return err
			}
		}
	}

	if isHostProcMounted {
		return mountExt4OnHostNamespace(devPath, mountPoint, false)
	}

	return mountExt4(devPath, mountPoint, false)
}

// UmountDisk unmounts the specified volume device to the specified path
func UmountDisk(path string) error {
	isHostProcMounted, err := IsHostProcMounted()
	if err != nil {
		return err
	}
	if isHostProcMounted {
		_, err := executeOnHostNamespace("umount", []string{path})
		return err
	}
	err = syscall.Unmount(path, 0)
	return os.NewSyscallError("umount", err)
}

// ForceUmountWithTimeout umounts the specific device with timeout to the specified path
func ForceUmountWithTimeout(path string, timeout time.Duration) error {
	isHostProcMounted, err := IsHostProcMounted()
	if err != nil {
		return err
	}
	if isHostProcMounted {
		_, err := executeOnHostNamespaceWithTimeout("umount", []string{"-f", path}, timeout)
		return err
	}
	// flags, MNT_FORCE -> 1
	err = syscall.Unmount(path, 1)
	return os.NewSyscallError("umount", err)
}

// mountExt4 mount the ext4 volume device to the specified path with readonly option
func mountExt4(device, path string, readonly bool) error {
	var flags uintptr
	flags = syscall.MS_RELATIME
	if readonly {
		flags |= syscall.MS_RDONLY
	}
	err := syscall.Mount(device, path, "ext4", flags, ext4MountOptions)
	return os.NewSyscallError("mount", err)
}

// mountExt4OnHostNamespace provides the same functionality as mountExt4 but on host namespace.
func mountExt4OnHostNamespace(device, path string, readonly bool) error {
	ns := GetHostNamespacePath(HostProcPath)
	executor, err := NewExecutorWithNS(ns)
	if err != nil {
		return err
	}

	opts := ext4MountOptions + ",relatime"
	if readonly {
		opts = opts + ",ro"
	}

	_, err = executor.Execute("mount", []string{"-t", "ext4", "-o", opts, device, path})
	return err
}

// executeOnHostNamespace executes the command in the host namespace
// return the command result and error
func executeOnHostNamespace(cmd string, args []string) (string, error) {
	ns := GetHostNamespacePath(HostProcPath)
	executor, err := NewExecutorWithNS(ns)
	if err != nil {
		return "", err
	}
	return executor.Execute(cmd, args)
}

// executeOnHostNamespace executes the command with timeout value in the host namespace
// return the command result and error
func executeOnHostNamespaceWithTimeout(cmd string, args []string, cmdTimeout time.Duration) (string, error) {
	ns := GetHostNamespacePath(HostProcPath)
	executor, err := NewExecutorWithNS(ns)
	executor.SetTimeout(cmdTimeout)
	if err != nil {
		return "", err
	}
	return executor.Execute(cmd, args)
}

// IsFSCorrupted checks if the error is caused by a corrupted filesystem
func IsFSCorrupted(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "wrong fs type")
}

// IsSupportedFileSystem checks if the filesystem type is supported
func IsSupportedFileSystem(fsType string) bool {
	if fsType == "ext4" || fsType == "xfs" {
		return true
	}
	return false
}

// CallerWithLock is a helper function to call a function with a condition lock
func CallerWithCondLock[T any](cond *sync.Cond, f func() T) T {
	cond.L.Lock()
	defer cond.L.Unlock()
	return f()
}

// IsMultipathDevice checks if a dm-x device is multipath device
func IsMultipathDevice(path string) (string, error) {
	ns := GetHostNamespacePath(HostProcPath)
	executor, err := NewExecutorWithNS(ns)
	if err != nil {
		return "", fmt.Errorf("failed to create executor with namespace: %v", err)
	}

	// Execute multipath -C dm-x command to check if device is managed by multipath
	// multipath -C returns exit code 0 if device is multipath device, non-zero if not
	output, err := executor.Execute("multipath", []string{"-C", path})
	if err != nil {
		return output, err
	}

	return output, nil
}

// IsManagedByMultipath checks if a /dev/xxx device is managed by multipath
func IsManagedByMultipath(deviceName string) (string, error) {
	ns := GetHostNamespacePath(HostProcPath)
	executor, err := NewExecutorWithNS(ns)
	if err != nil {
		return "", fmt.Errorf("failed to create executor with namespace: %v", err)
	}

	// Execute multipath -u /dev/xxx command to check if device is managed by multipath
	// multipath -u return two results with exit code 0:
	// DM_MULTIPATH_DEVICE_PATH="0" -> device is not managed by multipath
	// DM_MULTIPATH_DEVICE_PATH="1" -> device is managed by multipath
	output, err := executor.Execute("multipath", []string{"-u", fmt.Sprintf("/dev/%s", deviceName)})
	if err != nil {
		return output, err
	}

	output = strings.TrimSpace(output)

	logrus.Debugf("multipath -u output for device %s: %s", deviceName, output)

	if strings.Contains(output, "0") {
		return output, fmt.Errorf("device %s is not managed by multipath", deviceName)
	}

	return output, nil
}

// GetMapperDeviceFromDM retrieves the mapper device for a dm-x device using dmsetup
// For example, dm-0 might return "0QEMU_QEMU_HARDDISK_disk2"
// This provides a stable device name that persists across reboots, unlike dm-x which can change
func GetMapperDeviceFromDM(dmDevice string) (string, error) {
	ns := GetHostNamespacePath(HostProcPath)
	executor, err := NewExecutorWithNS(ns)
	if err != nil {
		return "", fmt.Errorf("failed to create executor with namespace: %v", err)
	}

	dmDevice = strings.TrimPrefix(dmDevice, "/dev/")

	// Execute dmsetup info command to get mapper name
	// dmsetup info -c --noheading -o name /dev/dm-x
	output, err := executor.Execute("dmsetup", []string{"info", "-c", "--noheading", "-o", "name", fmt.Sprintf("/dev/%s", dmDevice)})
	if err != nil {
		return "", fmt.Errorf("failed to get mapper name for %s: %v", dmDevice, err)
	}

	mapperName := strings.TrimSpace(output)
	if mapperName == "" {
		return "", fmt.Errorf("empty mapper name returned for device %s", dmDevice)
	}

	logrus.Debugf("Mapper name for device %s: %s", dmDevice, mapperName)
	return fmt.Sprintf("/dev/mapper/%s", mapperName), nil
}
