package disk

import (
	"bytes"
	"fmt"
	"os/exec"
)

// MakeExt4DiskFormatting create ext4 filesystem formatting of the specified devPath
func MakeExt4DiskFormatting(devPath, uuid string) error {
	args := []string{"-F", devPath}
	if uuid != "" {
		args = append(args, "-U", uuid)
	}
	cmd := exec.Command("mkfs.ext4", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("stderr: %s, err: %s", stderr.String(), err.Error())
	}
	return nil
}
