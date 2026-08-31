package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This guards the most likely accidental payload-logging regression. Runtime
// integration is additionally checked during physical UAT.
func TestSourceDoesNotLogClipboardText(t *testing.T) {
	root := filepath.Join("..", "..")
	secret := "TEST_CLIPBOARD_SECRET_DO_NOT_LOG"
	_ = secret
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "slog.") && (strings.Contains(line, "m.Text") || strings.Contains(line, "e.Text")) {
				t.Errorf("possible clipboard payload logging in %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
