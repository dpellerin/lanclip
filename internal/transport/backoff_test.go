package transport

import (
	"testing"
	"time"
)

func TestBackoffBounds(t *testing.T) {
	b := Backoff{}
	for i := 0; i < 20; i++ {
		d := b.Next()
		if d < 218*time.Millisecond || d > 11250*time.Millisecond {
			t.Fatalf("out of bounds %s", d)
		}
	}
	b.Reset()
	if b.Attempt != 0 {
		t.Fatal("not reset")
	}
}
