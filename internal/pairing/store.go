package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type State string

const (
	Pending  State = "pending"
	Trusted  State = "trusted"
	Rejected State = "rejected"
)

type Peer struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Fingerprint    string    `json:"fingerprint"`
	State          State     `json:"state"`
	ComparisonCode string    `json:"comparison_code,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	peers map[string]Peer
}

func Load(path string) (*Store, error) {
	s := &Store{path: path, peers: map[string]Peer{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.peers); err != nil {
		return nil, fmt.Errorf("parse peers: %w", err)
	}
	return s, nil
}
func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.peers, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(s.path, b, 0600); err != nil {
		return err
	}
	return os.Chmod(s.path, 0600)
}
func (s *Store) PutPending(p Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.peers[p.ID]; ok && old.State == Trusted {
		if old.Fingerprint != p.Fingerprint {
			return errors.New("trusted peer identity changed")
		}
		return errors.New("peer is already trusted")
	}
	p.State = Pending
	p.UpdatedAt = time.Now()
	s.peers[p.ID] = p
	return s.saveLocked()
}
func (s *Store) Approve(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, p, err := s.resolveLocked(id, Pending)
	if err != nil {
		return err
	}
	p.State = Trusted
	p.ComparisonCode = ""
	p.UpdatedAt = time.Now()
	s.peers[id] = p
	return s.saveLocked()
}
func (s *Store) Reject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, p, err := s.resolveLocked(id)
	if err != nil {
		return err
	}
	p.State = Rejected
	p.ComparisonCode = ""
	p.UpdatedAt = time.Now()
	s.peers[id] = p
	return s.saveLocked()
}
func (s *Store) Unpair(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, _, err := s.resolveLocked(id)
	if err != nil {
		return err
	}
	delete(s.peers, id)
	return s.saveLocked()
}

// Resolve returns the peer matching an exact ID, unambiguous ID prefix, or
// unambiguous case-insensitive device name. It is primarily used by control
// surfaces that present friendly device names while operating on stable IDs.
func (s *Store) Resolve(query string, states ...State) (string, Peer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolveLocked(query, states...)
}

// UpdateName refreshes display metadata after the peer has authenticated. The
// stable ID and pinned fingerprint remain the source of trust.
func (s *Store) UpdateName(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("peer name is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.peers[id]
	if !ok {
		return errors.New("peer is not known")
	}
	if p.Name == name {
		return nil
	}
	p.Name = name
	p.UpdatedAt = time.Now()
	s.peers[id] = p
	return s.saveLocked()
}

// resolveLocked accepts an exact ID, an unambiguous ID prefix, or an
// unambiguous case-insensitive device name. Callers must hold s.mu.
func (s *Store) resolveLocked(query string, states ...State) (string, Peer, error) {
	query = strings.TrimSpace(query)
	allowed := func(p Peer) bool {
		if len(states) == 0 {
			return true
		}
		for _, state := range states {
			if p.State == state {
				return true
			}
		}
		return false
	}
	if p, ok := s.peers[query]; ok && allowed(p) {
		return query, p, nil
	}
	type match struct {
		id   string
		peer Peer
	}
	matches := []match{}
	for id, p := range s.peers {
		if allowed(p) && (strings.EqualFold(p.Name, query) || strings.HasPrefix(id, query)) {
			matches = append(matches, match{id, p})
		}
	}
	if len(matches) == 0 {
		if len(states) == 1 && states[0] == Pending {
			return "", Peer{}, errors.New("no pending peer matches that name or ID")
		}
		return "", Peer{}, errors.New("no peer matches that name or ID")
	}
	if len(matches) > 1 {
		return "", Peer{}, errors.New("peer reference is ambiguous; use a longer ID prefix")
	}
	return matches[0].id, matches[0].peer, nil
}
func (s *Store) Get(id string) (Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.peers[id]
	return p, ok
}
func (s *Store) TrustedFingerprint(fp string) (Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.peers {
		if p.State == Trusted && p.Fingerprint == fp {
			return p, true
		}
	}
	return Peer{}, false
}
func (s *Store) List() []Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var words = []string{"amber", "apple", "atlas", "birch", "blue", "bravo", "cedar", "coral", "delta", "ember", "falcon", "fern", "fjord", "gold", "harbor", "hazel", "indigo", "jade", "kiwi", "lake", "lemon", "lunar", "maple", "mesa", "navy", "olive", "onyx", "pearl", "pine", "quartz", "river", "robin", "sage", "silver", "solar", "stone", "tango", "terra", "tiger", "ultra", "violet", "willow", "winter", "xenon", "yellow", "zebra"}

func ComparisonCode(localID, localFP, localNonce, remoteID, remoteFP, remoteNonce string) string {
	a := []string{localID, localFP, localNonce}
	b := []string{remoteID, remoteFP, remoteNonce}
	if strings.Join(a, "\x00") > strings.Join(b, "\x00") {
		a, b = b, a
	}
	h := sha256.Sum256([]byte(strings.Join(append(a, b...), "\x00")))
	out := make([]string, 6)
	for i := range out {
		out[i] = words[int(h[i])%len(words)]
	}
	return strings.Join(out, " ")
}
func RandomNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
