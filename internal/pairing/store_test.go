package pairing

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreLifecycle(t *testing.T) {
	s, e := Load(filepath.Join(t.TempDir(), "peers.json"))
	if e != nil {
		t.Fatal(e)
	}
	p := Peer{ID: "b", Name: "B", Fingerprint: "ff", ComparisonCode: "code"}
	if e = s.PutPending(p); e != nil {
		t.Fatal(e)
	}
	if e = s.Approve("b"); e != nil {
		t.Fatal(e)
	}
	if p, ok := s.TrustedFingerprint("ff"); !ok || p.ID != "b" {
		t.Fatal(p, ok)
	}
	if e = s.Unpair("b"); e != nil {
		t.Fatal(e)
	}
}
func TestComparisonSymmetric(t *testing.T) {
	a := ComparisonCode("a", "1", "x", "b", "2", "y")
	b := ComparisonCode("b", "2", "y", "a", "1", "x")
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
}

func TestStoreAcceptsFriendlyPeerReferences(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	mac := Peer{ID: "bce45a94-d129-4d06-ad6d-5cc70156f334", Name: "Mac", Fingerprint: "aa"}
	if err := s.PutPending(mac); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve("mac"); err != nil {
		t.Fatal(err)
	}
	if p, ok := s.Get(mac.ID); !ok || p.State != Trusted {
		t.Fatalf("peer=%+v ok=%v", p, ok)
	}
	if id, p, err := s.Resolve("MAC", Trusted); err != nil || id != mac.ID || p.Name != "Mac" {
		t.Fatalf("resolve id=%q peer=%+v err=%v", id, p, err)
	}
	if err := s.UpdateName(mac.ID, "Studio Mac"); err != nil {
		t.Fatal(err)
	}
	if p, ok := s.Get(mac.ID); !ok || p.Name != "Studio Mac" || p.State != Trusted || p.Fingerprint != mac.Fingerprint {
		t.Fatalf("updated peer=%+v ok=%v", p, ok)
	}
	if err := s.Unpair("bce45a94"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsAmbiguousFriendlyReference(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Peer{
		{ID: "aaaa-1111", Name: "Laptop", Fingerprint: "aa"},
		{ID: "aaaa-2222", Name: "Laptop", Fingerprint: "bb"},
	} {
		if err := s.PutPending(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Approve("Laptop"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err=%v", err)
	}
}
