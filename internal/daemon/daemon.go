package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dpellerin/lanclip/internal/clipboard"
	"github.com/dpellerin/lanclip/internal/config"
	"github.com/dpellerin/lanclip/internal/control"
	"github.com/dpellerin/lanclip/internal/discovery"
	"github.com/dpellerin/lanclip/internal/identity"
	"github.com/dpellerin/lanclip/internal/pairing"
	"github.com/dpellerin/lanclip/internal/protocol"
	syncer "github.com/dpellerin/lanclip/internal/sync"
	"github.com/dpellerin/lanclip/internal/transport"
)

type Stats struct {
	Connected     bool      `json:"connected"`
	Address       string    `json:"address,omitempty"`
	LastSent      time.Time `json:"last_sent,omitempty"`
	SentBytes     int       `json:"sent_bytes,omitempty"`
	LastReceived  time.Time `json:"last_received,omitempty"`
	ReceivedBytes int       `json:"received_bytes,omitempty"`
	Reconnects    int       `json:"reconnects"`
	LastError     string    `json:"last_error,omitempty"`
}
type Status struct {
	Version     string           `json:"version"`
	Uptime      string           `json:"uptime"`
	Name        string           `json:"name"`
	DeviceID    string           `json:"device_id"`
	Fingerprint string           `json:"fingerprint"`
	Clipboard   clipboard.Health `json:"clipboard"`
	Discovery   map[string]any   `json:"discovery"`
	Peers       []PeerStatus     `json:"peers"`
}
type PeerStatus struct {
	pairing.Peer
	Addresses []string `json:"addresses,omitempty"`
	Stats     Stats    `json:"connection"`
}
type DoctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type peerConn struct {
	id       string
	conn     *tls.Conn
	write    sync.Mutex
	outbound bool
}
type Daemon struct {
	cfg        config.Config
	paths      config.Paths
	id         *identity.Identity
	store      *pairing.Store
	clip       clipboard.Adapter
	disc       *discovery.Service
	started    time.Time
	version    string
	ctx        context.Context
	cancel     context.CancelFunc
	listener   net.Listener
	ctl        *control.Server
	suppress   *syncer.Suppressor
	mu         sync.RWMutex
	conns      map[string]*peerConn
	stats      map[string]Stats
	connecting map[string]bool
}

func New(cfg config.Config, paths config.Paths, id *identity.Identity, store *pairing.Store, version string) (*Daemon, error) {
	d := &Daemon{cfg: cfg, paths: paths, id: id, store: store, version: version, started: time.Now(), clip: clipboard.New(cfg.MaxClipboardBytes), suppress: syncer.NewSuppressor(5 * time.Second), conns: map[string]*peerConn{}, stats: map[string]Stats{}, connecting: map[string]bool{}}
	return d, nil
}

func (d *Daemon) Run(parent context.Context) error {
	d.ctx, d.cancel = context.WithCancel(parent)
	tlsCfg, err := transport.ServerConfig(d.id, d.store)
	if err != nil {
		return err
	}
	base, err := net.Listen("tcp", fmt.Sprintf(":%d", d.cfg.ListenPort))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	d.listener = tls.NewListener(base, tlsCfg)
	osName := runtime.GOOS
	d.disc, err = discovery.New(d.id.ID, d.cfg.Name, d.id.ShortFingerprint(), osName, d.cfg.ServiceType, d.cfg.ListenPort)
	if err != nil {
		d.listener.Close()
		return fmt.Errorf("discovery: %w", err)
	}
	events, err := d.clip.Start(d.ctx)
	if err != nil {
		d.disc.Close()
		d.listener.Close()
		return fmt.Errorf("clipboard: %w", err)
	}
	d.ctl, err = control.Listen(d.paths.Socket, d.handleControl)
	if err != nil {
		d.clip.Close()
		d.disc.Close()
		d.listener.Close()
		return fmt.Errorf("control: %w", err)
	}
	slog.Info("daemon started", "device", d.cfg.Name, "id", short(d.id.ID), "port", d.cfg.ListenPort)
	go d.disc.Run(d.ctx)
	go d.acceptLoop()
	go d.localLoop(events)
	go d.connectLoop()
	<-d.ctx.Done()
	d.close()
	return nil
}

