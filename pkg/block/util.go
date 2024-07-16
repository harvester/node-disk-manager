package block

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	LSBLKCMD = "lsblk"
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
		logrus.Debugf(err.Error())
	}
	return result
}

func GetPartType(devPath string) string {
	result, err := lsblk(devPath, "parttype")
	if err != nil {
		logrus.Debugf(err.Error())
	}
	return result
}

func GetDevPathByPTUUID(ptUUID string) (string, error) {
	args := []string{"-dJo", "PATH,PTUUID"}
	out, err := exec.Command(LSBLKCMD, args[0:]...).Output() // #nosec G204
	if err != nil {
		return "", fmt.Errorf("failed to execute `%s` for PTUUID %s: %w", strings.Join(args, " "), ptUUID, err)
	}

	bds := struct {
		BlockDevices []struct {
			Path   string `json:"path"`
			PTUUID string `json:"ptuuid"`
		} `json:"blockdevices"`
	}{}
	if err := json.Unmarshal(out, &bds); err != nil {
		return "", fmt.Errorf("failed to unmarshal lsblk for PTUUID `%s`: %w", ptUUID, err)
	}

	for _, bd := range bds.BlockDevices {
		if bd.PTUUID == ptUUID {
			return bd.Path, nil
		}
	}

	return "", nil
}

func lsblk(devPath, output string) (string, error) {
	if !strings.HasPrefix(devPath, "/dev") {
		devPath = "/dev/" + devPath
	}
	args := []string{
		"-dno",
		output,
		devPath,
	}
	out, err := exec.Command(LSBLKCMD, args[0:]...).Output() // #nosec G204
	if err != nil {
		return "", fmt.Errorf("failed to execute `%s %s`: %s", LSBLKCMD, strings.Join(args, " "), err.Error())
	}

	return strings.TrimSuffix(string(out), "\n"), nil
}
