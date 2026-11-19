package main

import (
	"os/exec"
	"strings"
)

func nativeLocale() string {
	out, err := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if tag := strings.Trim(strings.TrimSpace(line), "(),\" "); tag != "" {
				return tag
			}
		}
	}
	return ""
}
