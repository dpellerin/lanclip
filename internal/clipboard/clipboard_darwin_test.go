//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestNativeClipboardRoundTrip is opt-in because it temporarily replaces the
// user's text clipboard. It restores the original text before returning.
func TestNativeClipboardRoundTrip(t *testing.T) {
	if os.Getenv("LANCLIP_TEST_NATIVE_CLIPBOARD") != "1" {
		t.Skip("set LANCLIP_TEST_NATIVE_CLIPBOARD=1 to exercise NSPasteboard")
	}

	original, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Skip("current clipboard is not restorable plain text")
	}
	t.Cleanup(func() {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = bytes.NewReader(original)
		if err := cmd.Run(); err != nil {
			t.Errorf("restore clipboard: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := New(1 << 20)
	events, err := a.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	watched := fmt.Sprintf("lanclip native watch %d\nUnicode: café 🙂", time.Now().UnixNano())
	copyIn := exec.Command("pbcopy")
	copyIn.Stdin = bytes.NewBufferString(watched)
	if err := copyIn.Run(); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Text != watched {
			t.Fatalf("watcher returned %q", event.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native clipboard watcher timed out")
	}

	written := fmt.Sprintf("lanclip native write %d\nUnicode: naïve 🚀", time.Now().UnixNano())
	if err := a.Write(ctx, written); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != written {
		t.Fatalf("native write returned %q", got)
	}
}
