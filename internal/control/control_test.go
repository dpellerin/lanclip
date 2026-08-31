package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s")
	s, e := Listen(p, func(_ context.Context, r Request) Response { return Response{OK: true, Data: r.Argument} })
	if e != nil {
		if errors.Is(e, os.ErrPermission) || errors.Is(e, syscall.EPERM) {
			t.Skip("sandbox does not permit Unix sockets")
		}
		t.Fatal(e)
	}
	defer s.Close()
	got, e := Call(context.Background(), p, Request{Command: "x", Argument: "y"})
	if e != nil || !got.OK {
		t.Fatal(got, e)
	}
}
