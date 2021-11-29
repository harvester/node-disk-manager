package block

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

func GetParentDevName(devPath string) (string, error) {
	return lsblk(devPath, "pkname")
}

func HasPartitions(disk *Disk) bool {
	return len(disk.Partitions) > 0
}

func GetFileSystemLabel(devPath string) string {
	result, err := lsblk(devPath, "label")
	if err != nil {
		logrus.Warnf(err.Error())
	}
	return result
}

func GetPartType(devPath string) string {
	result, err := lsblk(devPath, "parttype")
	if err != nil {
		logrus.Warnf(err.Error())
	}
	return result
}

func lsblk(devPath, output string) (string, error) {
	if !strings.HasPrefix(devPath, "/dev") {
		devPath = "/dev/" + devPath
	}
	args := []string{
		"lsblk",
		"-dno",
		output,
		devPath,
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute `%s`: %s", strings.Join(args, " "), err.Error())
	}

	return strings.TrimSuffix(string(out), "\n"), nil
}
