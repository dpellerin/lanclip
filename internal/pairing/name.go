package pairing

import (
	"strings"
	"unicode"
)

const MaxDeviceNameRunes = 128

// NormalizeDeviceName makes network-supplied display metadata safe for logs,
// terminals, and prompts. Device names never participate in authentication.
func NormalizeDeviceName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return -1
		}
		return r
	}, name))
	runes := []rune(name)
	if len(runes) > MaxDeviceNameRunes {
		name = string(runes[:MaxDeviceNameRunes])
	}
	return name
}
