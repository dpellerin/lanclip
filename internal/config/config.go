package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dpellerin/lanclip/internal/pairing"
)

const (
	DefaultPort = 24872
	MaxPayload  = 1 << 20
)

type Config struct {
	Version           int      `json:"version"`
	Name              string   `json:"name"`
	ListenPort        int      `json:"listen_port"`
	ServiceType       string   `json:"service_type"`
	MaxClipboardBytes int      `json:"max_clipboard_bytes"`
	ManualPeers       []string `json:"manual_peers"`
}

type Paths struct {
	ConfigDir, DataDir, RuntimeDir  string
	Config, Identity, Peers, Socket string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	if runtime.GOOS == "darwin" {
		base := filepath.Join(home, "Library", "Application Support", "Lanclip")
		return Paths{base, base, base, filepath.Join(base, "config.json"), filepath.Join(base, "identity.pem"), filepath.Join(base, "peers.json"), filepath.Join(base, "lanclip.sock")}, nil
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	run := os.Getenv("XDG_RUNTIME_DIR")
	if run == "" {
		return Paths{}, errors.New("XDG_RUNTIME_DIR is not set")
	}
	cfg, data = filepath.Join(cfg, "lanclip"), filepath.Join(data, "lanclip")
	return Paths{cfg, data, run, filepath.Join(cfg, "config.json"), filepath.Join(data, "identity.pem"), filepath.Join(data, "peers.json"), filepath.Join(run, "lanclip.sock")}, nil
}

func Default() Config {
	return Config{1, deviceName(), DefaultPort, "_lanclip._tcp", MaxPayload, []string{}}
}

func LoadOrCreate(paths Paths) (Config, error) {
	for _, dir := range []string{paths.ConfigDir, paths.DataDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return Config{}, err
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return Config{}, err
		}
	}
	b, err := os.ReadFile(paths.Config)
	if errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		if cfg.Name == "" {
			return Config{}, errors.New("could not determine this device's name")
		}
		if err := save(paths.Config, cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Version != 1 {
		return Config{}, fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.Name == "" || cfg.ListenPort < 1 || cfg.ListenPort > 65535 || cfg.MaxClipboardBytes < 1 || cfg.MaxClipboardBytes > MaxPayload {
		return Config{}, errors.New("invalid config values")
	}
	if cfg.ServiceType == "" {
		cfg.ServiceType = "_lanclip._tcp"
	}
	// The device name is host identity metadata, not a user-maintained alias.
	// Refresh it on every daemon start so machine renames and older macOS
	// installs that captured a generic hostname are corrected automatically.
	if current := deviceName(); current != "" && current != cfg.Name {
		cfg.Name = current
		if err := save(paths.Config, cfg); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func save(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

var deviceName = platformDeviceName

func cleanDeviceName(name string) string {
	return pairing.NormalizeDeviceName(name)
}

func CheckPrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%s permissions are %o, want no group/other access", path, info.Mode().Perm())
	}
	return nil
}
