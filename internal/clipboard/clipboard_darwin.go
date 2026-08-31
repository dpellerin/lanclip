//go:build darwin

package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>
long lanclip_change_count(void);
char *lanclip_read_string(void);
int lanclip_write_string(const char *value);
*/
import "C"

import (
	"context"
	"errors"
	"sync"
	"time"
	"unsafe"
)

type platformAdapter struct {
	max    int
	mu     sync.RWMutex
	health Health
	cancel context.CancelFunc
}

func New(max int) *platformAdapter { return &platformAdapter{max: max} }
func (a *platformAdapter) Start(parent context.Context) (<-chan Event, error) {
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	out := make(chan Event, 32)
	a.mu.Lock()
	a.health.Running = true
	a.mu.Unlock()
	go func() {
		defer close(out)
		last := int64(C.lanclip_change_count())
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				n := int64(C.lanclip_change_count())
				if n == last {
					continue
				}
				last = n
				p := C.lanclip_read_string()
				if p == nil {
					continue
				}
				text := C.GoString(p)
				C.free(unsafe.Pointer(p))
				if text == "" || len(text) > a.max {
					continue
				}
				a.mu.Lock()
				a.health.LastEvent = time.Now().Format(time.RFC3339)
				a.mu.Unlock()
				select {
				case out <- Event{Text: text}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
func (a *platformAdapter) Write(ctx context.Context, text string) error {
	if len(text) > a.max {
		return errors.New("clipboard exceeds size limit")
	}
	p := C.CString(text)
	defer C.free(unsafe.Pointer(p))
	if C.lanclip_write_string(p) == 0 {
		return errors.New("AppKit pasteboard write failed")
	}
	return nil
}
func (a *platformAdapter) Health() Health { a.mu.RLock(); defer a.mu.RUnlock(); return a.health }
func (a *platformAdapter) Close() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Lock()
	a.health.Running = false
	a.mu.Unlock()
	return nil
}

func SendWatchEvent(string, interface{}, int) error {
	return errors.New("clipboard-event is Linux-only")
}
