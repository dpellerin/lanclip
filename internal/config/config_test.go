package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateAndPermissions(t *testing.T) {
	originalDeviceName := deviceName
	deviceName = func() string { return "Office Linux" }
	t.Cleanup(func() { deviceName = originalDeviceName })
	base := t.TempDir()
	p := Paths{ConfigDir: filepath.Join(base, "c"), DataDir: filepath.Join(base, "d"), RuntimeDir: base}
	p.Config, p.Identity, p.Peers, p.Socket = filepath.Join(p.ConfigDir, "config.json"), filepath.Join(p.DataDir, "identity.pem"), filepath.Join(p.DataDir, "peers.json"), filepath.Join(base, "sock")
	c, err := LoadOrCreate(p)
	if err != nil || c.ListenPort != DefaultPort || c.Name != "Office Linux" {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	if err := CheckPrivate(p.Config); err != nil {
		t.Fatal(err)
	}
	if mode, _ := os.Stat(p.Config); mode.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", mode.Mode().Perm())
	}
}

func TestLoadRefreshesSavedDeviceName(t *testing.T) {
	originalDeviceName := deviceName
	deviceName = func() string { return "Studio Mac" }
	t.Cleanup(func() { deviceName = originalDeviceName })

	base := t.TempDir()
	p := Paths{ConfigDir: filepath.Join(base, "c"), DataDir: filepath.Join(base, "d"), RuntimeDir: base}
	p.Config, p.Identity, p.Peers, p.Socket = filepath.Join(p.ConfigDir, "config.json"), filepath.Join(p.DataDir, "identity.pem"), filepath.Join(p.DataDir, "peers.json"), filepath.Join(base, "sock")
	if err := os.MkdirAll(p.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	stale := Default()
	stale.Name = "Mac"
	if err := save(p.Config, stale); err != nil {
		t.Fatal(err)
	}

	got, err := LoadOrCreate(p)
	if err != nil || got.Name != "Studio Mac" {
		t.Fatalf("config=%+v err=%v", got, err)
	}
	var persisted Config
	b, err := os.ReadFile(p.Config)
	if err != nil || json.Unmarshal(b, &persisted) != nil || persisted.Name != got.Name {
		t.Fatalf("persisted=%+v readErr=%v", persisted, err)
	}
}

func TestCleanDeviceNameRemovesControlCharacters(t *testing.T) {
	if got := cleanDeviceName("  Studio\nMac\x1b  "); got != "StudioMac" {
		t.Fatalf("name=%q", got)
	}
}
