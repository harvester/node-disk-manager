package block

import (
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

func doCommandBlkid(partition string, param string) ([]byte, error) {
	if !strings.HasPrefix(partition, "/dev") {
		partition = "/dev/" + partition
	}
	args := []string{
		"blkid",
		"-s",
		param,
		partition,
		"-o",
		"value",
	}
	return exec.Command(args[0], args[1:]...).Output()
}

func GetFileSystemType(part string) string {
	out, err := doCommandBlkid(part, FsType)

	if err != nil {
		logrus.Debugf("failed to read disk uuid of %s : %s\n", part, err.Error())
		return ""
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Split(string(out), "\n")[0]
}

func GetDiskUUID(part string, uuidType string) string {
	out, err := doCommandBlkid(part, uuidType)
	if err != nil {
		logrus.Debugf("failed to read disk uuid of %s : %s\n", part, err.Error())
		return ""
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Split(string(out), "\n")[0]
}
