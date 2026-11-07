//go:build linux

package clipboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// The adapter runs its own executable as the wl-paste callback.
	if len(os.Args) == 3 && os.Args[1] == "clipboard-event" {
		if err := SendWatchEvent(os.Args[2], os.Stdin, 1<<20); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNativeClipboardOrigin(t *testing.T) {
	if os.Getenv("LANCLIP_TEST_ISOLATED_WAYLAND") != "1" {
		t.Skip("run in an isolated Wayland session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := New(1 << 20)
	a.socket = filepath.Join(t.TempDir(), "clipboard.sock")
	events, err := a.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	go func() {
		for range events {
		}
	}()

	for _, mime := range []string{"text/plain", "image/png"} {
		const text = "Lanclip test café"
		if err := a.Write(ctx, text); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			out, err := exec.CommandContext(ctx, "wl-paste", "--no-newline").Output()
			if err != nil || string(out) != text || !a.Remote() {
				t.Fatalf("paste failed or cleared remote ownership: %q, %v", out, err)
			}
		}

		copy := exec.CommandContext(ctx, "wl-copy", "--type", mime)
		copy.Stdin = strings.NewReader(text)
		if err := copy.Run(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for a.Remote() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if a.Remote() {
			t.Fatalf("local replacement of type %s retained remote ownership", mime)
		}
	}
}
