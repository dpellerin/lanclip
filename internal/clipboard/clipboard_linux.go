//go:build linux

package clipboard

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type platformAdapter struct {
	max      int
	socket   string
	listener net.Listener
	mu       sync.RWMutex
	health   Health
	ctx      context.Context
	cancel   context.CancelFunc
	owner    <-chan struct{}
}

func New(max int) *platformAdapter {
	run := os.Getenv("XDG_RUNTIME_DIR")
	return &platformAdapter{max: max, socket: filepath.Join(run, "lanclip-clipboard.sock")}
}
func (a *platformAdapter) Start(parent context.Context) (<-chan Event, error) {
	if os.Getenv("WAYLAND_DISPLAY") == "" || os.Getenv("XDG_RUNTIME_DIR") == "" {
		return nil, errors.New("WAYLAND_DISPLAY and XDG_RUNTIME_DIR are required")
	}
	if _, err := exec.LookPath("wl-paste"); err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("wl-copy"); err != nil {
		return nil, err
	}
	_ = os.Remove(a.socket)
	ln, err := net.Listen("unix", a.socket)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(a.socket, 0600); err != nil {
		ln.Close()
		return nil, err
	}
	a.listener = ln
	ctx, cancel := context.WithCancel(parent)
	a.ctx = ctx
	a.cancel = cancel
	out := make(chan Event, 32)
	a.setHealth(true, "")
	go a.accept(ctx, out)
	go a.watchLoop(ctx)
	return out, nil
}
func (a *platformAdapter) accept(ctx context.Context, out chan<- Event) {
	defer close(out)
	var handlers sync.WaitGroup
	defer handlers.Wait()
	for {
		c, err := a.listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				a.setHealth(false, err.Error())
			}
			return
		}
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer c.Close()
			_ = c.SetReadDeadline(time.Now().Add(time.Second))
			var h [4]byte
			if _, e := io.ReadFull(c, h[:]); e != nil {
				return
			}
			n := binary.BigEndian.Uint32(h[:])
			if n == 0 || n > uint32(a.max) {
				return
			}
			b := make([]byte, n)
			if _, e := io.ReadFull(c, b); e != nil {
				return
			}
			a.mu.Lock()
			a.health.LastEvent = time.Now().Format(time.RFC3339)
			a.mu.Unlock()
			select {
			case out <- Event{Text: string(b)}:
			case <-ctx.Done():
			}
		}()
	}
}
func (a *platformAdapter) watchLoop(ctx context.Context) {
	exe, err := os.Executable()
	if err != nil {
		a.setHealth(false, err.Error())
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "wl-paste", "--type", "text", "--watch", exe, "clipboard-event", a.socket)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		err = cmd.Run()
		if ctx.Err() != nil {
			return
		}
		a.setHealth(false, "clipboard watcher exited")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
		a.setHealth(true, "")
	}
}
func (a *platformAdapter) Write(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// wl-copy also offers the standard plain-text MIME types. The unique
	// offer lets us confirm this write even if the previous text was identical.
	mime := "text/plain;charset=utf-8;lanclip=" + rand.Text()
	cmd := exec.CommandContext(a.ctx, "wl-copy", "--foreground", "--type", mime)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		types, err := exec.CommandContext(ctx, "wl-paste", "--list-types").Output()
		if err == nil && slices.Contains(strings.Split(string(types), "\n"), mime) {
			a.mu.Lock()
			a.owner = done
			a.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-done
			return ctx.Err()
		case <-done:
			return errors.New("wl-copy exited before acquiring clipboard ownership")
		case <-tick.C:
		}
	}
}

func (a *platformAdapter) Remote() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.owner == nil {
		return false
	}
	// wl-copy exits when the compositor cancels its ownership. Paste requests
	// leave it running, and every replacement cancels it regardless of MIME.
	select {
	case <-a.owner:
		return false
	default:
		return true
	}
}
func (a *platformAdapter) Health() Health { a.mu.RLock(); defer a.mu.RUnlock(); return a.health }
func (a *platformAdapter) setHealth(r bool, e string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.health.Running = r
	a.health.LastError = e
}
func (a *platformAdapter) Close() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.listener != nil {
		_ = a.listener.Close()
	}
	if a.socket != "" {
		_ = os.Remove(a.socket)
	}
	return nil
}

// SendWatchEvent is the private wl-paste callback entry point.
func SendWatchEvent(socket string, r io.Reader, max int) error {
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if len(b) > max {
		return errors.New("clipboard exceeds size limit")
	}
	c, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	if _, err = c.Write(h[:]); err != nil {
		return err
	}
	_, err = c.Write(b)
	return err
}