func (d *Daemon) close() {
	if d.ctl != nil {
		_ = d.ctl.Close()
	}
	if d.listener != nil {
		_ = d.listener.Close()
	}
	if d.disc != nil {
		_ = d.disc.Close()
	}
	if d.clip != nil {
		_ = d.clip.Close()
	}
	d.mu.Lock()
	for _, c := range d.conns {
		_ = c.conn.Close()
	}
	d.conns = map[string]*peerConn{}
	d.mu.Unlock()
}

func (d *Daemon) acceptLoop() {
	for {
		c, err := d.listener.Accept()
		if err != nil {
			if d.ctx.Err() == nil {
				slog.Warn("accept failed", "error", err)
			}
			return
		}
		remote, ok := c.RemoteAddr().(*net.TCPAddr)
		if !ok || !discovery.IsDirectLANIP(remote.IP) {
			slog.Warn("rejected connection outside directly connected LAN")
			_ = c.Close()
			continue
		}
		go d.handleTLS(c.(*tls.Conn), false)
	}
}
func (d *Daemon) handleTLS(c *tls.Conn, outbound bool) {
	if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		c.Close()
		return
	}
	if err := c.HandshakeContext(d.ctx); err != nil {
		slog.Warn("TLS handshake rejected", "error", safeError(err))
		c.Close()
		return
	}
	_ = c.SetDeadline(time.Time{})
	switch c.ConnectionState().NegotiatedProtocol {
	case transport.ALPNPair:
		d.handleIncomingPair(c)
	case transport.ALPNSync:
		d.handleSync(c, outbound)
	default:
		c.Close()
	}
}

func (d *Daemon) handleIncomingPair(c *tls.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	m, err := protocol.Read(c, 16<<10)
	if err != nil || m.Type != "pair_offer" {
		return
	}
	peerCert := c.ConnectionState().PeerCertificates[0]
	actual := identity.Fingerprint(peerCert.Raw)
	if m.DeviceID == "" || m.Nonce == "" || m.Fingerprint != actual || !ValidateCertificateIdentity(peerCert, m.DeviceID) {
		return
	}
	nonce, err := pairing.RandomNonce()
	if err != nil {
		return
	}
	code := pairing.ComparisonCode(d.id.ID, d.id.Fingerprint(), nonce, m.DeviceID, actual, m.Nonce)
	if err = d.store.PutPending(pairing.Peer{ID: m.DeviceID, Name: m.Name, Fingerprint: actual, ComparisonCode: code}); err != nil {
		return
	}
	_ = protocol.Write(c, protocol.Message{Type: "pair_answer", DeviceID: d.id.ID, Name: d.cfg.Name, Nonce: nonce, Fingerprint: d.id.Fingerprint()}, 16<<10)
	slog.Info("pairing request pending approval", "peer", m.Name, "id", short(m.DeviceID))
}

func (d *Daemon) pair(ctx context.Context, query string) (pairing.Peer, error) {
	p, ok := d.disc.Find(query)
	if !ok {
		return pairing.Peer{}, fmt.Errorf("peer %q is not currently discovered", query)
	}
	endpoint, err := p.Endpoint()
	if err != nil {
		return pairing.Peer{}, err
	}
	cfg, err := transport.ClientConfig(d.id, d.store, transport.ALPNPair, p.Fingerprint)
	if err != nil {
		return pairing.Peer{}, err
	}
	dialer := tls.Dialer{Config: cfg, NetDialer: &net.Dialer{Timeout: 5 * time.Second}}
	raw, err := dialer.DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return pairing.Peer{}, err
	}
	c := raw.(*tls.Conn)
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	nonce, err := pairing.RandomNonce()
	if err != nil {
		return pairing.Peer{}, err
	}
	if err = protocol.Write(c, protocol.Message{Type: "pair_offer", DeviceID: d.id.ID, Name: d.cfg.Name, Nonce: nonce, Fingerprint: d.id.Fingerprint()}, 16<<10); err != nil {
		return pairing.Peer{}, err
	}
	m, err := protocol.Read(c, 16<<10)
	if err != nil {
		return pairing.Peer{}, err
	}
	if m.Type != "pair_answer" || m.DeviceID != p.ID || !strings.HasPrefix(m.Fingerprint, p.Fingerprint) || m.Nonce == "" || !ValidateCertificateIdentity(c.ConnectionState().PeerCertificates[0], m.DeviceID) {
		return pairing.Peer{}, errors.New("invalid pairing response")
	}
	code := pairing.ComparisonCode(d.id.ID, d.id.Fingerprint(), nonce, m.DeviceID, m.Fingerprint, m.Nonce)
	peer := pairing.Peer{ID: m.DeviceID, Name: m.Name, Fingerprint: m.Fingerprint, ComparisonCode: code}
	if err = d.store.PutPending(peer); err != nil {
		return pairing.Peer{}, err
	}
	return peer, nil
}

