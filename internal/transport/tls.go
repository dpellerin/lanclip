package transport

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dpellerin/lanclip/internal/identity"
	"github.com/dpellerin/lanclip/internal/pairing"
)

const (
	ALPNSync = "lanclip-sync/1"
	ALPNPair = "lanclip-pair/1"
)

func Certificate(id *identity.Identity) (tls.Certificate, error) {
	return tls.X509KeyPair(id.TLSCert, id.TLSCert)
}

func ServerConfig(id *identity.Identity, store *pairing.Store) (*tls.Config, error) {
	cert, err := Certificate(id)
	if err != nil {
		return nil, err
	}
	// The private PKI is fingerprint-pinned rather than CA-rooted. The standard
	// chain verifier is intentionally replaced, never omitted: every connection
	// is signature-checked below and sync additionally requires an approved pin.
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert, NextProtos: []string{ALPNSync, ALPNPair}, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) != 1 {
			return errors.New("exactly one peer certificate required")
		}
		if err := verifySelfSigned(cs.PeerCertificates[0]); err != nil {
			return err
		}
		if cs.NegotiatedProtocol == ALPNPair {
			return nil
		}
		if cs.NegotiatedProtocol != ALPNSync {
			return errors.New("unsupported application protocol")
		}
		fp := identity.Fingerprint(cs.PeerCertificates[0].Raw)
		if _, ok := store.TrustedFingerprint(fp); !ok {
			return fmt.Errorf("untrusted or changed peer identity %s", fp[:16])
		}
		return nil
	}}, nil
}

func ClientConfig(id *identity.Identity, store *pairing.Store, proto, expectedFingerprint string) (*tls.Config, error) {
	cert, err := Certificate(id)
	if err != nil {
		return nil, err
	}
	// See ServerConfig: custom verification is mandatory for this pinned,
	// self-signed identity model.
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, NextProtos: []string{proto}, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) != 1 {
			return errors.New("exactly one peer certificate required")
		}
		c := cs.PeerCertificates[0]
		if err := verifySelfSigned(c); err != nil {
			return err
		}
		fp := identity.Fingerprint(c.Raw)
		if expectedFingerprint != "" && (len(expectedFingerprint) < 12 || !strings.HasPrefix(fp, expectedFingerprint)) {
			return fmt.Errorf("peer identity does not match discovery: got %s", fp[:16])
		}
		if proto == ALPNSync {
			if _, ok := store.TrustedFingerprint(fp); !ok {
				return errors.New("peer is not trusted")
			}
		}
		return nil
	}}, nil
}

func verifySelfSigned(c *x509.Certificate) error {
	if c.PublicKeyAlgorithm != x509.Ed25519 {
		return errors.New("identity certificate must use Ed25519")
	}
	now := time.Now()
	if now.Before(c.NotBefore) || now.After(c.NotAfter) {
		return errors.New("identity certificate is outside its validity period")
	}
	if err := c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature); err != nil {
		return fmt.Errorf("invalid self-signed certificate: %w", err)
	}
	return nil
}
