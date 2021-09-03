package disk

import (
	"os"
	"strings"
	"syscall"

	iscsiutil "github.com/longhorn/go-iscsi-helper/util"
	"github.com/longhorn/node-disk-manager/pkg/util"
)

var ext4MountOptions = strings.Join([]string{
	"journal_checksum",
	"journal_ioprio=0",
	"barrier=1",
	"errors=remount-ro",
}, ",")

// MountDisk mounts the specified ext4 volume device to the specified path
func MountDisk(devPath, mountPoint string) error {
	isHostProcMounted, err := util.IsHostProcMounted()
	if err != nil {
		return err
	}
	if isHostProcMounted {
		if _, err := executeOnHostNamespace("mkdir", []string{"-p", mountPoint}); err != nil {
			return err
		}
		return mountExt4OnHostNamespace(devPath, mountPoint, false)
	}

	if err := os.MkdirAll(mountPoint, os.ModeDir); err != nil {
		return err
	}
	return mountExt4(devPath, mountPoint, false)
}

// UmountDisk unmounts the specified volume device to the specified path
func UmountDisk(path string) error {
	isHostProcMounted, err := util.IsHostProcMounted()
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
	ns := iscsiutil.GetHostNamespacePath(util.HostProcPath)
	executor, err := iscsiutil.NewNamespaceExecutor(ns)
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

func executeOnHostNamespace(cmd string, args []string) (string, error) {
	ns := iscsiutil.GetHostNamespacePath(util.HostProcPath)
	executor, err := iscsiutil.NewNamespaceExecutor(ns)
	if err != nil {
		return "", err
	}
	return executor.Execute(cmd, args)
}