func (d *Daemon) connectLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	backoffs := map[string]*transport.Backoff{}
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-tick.C:
			seen := map[string]bool{}
			for _, dp := range d.disc.Peers() {
				seen[dp.ID] = true
				tp, ok := d.store.Get(dp.ID)
				if !ok || tp.State != pairing.Trusted || d.id.ID >= dp.ID || d.connected(dp.ID) || !d.beginConnect(dp.ID) {
					continue
				}
				endpoint, err := dp.Endpoint()
				if err != nil {
					d.endConnect(dp.ID)
					continue
				}
				b := backoffs[dp.ID]
				if b == nil {
					b = &transport.Backoff{}
					backoffs[dp.ID] = b
				}
				go func(dp discovery.Peer, tp pairing.Peer, endpoint string, b *transport.Backoff) {
					defer d.endConnect(dp.ID)
					if err := d.dialSync(dp, tp, endpoint); err != nil {
						d.recordError(dp.ID, err)
						_ = transport.Sleep(d.ctx, b.Next())
					} else {
						b.Reset()
					}
				}(dp, tp, endpoint, b)
			}
			// A manual hostname is only a location hint. The normal pinned
			// certificate and hello identity checks still determine which trusted
			// peer, if any, is allowed to connect there.
			for _, tp := range d.store.List() {
				if tp.State != pairing.Trusted || d.id.ID >= tp.ID || seen[tp.ID] || d.connected(tp.ID) || !d.beginConnect(tp.ID) {
					continue
				}
				endpoints := d.manualEndpoints()
				if len(endpoints) == 0 {
					d.endConnect(tp.ID)
					continue
				}
				b := backoffs[tp.ID]
				if b == nil {
					b = &transport.Backoff{}
					backoffs[tp.ID] = b
				}
				dp := discovery.Peer{ID: tp.ID, Name: tp.Name, Fingerprint: tp.Fingerprint}
				go func() {
					defer d.endConnect(tp.ID)
					var lastErr error
					for _, endpoint := range endpoints {
						if lastErr = d.dialSync(dp, tp, endpoint); lastErr == nil {
							b.Reset()
							return
						}
					}
					d.recordError(tp.ID, lastErr)
					_ = transport.Sleep(d.ctx, b.Next())
				}()
			}
		}
	}
}
func (d *Daemon) beginConnect(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.connecting[id] {
		return false
	}
	d.connecting[id] = true
	return true
}
func (d *Daemon) endConnect(id string) { d.mu.Lock(); delete(d.connecting, id); d.mu.Unlock() }
func (d *Daemon) manualEndpoints() []string {
	var out []string
	for _, host := range d.cfg.ManualPeers {
		port := d.cfg.ListenPort
		if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
			if _, err := fmt.Sscan(parsedPort, &port); err != nil || port < 1 || port > 65535 {
				continue
			}
		}
		resolved, err := ResolveManualPeer(host, port)
		if err == nil {
			out = append(out, resolved...)
		}
	}
	return out
}
func (d *Daemon) dialSync(dp discovery.Peer, tp pairing.Peer, endpoint string) error {
	cfg, err := transport.ClientConfig(d.id, d.store, transport.ALPNSync, tp.Fingerprint)
	if err != nil {
		return err
	}
	raw, err := (&tls.Dialer{Config: cfg, NetDialer: &net.Dialer{Timeout: 5 * time.Second}}).DialContext(d.ctx, "tcp", endpoint)
	if err != nil {
		return err
	}
	d.handleTLS(raw.(*tls.Conn), true)
	return nil
}

