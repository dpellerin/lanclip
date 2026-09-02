package transport

import (
	"crypto/tls"
	"crypto/x509"
	"path/filepath"
	"testing"

	"github.com/dpellerin/lanclip/internal/identity"
	"github.com/dpellerin/lanclip/internal/pairing"
)

func TestPinnedTLSAcceptsTrustedAndRejectsUnknown(t *testing.T) {
	d := t.TempDir()
	serverID, e := identity.LoadOrCreate(filepath.Join(d, "server.pem"))
	if e != nil {
		t.Fatal(e)
	}
	clientID, e := identity.LoadOrCreate(filepath.Join(d, "client.pem"))
	if e != nil {
		t.Fatal(e)
	}
	serverStore, e := pairing.Load(filepath.Join(d, "server-peers.json"))
	if e != nil {
		t.Fatal(e)
	}
	clientStore, e := pairing.Load(filepath.Join(d, "client-peers.json"))
	if e != nil {
		t.Fatal(e)
	}
	trust(t, serverStore, clientID)
	trust(t, clientStore, serverID)
	scfg, e := ServerConfig(serverID, serverStore)
	if e != nil {
		t.Fatal(e)
	}
	ccfg, e := ClientConfig(clientID, clientStore, ALPNSync, serverID.Fingerprint()[:16])
	if e != nil {
		t.Fatal(e)
	}
	clientCert, e := x509.ParseCertificate(clientID.Certificate)
	if e != nil {
		t.Fatal(e)
	}
	serverCert, e := x509.ParseCertificate(serverID.Certificate)
	if e != nil {
		t.Fatal(e)
	}
	if e = scfg.VerifyConnection(tls.ConnectionState{NegotiatedProtocol: ALPNSync, PeerCertificates: []*x509.Certificate{clientCert}}); e != nil {
		t.Fatal(e)
	}
	if e = ccfg.VerifyConnection(tls.ConnectionState{NegotiatedProtocol: ALPNSync, PeerCertificates: []*x509.Certificate{serverCert}}); e != nil {
		t.Fatal(e)
	}
	unknown, e := identity.LoadOrCreate(filepath.Join(d, "unknown.pem"))
	if e != nil {
		t.Fatal(e)
	}
	unknownCert, e := x509.ParseCertificate(unknown.Certificate)
	if e != nil {
		t.Fatal(e)
	}
	if e = scfg.VerifyConnection(tls.ConnectionState{NegotiatedProtocol: ALPNSync, PeerCertificates: []*x509.Certificate{unknownCert}}); e == nil {
		t.Fatal("server accepted unknown client")
	}
}
func trust(t *testing.T, s *pairing.Store, id *identity.Identity) {
	t.Helper()
	p, e := s.PutPending(pairing.Peer{ID: id.ID, Name: id.ID, Fingerprint: id.Fingerprint(), ComparisonCode: "test code"})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.Approve(id.ID, p.ApprovalToken, p.Fingerprint, p.ComparisonCode); e != nil {
		t.Fatal(e)
	}
}
