package block

import (
	"strings"

	"github.com/harvester/node-disk-manager/pkg/utils"
	"github.com/sirupsen/logrus"
)

const (
	BLKIDCMD = "blkid"
)

// doCommandBlkid runs `blkid -o export` for a given device and returns
// a map of whatever tags are set for that device, for example we might
// get something like this for a provisioned Longhorn V1 device:
//
//	# blkid -o export /dev/sdd
//	DEVNAME=/dev/sdd
//	UUID=e4e57552-f7cb-4934-864e-56dee55ae7da
//	BLOCK_SIZE=4096
//	TYPE=ext4
//
// The map will be empty if blkid can't find anything interesting on
// the device.
func doCommandBlkid(partition string) (blkidInfo map[string]string) {
	blkidInfo = make(map[string]string)
	if !strings.HasPrefix(partition, "/dev") {
		partition = "/dev/" + partition
	}
	out, err := utils.NewExecutor().Execute(BLKIDCMD, []string{"-o", "export", partition})
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"partition": partition,
			"error":     err.Error(),
		}).Debug("failed to read disk info")
		return
	}
	for line := range strings.SplitSeq(out, "\n") {
		kv := strings.Split(line, "=")
		if len(kv) == 2 {
			blkidInfo[kv[0]] = kv[1]
		}
	}
	return blkidInfo
}
