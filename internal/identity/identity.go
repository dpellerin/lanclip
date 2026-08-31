package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"
)

type Identity struct {
	ID          string
	Certificate []byte
	PrivateKey  ed25519.PrivateKey
	TLSCert     []byte
}

func LoadOrCreate(path string) (*Identity, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return create(path)
	}
	if err != nil {
		return nil, err
	}
	return parse(b)
}

func create(path string) (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id, err := UUID()
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "lanclip:" + id}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(20, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		return nil, err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	b := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	b = append(b, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})...)
	if err := os.WriteFile(path, b, 0600); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	return &Identity{ID: id, Certificate: der, PrivateKey: priv, TLSCert: b}, nil
}

func parse(b []byte) (*Identity, error) {
	certBlock, rest := pem.Decode(b)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, errors.New("identity has no certificate")
	}
	keyBlock, _ := pem.Decode(rest)
	if keyBlock == nil {
		return nil, errors.New("identity has no private key")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := keyAny.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("identity key is not Ed25519")
	}
	id := strings.TrimPrefix(cert.Subject.CommonName, "lanclip:")
	if id == cert.Subject.CommonName || id == "" {
		return nil, errors.New("certificate has invalid identity")
	}
	return &Identity{ID: id, Certificate: certBlock.Bytes, PrivateKey: key, TLSCert: b}, nil
}

func (i *Identity) Fingerprint() string      { return Fingerprint(i.Certificate) }
func (i *Identity) ShortFingerprint() string { return i.Fingerprint()[:16] }
func Fingerprint(der []byte) string          { h := sha256.Sum256(der); return hex.EncodeToString(h[:]) }

func UUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
