package disk

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/sirupsen/logrus"
)

// MakeExt4DiskFormatting create ext4 filesystem formatting of the specified devPath
func MakeExt4DiskFormatting(devPath string) error {
	logrus.Infof("make ext4 format of the device %s", devPath)
	cmd := exec.Command("mkfs.ext4", "-F", devPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("stderr: %s, err: %s", stderr.String(), err.Error())
	}
	return nil
}