func (d *Daemon) handleSync(c *tls.Conn, outbound bool) {
	defer c.Close()
	fp := identity.Fingerprint(c.ConnectionState().PeerCertificates[0].Raw)
	trusted, ok := d.store.TrustedFingerprint(fp)
	if !ok {
		return
	}
	if outbound {
		if err := protocol.Write(c, protocol.Message{Type: "hello", DeviceID: d.id.ID, Name: d.cfg.Name}, 16<<10); err != nil {
			return
		}
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	m, err := protocol.Read(c, 16<<10)
	if err != nil {
		return
	}
	if m.Type != "hello" || m.DeviceID != trusted.ID {
		return
	}
	if m.Name != "" && m.Name != trusted.Name {
		if err := d.store.UpdateName(trusted.ID, m.Name); err == nil {
			trusted.Name = m.Name
		}
	}
	if !outbound {
		if err = protocol.Write(c, protocol.Message{Type: "hello", DeviceID: d.id.ID, Name: d.cfg.Name}, 16<<10); err != nil {
			return
		}
	}
	// Stable UUID connection ownership: smaller ID must be the dialer.
	if outbound != (d.id.ID < trusted.ID) {
		return
	}
	pc := &peerConn{id: trusted.ID, conn: c, outbound: outbound}
	if !d.register(pc) {
		return
	}
	defer d.unregister(pc)
	slog.Info("peer connected", "peer", trusted.Name, "id", short(trusted.ID), "address", c.RemoteAddr().String())
	d.readLoop(pc)
}
func (d *Daemon) register(pc *peerConn) bool {
	d.mu.Lock()
	old := d.conns[pc.id]
	if old == pc {
		d.mu.Unlock()
		return false
	}
	// Connection ownership has already been validated, so a second connection
	// for the same peer is a fresher replacement for a possibly half-open socket.
	// Install it before closing the old one; the old read loop's unregister then
	// observes that it no longer owns the map entry.
	d.conns[pc.id] = pc
	s := d.stats[pc.id]
	s.Connected = true
	s.Address = pc.conn.RemoteAddr().String()
	s.Reconnects++
	s.LastError = ""
	d.stats[pc.id] = s
	d.mu.Unlock()
	if old != nil && old.conn != nil {
		_ = old.conn.Close()
	}
	return true
}
func (d *Daemon) unregister(pc *peerConn) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conns[pc.id] == pc {
		delete(d.conns, pc.id)
		s := d.stats[pc.id]
		s.Connected = false
		d.stats[pc.id] = s
	}
}
func (d *Daemon) connected(id string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.conns[id] != nil
}
func (d *Daemon) readLoop(pc *peerConn) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	errs := make(chan error, 1)
	go func() {
		for {
			_ = pc.conn.SetReadDeadline(time.Now().Add(45 * time.Second))
			m, err := protocol.Read(pc.conn, protocol.DefaultMaxFrame)
			if err != nil {
				errs <- err
				return
			}
			switch m.Type {
			case "clipboard":
				if m.Text == "" || m.MIME != "text/plain;charset=utf-8" || len(m.Text) > d.cfg.MaxClipboardBytes {
					continue
				}
				d.suppress.Add(m.EventID, []byte(m.Text))
				ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
				err = d.clip.Write(ctx, m.Text)
				cancel()
				if err != nil {
					d.suppress.Consume([]byte(m.Text))
					d.recordError(pc.id, err)
					continue
				}
				d.mu.Lock()
				s := d.stats[pc.id]
				s.LastReceived = time.Now()
				s.ReceivedBytes = len(m.Text)
				d.stats[pc.id] = s
				d.mu.Unlock()
			case "ping":
				_ = d.send(pc, protocol.Message{Type: "pong", Nonce: m.Nonce})
			case "pong":
			default:
			}
		}
	}()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-tick.C:
			nonce, _ := pairing.RandomNonce()
			if err := d.send(pc, protocol.Message{Type: "ping", Nonce: nonce}); err != nil {
				return
			}
		case err := <-errs:
			if !errors.Is(err, io.EOF) {
				d.recordError(pc.id, err)
			}
			return
		}
	}
}

