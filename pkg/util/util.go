package util

import (
	"fmt"
	"strings"
)

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
