package block

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

func GetParentDevName(devPath string) (string, error) {
	if !strings.HasPrefix(devPath, "/dev") {
		devPath = "/dev/" + devPath
	}
	args := []string{
		"lsblk",
		"-no",
		"pkname",
		devPath,
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("failed to get parent disk, %s", err.Error())
	}

	return strings.TrimSuffix(string(out), "\n"), nil
}

func HasPartitions(disk *Disk) bool {
	return len(disk.Partitions) > 0
}

func GetFileSystemLabel(devPath string) string {
	if !strings.HasPrefix(devPath, "/dev") {
		devPath = "/dev/" + devPath
	}
	args := []string{
		"lsblk",
		"-no",
		"label",
		devPath,
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		logrus.Warnf("failed to get filesystem label for device %s, %s", devPath, err.Error())
		return ""
	}
	splited := strings.SplitN(string(out), "\n", 2)
	if len(splited) > 0 {
		return splited[0]
	}
	return ""
}
