package discovery

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dpellerin/lanclip/internal/identity"
	"github.com/dpellerin/lanclip/internal/pairing"
	"github.com/libp2p/zeroconf/v2"
)

const (
	peerTTL  = 45 * time.Second
	maxPeers = 256
)

type Peer struct {
	ID, Name, Fingerprint, OS string
	Port                      int
	Addresses                 []string
	LastSeen                  time.Time
}
type Service struct {
	mu                        sync.RWMutex
	selfID, name, fingerprint string
	service, osName           string
	port                      int
	server                    *zeroconf.Server
	peers                     map[string]Peer
	lastBrowse                time.Time
	lastError                 string
	closed                    bool
}

func New(selfID, name, fingerprint, osName, service string, port int) (*Service, error) {
	ifaces, err := activeLANInterfaces()
	if err != nil {
		return nil, err
	}
	txt := []string{"v=1", "id=" + selfID, "pk=" + fingerprint, "os=" + osName}
	server, err := zeroconf.Register(name, service, "local.", port, txt, ifaces)
	if err != nil {
		return nil, err
	}
	return &Service{selfID: selfID, name: name, fingerprint: fingerprint, service: service, osName: osName, port: port, server: server, peers: map[string]Peer{}}, nil
}
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.server != nil {
		s.server.Shutdown()
		s.server = nil
	}
	return nil
}

// Run intentionally recreates both browser and advertisement at a jittered
// interval. This refreshes interface bindings and advertised addresses after
// sleep, Wi-Fi cycling, or DHCP changes.
func (s *Service) Run(ctx context.Context) {
	for ctx.Err() == nil {
		ifaces, err := activeLANInterfaces()
		if err != nil {
			s.setError(err)
			if !wait(ctx, 5*time.Second) {
				return
			}
			continue
		}
		entries := make(chan *zeroconf.ServiceEntry, 32)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for e := range entries {
				s.observe(e)
			}
		}()
		cycle, cancel := context.WithTimeout(ctx, 15*time.Second+time.Duration(rand.Intn(5000))*time.Millisecond)
		s.mu.Lock()
		s.lastBrowse = time.Now()
		s.mu.Unlock()
		err = zeroconf.Browse(cycle, s.service, "local.", entries, zeroconf.SelectIfaces(ifaces))
		cancel()
		<-done
		if err != nil && ctx.Err() == nil && !errors.Is(err, context.DeadlineExceeded) {
			s.setError(err)
		} else {
			s.setError(nil)
		}
		if ctx.Err() == nil {
			s.refreshRegistration()
		}
	}
}
func (s *Service) refreshRegistration() {
	ifaces, err := activeLANInterfaces()
	if err != nil {
		s.setError(err)
		return
	}
	txt := []string{"v=1", "id=" + s.selfID, "pk=" + s.fingerprint, "os=" + s.osName}
	next, err := zeroconf.Register(s.name, s.service, "local.", s.port, txt, ifaces)
	if err != nil {
		s.setError(err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		next.Shutdown()
		return
	}
	old := s.server
	s.server = next
	if old != nil {
		old.Shutdown()
	}
}
func (s *Service) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.lastError = ""
	} else {
		s.lastError = err.Error()
	}
}
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func activeLANInterfaces() ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]net.Interface, 0, len(all))
	for _, iface := range all {
		if eligibleInterface(iface) {
			out = append(out, iface)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no active multicast LAN interface")
	}
	return out, nil
}
func eligibleInterface(iface net.Interface) bool {
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
		return false
	}
	n := strings.ToLower(iface.Name)
	for _, prefix := range []string{"docker", "br-", "bridge", "veth", "virbr", "vmnet", "vbox", "podman", "cni", "lxc", "incus", "tailscale", "tun", "utun", "tap", "wg", "zt", "nebula", "awdl", "llw"} {
		if strings.HasPrefix(n, prefix) {
			return false
		}
	}
	return true
}

