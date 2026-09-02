package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	ApprovalToken  string    `json:"approval_token,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Store struct {
	mu         sync.RWMutex
	path       string
	peers      map[string]Peer
	pending    map[string]Peer
	now        func() time.Time
	pendingTTL time.Duration
	maxPending int
}

const (
	defaultPendingTTL = 5 * time.Minute
	defaultMaxPending = 32
)

func Load(path string) (*Store, error) {
	s := &Store{path: path, peers: map[string]Peer{}, pending: map[string]Peer{}, now: time.Now, pendingTTL: defaultPendingTTL, maxPending: defaultMaxPending}
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
	loaded := map[string]Peer{}
	if err := json.Unmarshal(b, &loaded); err != nil {
		return nil, fmt.Errorf("parse peers: %w", err)
	}
	removedPending := false
	for id, p := range loaded {
		// Pending approvals are deliberately session-only. Discard pending entries
		// written by older releases rather than restoring stale approval state.
		if p.State == Pending {
			removedPending = true
			continue
		}
		p.ApprovalToken = ""
		p.ComparisonCode = ""
		s.peers[id] = p
	}
	if removedPending {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.peers, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".peers-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, s.path); err != nil {
		return err
	}
	ok = true
	if d, openErr := os.Open(dir); openErr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
func (s *Store) PutPending(p Peer) (Peer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePendingLocked()
	if old, ok := s.peers[p.ID]; ok && old.State == Trusted {
		if old.Fingerprint != p.Fingerprint {
			return Peer{}, errors.New("trusted peer identity changed")
		}
		return Peer{}, errors.New("peer is already trusted")
	}
	if old, ok := s.pending[p.ID]; ok && old.Fingerprint != p.Fingerprint {
		return Peer{}, errors.New("a pairing request for this device ID is already pending")
	}
	if _, exists := s.pending[p.ID]; !exists && len(s.pending) >= s.maxPending {
		return Peer{}, errors.New("too many pairing requests are pending")
	}
	token, err := RandomNonce()
	if err != nil {
		return Peer{}, err
	}
	p.State = Pending
	p.UpdatedAt = s.now()
	p.ApprovalToken = token
	s.pending[p.ID] = p
	return p, nil
}
func (s *Store) Approve(id, token, fingerprint, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePendingLocked()
	id, p, err := s.resolveLocked(id, Pending)
	if err != nil {
		return err
	}
	if token == "" || token != p.ApprovalToken || fingerprint != p.Fingerprint || code != p.ComparisonCode {
		return errors.New("pairing request changed or expired; compare it again before approving")
	}
	pending := p
	delete(s.pending, id)
	old, hadOld := s.peers[id]
	p.State = Trusted
	p.ComparisonCode = ""
	p.ApprovalToken = ""
	p.UpdatedAt = s.now()
	s.peers[id] = p
	if err := s.saveLocked(); err != nil {
		if hadOld {
			s.peers[id] = old
		} else {
			delete(s.peers, id)
		}
		s.pending[id] = pending
		return err
	}
	return nil
}
func (s *Store) Reject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePendingLocked()
	id, p, err := s.resolveLocked(id)
	if err != nil {
		return err
	}
	oldPeer, hadPeer := s.peers[id]
	oldPending, hadPending := s.pending[id]
	p.State = Rejected
	p.ComparisonCode = ""
	p.ApprovalToken = ""
	p.UpdatedAt = s.now()
	delete(s.pending, id)
	s.peers[id] = p
	if err := s.saveLocked(); err != nil {
		if hadPeer {
			s.peers[id] = oldPeer
		} else {
			delete(s.peers, id)
		}
		if hadPending {
			s.pending[id] = oldPending
		}
		return err
	}
	return nil
}
func (s *Store) Unpair(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, _, err := s.resolveLocked(id)
	if err != nil {
		return err
	}
	oldPeer, hadPeer := s.peers[id]
	oldPending, hadPending := s.pending[id]
	delete(s.peers, id)
	delete(s.pending, id)
	if err := s.saveLocked(); err != nil {
		if hadPeer {
			s.peers[id] = oldPeer
		}
		if hadPending {
			s.pending[id] = oldPending
		}
		return err
	}
	return nil
}

// Resolve returns the peer matching an exact ID, unambiguous ID prefix, or
// unambiguous case-insensitive device name. It is primarily used by control
// surfaces that present friendly device names while operating on stable IDs.
func (s *Store) Resolve(query string, states ...State) (string, Peer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePendingLocked()
	return s.resolveLocked(query, states...)
}

// UpdateName refreshes display metadata after the peer has authenticated. The
// stable ID and pinned fingerprint remain the source of trust.
func (s *Store) UpdateName(id, name string) error {
	name = NormalizeDeviceName(name)
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
	old := p
	p.Name = name
	p.UpdatedAt = s.now()
	s.peers[id] = p
	if err := s.saveLocked(); err != nil {
		s.peers[id] = old
		return err
	}
	return nil
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
	if p, ok := s.pending[query]; ok && allowed(p) {
		return query, p, nil
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
	for id, p := range s.pending {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePendingLocked()
	p, ok := s.pending[id]
	if !ok {
		p, ok = s.peers[id]
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunePendingLocked()
	out := make([]Peer, 0, len(s.peers)+len(s.pending))
	for id, p := range s.peers {
		if _, pending := s.pending[id]; pending {
			continue
		}
		out = append(out, p)
	}
	for _, p := range s.pending {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) prunePendingLocked() {
	now := s.now()
	for id, p := range s.pending {
		if !now.Before(p.UpdatedAt.Add(s.pendingTTL)) {
			delete(s.pending, id)
		}
	}
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
