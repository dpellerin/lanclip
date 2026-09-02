package identity

import (
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "identity.pem")
	a, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || a.Fingerprint() != b.Fingerprint() {
		t.Fatal("identity changed")
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", st.Mode().Perm())
	}
}

func TestIdentityRejectsMismatchedKey(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.pem")
	bPath := filepath.Join(dir, "b.pem")
	if _, err := LoadOrCreate(aPath); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(bPath); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(aPath)
	b, _ := os.ReadFile(bPath)
	cert, _ := pem.Decode(a)
	_, bRest := pem.Decode(b)
	mixed := append(pem.EncodeToMemory(cert), bRest...)
	if _, err := parse(mixed); err == nil {
		t.Fatal("accepted mismatched identity key")
	}
}

func TestUUIDValidation(t *testing.T) {
	if !ValidUUID("bce45a94-d129-4d06-ad6d-5cc70156f334") {
		t.Fatal("rejected valid v4 UUID")
	}
	for _, value := range []string{"", "not-a-uuid", "bce45a94-d129-5d06-ad6d-5cc70156f334"} {
		if ValidUUID(value) {
			t.Fatalf("accepted %q", value)
		}
	}
}