// IsDirectLANIP reports whether ip belongs to a subnet on an eligible active
// multicast LAN interface. Loopback, known VPN/tunnel/virtual interfaces, and
// routed-only addresses are excluded so Lanclip cannot extend beyond the link.
func IsDirectLANIP(ip net.IP) bool {
	return isDirectIP(ip, activeLANNetworks())
}

func isDirectIP(ip net.IP, networks []*net.IPNet) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return ipInNetworks(ip, networks)
}

func activeLANNetworks() []*net.IPNet {
	var networks []*net.IPNet
	ifaces, err := net.Interfaces()
	if err != nil {
		return networks
	}
	for _, iface := range ifaces {
		if !eligibleInterface(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if network, ok := addr.(*net.IPNet); ok {
				networks = append(networks, network)
			}
		}
	}
	return networks
}

func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Service) observe(e *zeroconf.ServiceEntry) {
	fields := map[string]string{}
	for _, v := range e.Text {
		p := strings.SplitN(v, "=", 2)
		if len(p) == 2 {
			fields[p[0]] = p[1]
		}
	}
	id := fields["id"]
	name := pairing.NormalizeDeviceName(e.Instance)
	if !identity.ValidUUID(id) || id == s.selfID || fields["v"] != "1" || !identity.ValidFingerprint(fields["pk"], 8) || name == "" {
		return
	}
	var addrs []string
	for _, ip := range e.AddrIPv4 {
		addrs = append(addrs, net.JoinHostPort(ip.String(), strconv.Itoa(e.Port)))
	}
	for _, ip := range e.AddrIPv6 {
		addrs = append(addrs, net.JoinHostPort(ip.String(), strconv.Itoa(e.Port)))
	}
	sort.Strings(addrs)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putPeerLocked(Peer{ID: id, Name: name, Fingerprint: fields["pk"], OS: fields["os"], Port: e.Port, Addresses: addrs, LastSeen: time.Now()})
}

func (s *Service) putPeerLocked(peer Peer) {
	s.pruneLocked(peer.LastSeen)
	if _, exists := s.peers[peer.ID]; !exists && len(s.peers) >= maxPeers {
		var oldestID string
		var oldest time.Time
		for peerID, existing := range s.peers {
			if oldestID == "" || existing.LastSeen.Before(oldest) {
				oldestID, oldest = peerID, existing.LastSeen
			}
		}
		delete(s.peers, oldestID)
	}
	s.peers[peer.ID] = peer
}
func (s *Service) Peers() []Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (s *Service) Find(query string) (Peer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	q := strings.ToLower(query)
	for _, p := range s.peers {
		if p.ID == query || strings.ToLower(p.Name) == q || strings.HasPrefix(p.ID, query) {
			return p, true
		}
	}
	return Peer{}, false
}

func (s *Service) pruneLocked(now time.Time) {
	cutoff := now.Add(-peerTTL)
	for id, p := range s.peers {
		if !p.LastSeen.After(cutoff) {
			delete(s.peers, id)
		}
	}
}
func (s *Service) Health() (time.Time, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBrowse, s.lastError
}
func (p Peer) Endpoint() (string, error) {
	if len(p.Addresses) == 0 {
		return "", fmt.Errorf("peer %s has no resolved address", p.Name)
	}
	if endpoint, ok := directEndpoint(p.Addresses, activeLANNetworks()); ok {
		return endpoint, nil
	}
	return "", fmt.Errorf("peer %s has no address on a directly connected LAN", p.Name)
}

func directEndpoint(addresses []string, networks []*net.IPNet) (string, bool) {
	for _, endpoint := range addresses {
		host, _, err := net.SplitHostPort(endpoint)
		if err != nil {
			continue
		}
		ip := net.ParseIP(host)
		if ip == nil {
			continue
		}
		if isDirectIP(ip, networks) {
			return endpoint, true
		}
	}
	return "", false
}
func (s *Service) ObserveForTest(p Peer) {
	if p.ID == s.selfID {
		return
	}
	if p.LastSeen.IsZero() {
		p.LastSeen = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putPeerLocked(p)
}
