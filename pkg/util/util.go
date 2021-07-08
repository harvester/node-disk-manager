package util

import (
	"fmt"
	"strings"
)

const (
	LonghornBusPathSubstring = "longhorn"
)

func GetBlockDeviceName(deviceName, nodeName string) string {
	return fmt.Sprintf("%s-%s", deviceName, nodeName)
}

func IsLonghornBlockDevice(path string) bool {
	return strings.Contains(path, LonghornBusPathSubstring)
}
