package pairing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreLifecycle(t *testing.T) {
	s, e := Load(filepath.Join(t.TempDir(), "peers.json"))
	if e != nil {
		t.Fatal(e)
	}
	p := Peer{ID: "b", Name: "B", Fingerprint: "ff", ComparisonCode: "code"}
	p, e = s.PutPending(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Approve("b", p.ApprovalToken, p.Fingerprint, p.ComparisonCode); e != nil {
		t.Fatal(e)
	}
	if p, ok := s.TrustedFingerprint("ff"); !ok || p.ID != "b" {
		t.Fatal(p, ok)
	}
	stored, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), p.ApprovalToken) || strings.Contains(string(stored), "code") {
		t.Fatal("persisted ephemeral pairing material")
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
	mac, err = s.PutPending(mac)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve("mac", mac.ApprovalToken, mac.Fingerprint, mac.ComparisonCode); err != nil {
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
		if _, err := s.PutPending(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Approve("Laptop", "token", "fingerprint", "code"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err=%v", err)
	}
}

func TestApprovalRejectsChangedSnapshot(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PutPending(Peer{ID: "peer", Name: "Laptop", Fingerprint: "first", ComparisonCode: "first code"})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := s.PutPending(Peer{ID: "peer", Name: "Laptop", Fingerprint: "first", ComparisonCode: "new code"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(p.ID, p.ApprovalToken, p.Fingerprint, p.ComparisonCode); err == nil || !strings.Contains(err.Error(), "changed or expired") {
		t.Fatalf("stale approval err=%v", err)
	}
	if err := s.Approve(refreshed.ID, refreshed.ApprovalToken, refreshed.Fingerprint, refreshed.ComparisonCode); err != nil {
		t.Fatal(err)
	}
}

func TestPendingIdentityCannotBeReplaced(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutPending(Peer{ID: "peer", Name: "Laptop", Fingerprint: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutPending(Peer{ID: "peer", Name: "Impostor", Fingerprint: "second"}); err == nil {
		t.Fatal("replaced an active pending identity")
	}
}

func TestPendingIsEphemeralAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	p, err := s.PutPending(Peer{ID: "peer", Name: "Laptop", Fingerprint: "first", ComparisonCode: "code"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]Peer
	if err := json.Unmarshal(b, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 0 {
		t.Fatalf("pending state persisted: %+v", persisted)
	}
	now = now.Add(defaultPendingTTL)
	if err := s.Approve(p.ID, p.ApprovalToken, p.Fingerprint, p.ComparisonCode); err == nil {
		t.Fatal("approved expired pending request")
	}
}

func TestPendingCap(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.maxPending = 2
	for _, id := range []string{"one", "two"} {
		if _, err := s.PutPending(Peer{ID: id, Name: id, Fingerprint: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PutPending(Peer{ID: "three", Name: "three", Fingerprint: "three"}); err == nil {
		t.Fatal("accepted pending request over cap")
	}
}

func TestLoadDropsLegacyPendingButKeepsTrusted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	legacy := map[string]Peer{
		"pending": {ID: "pending", Name: "Pending", Fingerprint: "aa", State: Pending, ComparisonCode: "old code"},
		"trusted": {ID: "trusted", Name: "Trusted", Fingerprint: "bb", State: Trusted},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("pending"); ok {
		t.Fatal("restored legacy pending approval")
	}
	if p, ok := s.Get("trusted"); !ok || p.State != Trusted {
		t.Fatalf("trusted peer not preserved: %+v ok=%v", p, ok)
	}
}
