package block

import (
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

func GetFileSystemType(part string) string {
	if !strings.HasPrefix(part, "/dev") {
		part = "/dev/" + part
	}
	args := []string{
		"blkid",
		"-s",
		FsType,
		part,
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		logrus.Warnf("failed to read disk uuid of %s : %s\n", part, err.Error())
		return ""
	}

	if out == nil || len(out) == 0 {
		return ""
	}

	parts := strings.Split(string(out), "TYPE=")
	if len(parts) != 2 {
		logrus.Warnf("failed to parse the type of %s\n", part)
		return ""
	}

	return strings.ReplaceAll(strings.TrimSpace(parts[1]), `"`, "")
}

func GetDiskUUID(part string, uuidType string) string {
	if !strings.HasPrefix(part, "/dev") {
		part = "/dev/" + part
	}
	args := []string{
		"blkid",
		"-s",
		uuidType,
		part,
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		logrus.Warnf("failed to read disk uuid of %s : %s\n", part, err.Error())
		return ""
	}

	if out == nil || len(out) == 0 {
		return ""
	}

	parts := strings.Split(string(out), "UUID=")
	if len(parts) != 2 {
		logrus.Warnf("failed to parse the uuid of %s\n", part)
		return ""
	}

	return strings.ReplaceAll(strings.TrimSpace(parts[1]), `"`, "")
}
