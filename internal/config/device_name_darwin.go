//go:build darwin

package config

import (
	"os"
	"os/exec"
)

func platformDeviceName() string {
	// ComputerName is the user-facing name shown in System Settings and Finder.
	// os.Hostname can instead return a generic or network-oriented value such as
	// "Mac", which is not useful when choosing a Lanclip peer.
	if output, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil {
		if name := cleanDeviceName(string(output)); name != "" {
			return name
		}
	}
	name, _ := os.Hostname()
	return cleanDeviceName(name)
}
