//go:build linux

package config

import "os"

func platformDeviceName() string {
	name, _ := os.Hostname()
	return cleanDeviceName(name)
}
