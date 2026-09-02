package pairing

import (
	"strings"
	"testing"
)

func TestNormalizeDeviceName(t *testing.T) {
	got := NormalizeDeviceName("  Studio\n\x1b[2J\u202eMac  ")
	if got != "Studio[2JMac" {
		t.Fatalf("name=%q", got)
	}
	long := strings.Repeat("x", MaxDeviceNameRunes+10)
	if got := NormalizeDeviceName(long); len([]rune(got)) != MaxDeviceNameRunes {
		t.Fatalf("runes=%d", len([]rune(got)))
	}
}
