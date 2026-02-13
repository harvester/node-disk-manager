package block

import (
	"strings"

	"github.com/harvester/node-disk-manager/pkg/utils"
	"github.com/sirupsen/logrus"
)

const (
	BLKIDCMD = "blkid"
)

func doCommandBlkid(partition string, param string) (string, error) {
	if !strings.HasPrefix(partition, "/dev") {
		partition = "/dev/" + partition
	}
	return utils.NewExecutor().Execute(BLKIDCMD, []string{
		"-s",
		param,
		partition,
		"-o",
		"value"})
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
	return strings.Split(out, "\n")[0]
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
	return strings.Split(out, "\n")[0]
}
