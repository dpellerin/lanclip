package daemon

import (
	"context"
	"crypto/tls"
	"net"
	"testing"

	"github.com/dpellerin/lanclip/internal/protocol"
)

func TestRegisterReplacesStaleConnection(t *testing.T) {
	d := &Daemon{conns: map[string]*peerConn{}, stats: map[string]Stats{}}
	oldLocal, oldRemote := net.Pipe()
	defer oldRemote.Close()
	nextLocal, nextRemote := net.Pipe()
	defer nextLocal.Close()
	defer nextRemote.Close()
	old := &peerConn{id: "peer", conn: tls.Client(oldLocal, &tls.Config{})}
	next := &peerConn{id: "peer", conn: tls.Client(nextLocal, &tls.Config{})}
	if !d.register(old) || !d.register(next) {
		t.Fatal("valid connections were not registered")
	}
	if d.conns["peer"] != next {
		t.Fatal("fresh connection did not replace stale connection")
	}
	d.unregister(old)
	if d.conns["peer"] != next || !d.stats["peer"].Connected {
		t.Fatal("stale unregister removed the fresh connection")
	}
	if got := d.stats["peer"].Reconnects; got != 2 {
		t.Fatalf("reconnects=%d", got)
	}
}

func TestPeerQueueKeepsLatestClipboard(t *testing.T) {
	d := &Daemon{ctx: context.Background()}
	pc := &peerConn{out: make(chan protocol.Message, 1), done: make(chan struct{})}
	d.enqueuePeer(pc, protocol.Message{Type: "clipboard", Text: "first"})
	d.enqueuePeer(pc, protocol.Message{Type: "clipboard", Text: "latest"})
	if got := (<-pc.out).Text; got != "latest" {
		t.Fatalf("queued=%q", got)
	}
}

func TestInboundClipboardQueueKeepsLatest(t *testing.T) {
	d := &Daemon{ctx: context.Background(), clipWrites: make(chan inboundClipboard, 1)}
	d.enqueueClipboard(inboundClipboard{text: "first"})
	d.enqueueClipboard(inboundClipboard{text: "latest"})
	if got := (<-d.clipWrites).text; got != "latest" {
		t.Fatalf("queued=%q", got)
	}
}

func TestPairAttemptsAreRateLimited(t *testing.T) {
	d := &Daemon{pairRates: map[string]pairRate{}}
	addr := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 1234}
	for i := 0; i < maxPairAttemptsPerMinute; i++ {
		if !d.allowPairAttempt(addr) {
			t.Fatalf("attempt %d was unexpectedly limited", i)
		}
	}
	if d.allowPairAttempt(addr) {
		t.Fatal("accepted attempt over rate limit")
	}
}