func (d *Daemon) localLoop(events <-chan clipboard.Event) {
	for {
		select {
		case <-d.ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			if e.Text == "" || len(e.Text) > d.cfg.MaxClipboardBytes {
				continue
			}
			if d.suppress.Consume([]byte(e.Text)) {
				continue
			}
			id, err := identity.UUID()
			if err != nil {
				continue
			}
			m := protocol.Message{Type: "clipboard", EventID: id, MIME: "text/plain;charset=utf-8", Text: e.Text}
			d.mu.RLock()
			conns := make([]*peerConn, 0, len(d.conns))
			for _, c := range d.conns {
				conns = append(conns, c)
			}
			d.mu.RUnlock()
			for _, c := range conns {
				if err := d.send(c, m); err != nil {
					d.recordError(c.id, err)
				} else {
					d.mu.Lock()
					s := d.stats[c.id]
					s.LastSent = time.Now()
					s.SentBytes = len(e.Text)
					d.stats[c.id] = s
					d.mu.Unlock()
				}
			}
		}
	}
}
func (d *Daemon) send(pc *peerConn, m protocol.Message) error {
	pc.write.Lock()
	defer pc.write.Unlock()
	_ = pc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return protocol.Write(pc.conn, m, protocol.DefaultMaxFrame)
}
func (d *Daemon) recordError(id string, err error) {
	d.mu.Lock()
	s := d.stats[id]
	s.LastError = safeError(err)
	d.stats[id] = s
	d.mu.Unlock()
}

