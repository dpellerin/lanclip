package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dpellerin/lanclip/internal/control"
	"github.com/dpellerin/lanclip/internal/daemon"
	"github.com/dpellerin/lanclip/internal/pairing"
)

func TestGroupedFingerprintRejectsOversizedInput(t *testing.T) {
	for _, n := range []int{65, 1 << 20} {
		if got := groupedFingerprint(strings.Repeat("a", n)); got != "Invalid fingerprint" {
			t.Errorf("%d-byte fingerprint: expected rejection, got %d output bytes", n, len(got))
		}
	}
}

func TestBuiltCLIRejectsOversizedFingerprintFromControlSocket(t *testing.T) {
	binary := os.Getenv("LANCLIP_TEST_BINARY")
	if binary == "" {
		t.Skip("set LANCLIP_TEST_BINARY to exercise a freshly built native CLI")
	}
	if runtime.GOOS != "linux" {
		t.Skip("isolated XDG control socket test is Linux-specific")
	}
	dir, err := os.MkdirTemp("", "lc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	server, err := control.Listen(filepath.Join(dir, "lanclip.sock"), func(_ context.Context, req control.Request) control.Response {
		if req.Command != "peers" {
			return control.Response{Error: "read-only test server"}
		}
		return control.Response{OK: true, Data: []daemon.PeerStatus{{Peer: pairing.Peer{
			ID: testPeerID, Name: "Synthetic peer", State: pairing.Pending,
			Fingerprint: strings.Repeat("a", 1<<20), ComparisonCode: "synthetic comparison code",
		}}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	cmd := exec.Command(binary, "peers")
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "Fingerprint: Invalid fingerprint") || len(output) > 1024 {
		t.Fatalf("CLI did not reject oversized fingerprint: %d output bytes", len(output))
	}
	t.Logf("built CLI roundtrip over isolated user-only Unix socket: %d output bytes, oversized fingerprint rejected", len(output))
}

func TestGroupedFingerprintPreservesSupportedLengths(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""},
		{"a", "a"},
		{"abcd", "abcd"},
		{"abcde", "abcd e"},
		{"0123456789abcdef", "0123 4567 89ab cdef"},
		{strings.Repeat("abcd", 16), strings.TrimSpace(strings.Repeat("abcd ", 16))},
	} {
		if got := groupedFingerprint(tc.input); got != tc.want {
			t.Errorf("groupedFingerprint(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
