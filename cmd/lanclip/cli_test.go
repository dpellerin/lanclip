package main

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dpellerin/lanclip/internal/clipboard"
	"github.com/dpellerin/lanclip/internal/control"
	"github.com/dpellerin/lanclip/internal/daemon"
	"github.com/dpellerin/lanclip/internal/pairing"
)

const (
	testPeerID  = "bce45a94-d129-4d06-ad6d-5cc70156f334"
	testLocalID = "d1202a65-f928-41d0-a35d-dcc745a9bf42"
	testFP      = "949cb6b305c5419f9b325dd207a6eb89cdd236d64f4ad25075096cd530000ed0"
)

func TestStatusIsHumanReadableAndHidesMachineIdentifiers(t *testing.T) {
	now := time.Now()
	status := daemon.Status{
		Version: "0.1.0-dev", Uptime: "12m3s", Name: "Linux Desktop", DeviceID: testLocalID, Fingerprint: testFP,
		Clipboard: clipboard.Health{Running: true, LastEvent: now.Format(time.RFC3339Nano)},
		Discovery: map[string]any{"last_browse": now.Format(time.RFC3339Nano), "last_error": "", "peers_seen": 1},
		Peers: []daemon.PeerStatus{{
			Peer:  pairing.Peer{ID: testPeerID, Name: "Mac", State: pairing.Trusted},
			Stats: daemon.Stats{Connected: true, Address: "192.0.2.20:24872", LastSent: now, SentBytes: 22, Reconnects: 2},
		}},
	}
	out := runControlForTest(t, []string{"status"}, "", func(_ context.Context, _ string, request control.Request) (control.Response, error) {
		if request.Command != "status" {
			t.Fatalf("command=%q", request.Command)
		}
		return control.Response{OK: true, Data: status}, nil
	})
	for _, want := range []string{"Lanclip 0.1.0-dev", "This device:  Linux Desktop", "Clipboard:    ready", "Discovery:    healthy", "Mac — connected", "Reconnects:    2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"{", `"device_id"`, testLocalID, testPeerID, testFP} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, out)
		}
	}
}

func TestPairUsesNumberedSelectionAndSendsHiddenID(t *testing.T) {
	peers := []daemon.PeerStatus{
		{Peer: pairing.Peer{ID: "first-id", Name: "Desktop", State: "unpaired"}, Addresses: []string{"192.0.2.10:24872"}},
		{Peer: pairing.Peer{ID: testPeerID, Name: "Mac", Fingerprint: testFP, State: "unpaired"}, Addresses: []string{"192.0.2.20:24872"}},
	}
	var sent control.Request
	out := runControlForTest(t, []string{"pair"}, "2\n", func(_ context.Context, _ string, request control.Request) (control.Response, error) {
		if request.Command == "peers" {
			return control.Response{OK: true, Data: peers}, nil
		}
		sent = request
		return control.Response{OK: true, Data: pairing.Peer{ID: testPeerID, Name: "Mac", Fingerprint: testFP, ComparisonCode: "amber lake solar fern jade blue"}}, nil
	})
	if sent.Command != "pair" || sent.Argument != testPeerID {
		t.Fatalf("request=%+v", sent)
	}
	for _, want := range []string{"1) Desktop", "2) Mac", "Pairing request sent to Mac", "amber lake solar fern jade blue", "lanclip approve \"Mac\""} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, testPeerID) {
		t.Fatalf("machine ID leaked into output:\n%s", out)
	}
}

func TestApproveDefaultsSingleSelectionAndConfirmsComparison(t *testing.T) {
	peer := daemon.PeerStatus{Peer: pairing.Peer{ID: testPeerID, Name: "Mac", Fingerprint: testFP, State: pairing.Pending, ComparisonCode: "amber lake solar fern jade blue", ApprovalToken: "approval-token"}}
	var sent control.Request
	out := runControlForTest(t, []string{"approve"}, "\ny\n", func(_ context.Context, _ string, request control.Request) (control.Response, error) {
		if request.Command == "peers" {
			return control.Response{OK: true, Data: []daemon.PeerStatus{peer}}, nil
		}
		sent = request
		return control.Response{OK: true}, nil
	})
	if sent.Command != "approve" || sent.Argument != testPeerID {
		t.Fatalf("request=%+v", sent)
	}
	if sent.PairToken != peer.ApprovalToken || sent.Fingerprint != peer.Fingerprint || sent.Code != peer.ComparisonCode {
		t.Fatalf("approval was not bound to displayed pairing: %+v", sent)
	}
	for _, want := range []string{"Selection [1]", "Do the code and fingerprint match", "Approved Mac"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestUnpairCancellationMakesNoChange(t *testing.T) {
	peer := daemon.PeerStatus{Peer: pairing.Peer{ID: testPeerID, Name: "Mac", State: pairing.Trusted}}
	calls := 0
	out := runControlForTest(t, []string{"unpair", "Mac"}, "n\n", func(_ context.Context, _ string, request control.Request) (control.Response, error) {
		calls++
		if request.Command != "peers" {
			t.Fatalf("unexpected mutation request: %+v", request)
		}
		return control.Response{OK: true, Data: []daemon.PeerStatus{peer}}, nil
	})
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	if !strings.Contains(out, "Cancelled. No changes were made.") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestDoctorUsesReadableCheckList(t *testing.T) {
	checks := []daemon.DoctorCheck{{Name: "clipboard watcher", OK: true}, {Name: "peer discovery", OK: false, Detail: "no devices found"}}
	out := runControlForTest(t, []string{"doctor"}, "", func(_ context.Context, _ string, _ control.Request) (control.Response, error) {
		return control.Response{OK: true, Data: checks}, nil
	})
	for _, want := range []string{"[ok] clipboard watcher", "[!!] peer discovery — no devices found", "1 of 2 checks passed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "{") {
		t.Fatalf("JSON leaked into output:\n%s", out)
	}
}

func TestPeerNamesCannotInjectTerminalControls(t *testing.T) {
	peer := daemon.PeerStatus{Peer: pairing.Peer{ID: testPeerID, Name: "Mac\x1b[2J\u202e", State: pairing.Trusted}}
	out := runControlForTest(t, []string{"peers"}, "", func(_ context.Context, _ string, _ control.Request) (control.Response, error) {
		return control.Response{OK: true, Data: []daemon.PeerStatus{peer}}, nil
	})
	if strings.ContainsAny(out, "\x1b\u202e") || !strings.Contains(out, "Mac[2J") {
		t.Fatalf("unsafe output: %q", out)
	}
}

func runControlForTest(t *testing.T, args []string, input string, call controlCaller) string {
	t.Helper()
	var output bytes.Buffer
	if err := runControl(args, "test.sock", bufio.NewReader(strings.NewReader(input)), &output, call); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
