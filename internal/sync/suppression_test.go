package sync

import (
	"testing"
	"time"
)

func TestSuppressorConsumesExactlyOne(t *testing.T) {
	s := NewSuppressor(time.Second)
	s.Add("a", []byte("same"))
	if !s.Consume([]byte("same")) {
		t.Fatal("no match")
	}
	if s.Consume([]byte("same")) {
		t.Fatal("matched twice")
	}
}
func TestSuppressorMismatchAndExpiry(t *testing.T) {
	s := NewSuppressor(time.Second)
	now := time.Now()
	s.now = func() time.Time { return now }
	s.Add("a", []byte("x"))
	if s.Consume([]byte("y")) {
		t.Fatal("mismatch")
	}
	now = now.Add(2 * time.Second)
	if s.Consume([]byte("x")) {
		t.Fatal("expired matched")
	}
}
