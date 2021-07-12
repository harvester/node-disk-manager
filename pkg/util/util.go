package util

import (
	"fmt"
	"strings"
)

func GetBlockDeviceName(deviceName, nodeName string) string {
	return fmt.Sprintf("%s-%s", deviceName, nodeName)
}

func GetFullDevPath(shortPath string) string {
	if shortPath == "" {
		return ""
	}
	return fmt.Sprintf("/dev/%s", shortPath)
}

func ContainsIgnoredCase(s []string, k string) bool {
	for _, e := range s {
		if strings.EqualFold(e, k) {
			return true
		}
	}
	return false
}
