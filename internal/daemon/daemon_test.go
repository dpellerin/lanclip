package daemon

import (
	"crypto/tls"
	"net"
	"testing"
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
