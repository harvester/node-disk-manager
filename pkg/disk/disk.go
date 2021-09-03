package disk

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

var gptPartitionOptions = strings.Join([]string{
	"mklabel",
	"gpt",
	"mkpart",
	"primary",
	"ext4",
	"0%",
	"100%",
}, " ")

// MakeGPTPartition making GPT partition table and creating partitions using parted in Linux
func MakeGPTPartition(device string) error {
	logrus.Infof("make gpt partition of device %s", device)
	cmd := exec.Command("parted", "-a", "optimal", "-s", device, gptPartitionOptions)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("stderr: %s, err: %s", stderr.String(), err.Error())
	}
	return nil
}

// MakeExt4DiskFormatting create ext4 filesystem formatting of the specified devPath
func MakeExt4DiskFormatting(devPath, label string) error {
	logrus.Infof("make ext4 format of the device %s", devPath)
	if len(label) > 16 {
		// The maximum length of the volume label is 16 bytes.
		label = label[0:16]
	}
	cmd := exec.Command("mkfs.ext4", "-F", devPath, "-L", label)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("stderr: %s, err: %s", stderr.String(), err.Error())
	}
	return nil
}
