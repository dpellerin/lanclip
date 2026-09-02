package sync

import (
	"crypto/sha256"
	"sync"
	"time"
)

type suppressionEntry struct {
	eventID string
	hash    [32]byte
	expires time.Time
}
type Suppressor struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries []suppressionEntry
	now     func() time.Time
}

const maxSuppressionEntries = 256

func NewSuppressor(ttl time.Duration) *Suppressor { return &Suppressor{ttl: ttl, now: time.Now} }
func (s *Suppressor) Add(eventID string, text []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if len(s.entries) >= maxSuppressionEntries {
		copy(s.entries, s.entries[len(s.entries)-maxSuppressionEntries+1:])
		s.entries = s.entries[:maxSuppressionEntries-1]
	}
	s.entries = append(s.entries, suppressionEntry{eventID, sha256.Sum256(text), s.now().Add(s.ttl)})
}
func (s *Suppressor) Consume(text []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	h := sha256.Sum256(text)
	for i, e := range s.entries {
		if e.hash == h {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return true
		}
	}
	return false
}
func (s *Suppressor) prune() {
	now := s.now()
	out := s.entries[:0]
	for _, e := range s.entries {
		if now.Before(e.expires) {
			out = append(out, e)
		}
	}
	s.entries = out
}