func (d *Daemon) handleControl(ctx context.Context, r control.Request) control.Response {
	switch r.Command {
	case "status":
		return control.Response{OK: true, Data: d.status()}
	case "peers":
		return control.Response{OK: true, Data: d.peerStatuses()}
	case "pair":
		p, e := d.pair(ctx, r.Argument)
		if e != nil {
			return fail(e)
		}
		return control.Response{OK: true, Data: p}
	case "approve":
		return result(d.store.Approve(r.Argument))
	case "reject":
		return result(d.store.Reject(r.Argument))
	case "unpair":
		id, _, e := d.store.Resolve(r.Argument, pairing.Trusted)
		if e != nil {
			return fail(e)
		}
		if c := d.getConn(id); c != nil {
			_ = c.conn.Close()
		}
		return result(d.store.Unpair(id))
	case "doctor":
		return control.Response{OK: true, Data: d.doctor()}
	default:
		return fail(fmt.Errorf("unknown command %q", r.Command))
	}
}
func (d *Daemon) getConn(id string) *peerConn { d.mu.RLock(); defer d.mu.RUnlock(); return d.conns[id] }
func (d *Daemon) status() Status {
	last, discErr := d.disc.Health()
	return Status{Version: d.version, Uptime: time.Since(d.started).Round(time.Second).String(), Name: d.cfg.Name, DeviceID: d.id.ID, Fingerprint: d.id.Fingerprint(), Clipboard: d.clip.Health(), Discovery: map[string]any{"last_browse": last, "last_error": discErr, "peers_seen": len(d.disc.Peers())}, Peers: d.peerStatuses()}
}
func (d *Daemon) peerStatuses() []PeerStatus {
	found := map[string]discovery.Peer{}
	for _, p := range d.disc.Peers() {
		found[p.ID] = p
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := []PeerStatus{}
	stored := map[string]bool{}
	for _, p := range d.store.List() {
		stored[p.ID] = true
		stats := d.stats[p.ID]
		if seen, ok := found[p.ID]; ok && seen.Fingerprint != "" && !strings.HasPrefix(p.Fingerprint, seen.Fingerprint) {
			stats.LastError = "discovered peer identity changed"
		}
		out = append(out, PeerStatus{Peer: p, Addresses: found[p.ID].Addresses, Stats: stats})
	}
	for id, seen := range found {
		if stored[id] {
			continue
		}
		out = append(out, PeerStatus{Peer: pairing.Peer{ID: id, Name: seen.Name, Fingerprint: seen.Fingerprint, State: pairing.State("unpaired")}, Addresses: seen.Addresses, Stats: d.stats[id]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (d *Daemon) doctor() []DoctorCheck {
	configOK := config.CheckPrivate(d.paths.Config) == nil
	identityOK := config.CheckPrivate(d.paths.Identity) == nil
	peersOK := config.CheckPrivate(d.paths.Peers) == nil
	clip := d.clip.Health()
	clipDetail := "watching for text changes"
	if !clip.Running {
		clipDetail = clip.LastError
		if clipDetail == "" {
			clipDetail = "watcher is not running"
		}
	}
	socketOK := fileMode(d.paths.Socket)&0077 == 0
	listenerOK := d.listener != nil
	checks := []DoctorCheck{
		{"config permissions", configOK, ternary(configOK, "private (0600)", "config file must be mode 0600")},
		{"identity permissions", identityOK, ternary(identityOK, "private (0600)", "identity file must be mode 0600")},
		{"peer store permissions", peersOK, ternary(peersOK, "private (0600)", "peer store must be mode 0600")},
		{"clipboard watcher", clip.Running, clipDetail},
		{"control socket", socketOK, ternary(socketOK, "user-only", "socket permissions are too broad")},
		{"listener port", listenerOK, ternary(listenerOK, fmt.Sprintf("listening on TCP %d", d.cfg.ListenPort), fmt.Sprintf("not listening on TCP %d", d.cfg.ListenPort))},
	}
	if runtime.GOOS == "linux" {
		_, wp := exec.LookPath("wl-paste")
		_, wc := exec.LookPath("wl-copy")
		toolsOK := wp == nil && wc == nil
		sessionOK := os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("XDG_RUNTIME_DIR") != ""
		checks = append(checks,
			DoctorCheck{"wl-clipboard", toolsOK, ternary(toolsOK, "wl-paste and wl-copy found", "install wl-paste and wl-copy")},
			DoctorCheck{"Wayland session", sessionOK, ternary(sessionOK, "session variables available", "WAYLAND_DISPLAY and XDG_RUNTIME_DIR are required")},
		)
	}
	last, e := d.disc.Health()
	trusted := 0
	for _, p := range d.store.List() {
		if p.State == pairing.Trusted {
			trusted++
		}
	}
	discovered := len(d.disc.Peers())
	checks = append(checks,
		DoctorCheck{"mDNS browse", e == "" && !last.IsZero(), ternary(e == "", fmt.Sprintf("last browse %s", last.Format(time.RFC3339)), e)},
		DoctorCheck{"peer discovery", discovered > 0, fmt.Sprintf("%d nearby %s found", discovered, ternary(discovered == 1, "device", "devices"))},
		DoctorCheck{"peer authentication", trusted > 0, fmt.Sprintf("%d paired %s", trusted, ternary(trusted == 1, "device", "devices"))},
	)
	return checks
}
func fileMode(p string) os.FileMode {
	st, e := os.Stat(p)
	if e != nil {
		return 0777
	}
	return st.Mode().Perm()
}
func result(err error) control.Response {
	if err != nil {
		return fail(err)
	}
	return control.Response{OK: true}
}
func fail(err error) control.Response { return control.Response{Error: safeError(err)} }
func safeError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
func ternary(ok bool, a, b string) string {
	if ok {
		return a
	}
	return b
}

// ValidateCertificateIdentity is exposed for focused security tests.
func ValidateCertificateIdentity(cert *x509.Certificate, id string) bool {
	return cert.Subject.CommonName == "lanclip:"+id
}

// ResolveManualPeer returns LAN endpoints for diagnostics and fallback dialing.
func ResolveManualPeer(host string, port int) ([]string, error) {
	ips, err := net.LookupIP(strings.TrimSpace(host))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if discovery.IsDirectLANIP(ip) {
			out = append(out, net.JoinHostPort(ip.String(), fmt.Sprint(port)))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("manual peer %q has no address on a directly connected LAN", host)
	}
	sort.Strings(out)
	return out, nil
}
