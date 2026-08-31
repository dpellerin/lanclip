package transport

import (
	"context"
	"math/rand"
	"time"
)

type Backoff struct {
	Attempt  int
	Min, Max time.Duration
}

func (b *Backoff) Next() time.Duration {
	min, max := b.Min, b.Max
	if min == 0 {
		min = 250 * time.Millisecond
	}
	if max == 0 {
		max = 10 * time.Second
	}
	d := min
	for i := 0; i < b.Attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	b.Attempt++
	j := time.Duration(rand.Int63n(int64(d/4 + 1)))
	return d - d/8 + j
}
func (b *Backoff) Reset() { b.Attempt = 0 }
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
