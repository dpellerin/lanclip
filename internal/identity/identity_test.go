package identity

import (
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
