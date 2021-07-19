package disk

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

var gptPartitionOptions = strings.Join([]string{
	"mklabel",
	"gpt",
	"mkpart",
	"primary",
	"ext4",
	"2048s",
	"100%",
}, " ")

// MakeGPTPartition making GPT partition table and creating partitions using parted in Linux
func MakeGPTPartition(device string) error {
	cmd := exec.Command("parted", "-s", device, gptPartitionOptions)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s", stderr.String())
	}
	return nil
}

// MakeDiskFormatting making GPT partition table and creating partitions using parted in Linux
func MakeDiskFormatting(device, fsType string) error {
	cmd := exec.Command("mkfs.ext4", device)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s", stderr.String())
	}
	return nil
}
