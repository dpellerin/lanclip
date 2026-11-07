package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/dpellerin/lanclip/internal/identity"
	"github.com/dpellerin/lanclip/internal/pairing"
)

// Exercise the actual TLS stack, not just a direct VerifyConnection call. In
// particular, a successful client handshake alone need not mean the server has
// accepted the client's certificate, so require application data in both directions.
func handshake(t *testing.T, server, client *tls.Config) (error, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		tc := tls.Server(c, server)
		if err = tc.HandshakeContext(ctx); err == nil {
			var b [1]byte
			if _, err = tc.Read(b[:]); err == nil {
				_, err = tc.Write(b[:])
			}
		}
		serverErr <- err
	}()
	c, clientErr := (&tls.Dialer{Config: client}).DialContext(ctx, "tcp", ln.Addr().String())
	if clientErr == nil {
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		if _, clientErr = c.Write([]byte{42}); clientErr == nil {
			var b [1]byte
			_, clientErr = c.Read(b[:])
		}
		c.Close()
	}
	return <-serverErr, clientErr
}

func TestTLSHandshakeEnforcesPinsAndKeepsPairingUntrusted(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		proto                    string
		trustClient, trustServer bool
		expected                 string
		wantOK                   bool
	}{
		{"mutually trusted sync", ALPNSync, true, true, "correct", true},
		{"unknown client", ALPNSync, false, true, "correct", false},
		{"unknown server even without discovery hint", ALPNSync, true, false, "", false},
		{"discovery mismatch cannot override pins", ALPNSync, true, true, "wrong", false},
		{"short discovery prefix rejected", ALPNSync, true, true, "short", false},
		{"untrusted pairing allowed for later human comparison", ALPNPair, false, false, "correct", true},
		{"pairing discovery mismatch rejected", ALPNPair, false, false, "wrong", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			serverID, err := identity.LoadOrCreate(filepath.Join(d, "server.pem"))
			if err != nil {
				t.Fatal(err)
			}
			clientID, err := identity.LoadOrCreate(filepath.Join(d, "client.pem"))
			if err != nil {
				t.Fatal(err)
			}
			ss, err := pairing.Load(filepath.Join(d, "server-peers.json"))
			if err != nil {
				t.Fatal(err)
			}
			cs, err := pairing.Load(filepath.Join(d, "client-peers.json"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.trustClient {
				trust(t, ss, clientID)
			}
			if tc.trustServer {
				trust(t, cs, serverID)
			}
			expected := tc.expected
			switch expected {
			case "correct":
				expected = serverID.ShortFingerprint()
			case "wrong":
				expected = clientID.Fingerprint()
			case "short":
				expected = serverID.Fingerprint()[:11]
			}
			scfg, err := ServerConfig(serverID, ss)
			if err != nil {
				t.Fatal(err)
			}
			ccfg, err := ClientConfig(clientID, cs, tc.proto, expected)
			if err != nil {
				t.Fatal(err)
			}
			se, ce := handshake(t, scfg, ccfg)
			if tc.wantOK && (se != nil || ce != nil) {
				t.Fatalf("expected handshake and roundtrip: server=%v client=%v", se, ce)
			}
			if !tc.wantOK && se == nil && ce == nil {
				t.Fatal("untrusted connection completed a TLS roundtrip")
			}
			if tc.proto == ALPNPair && len(ss.List())+len(cs.List()) != 0 {
				t.Fatal("pairing TLS added trust without human approval")
			}
		})
	}
}

func TestTLSVerifierRejectsInvalidSelfSignedIdentities(t *testing.T) {
	d := t.TempDir()
	id, err := identity.LoadOrCreate(filepath.Join(d, "local.pem"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := pairing.Load(filepath.Join(d, "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := ServerConfig(id, store)
	if err != nil {
		t.Fatal(err)
	}
	client, err := ClientConfig(id, store, ALPNPair, "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, tc := range []struct {
		name          string
		before, after time.Time
		tamper        bool
	}{
		{"expired", now.Add(-2 * time.Hour), now.Add(-time.Hour), false},
		{"not yet valid", now.Add(time.Hour), now.Add(2 * time.Hour), false},
		{"invalid self signature", now.Add(-time.Hour), now.Add(time.Hour), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic peer"}, NotBefore: tc.before, NotAfter: tc.after}
			der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, key)
			if err != nil {
				t.Fatal(err)
			}
			if tc.tamper {
				der[len(der)-1] ^= 1
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			state := tls.ConnectionState{NegotiatedProtocol: ALPNPair, PeerCertificates: []*x509.Certificate{cert}}
			if err := server.VerifyConnection(state); err == nil {
				t.Fatal("server accepted invalid certificate")
			}
			if err := client.VerifyConnection(state); err == nil {
				t.Fatal("client accepted invalid certificate")
			}
		})
	}
	cert, err := x509.ParseCertificate(id.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	for _, chain := range [][]*x509.Certificate{nil, {cert, cert}} {
		state := tls.ConnectionState{NegotiatedProtocol: ALPNPair, PeerCertificates: chain}
		if server.VerifyConnection(state) == nil || client.VerifyConnection(state) == nil {
			t.Fatal("accepted missing or multiple certificates")
		}
	}
}
