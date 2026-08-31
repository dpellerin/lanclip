package protocol

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestFrameRoundTripAndPartialReads(t *testing.T) {
	var b bytes.Buffer
	want := Message{Type: "clipboard", Protocol: 1, EventID: "e", MIME: "text/plain;charset=utf-8", Text: "héllo\n🙂"}
	if err := Write(&b, want, DefaultMaxFrame); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&oneByteReader{r: &b}, DefaultMaxFrame)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != want.Text {
		t.Fatalf("got %q", got.Text)
	}
}

type oneByteReader struct{ r *bytes.Buffer }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return o.r.Read(p)
}

func TestRejectMalformedAndOversized(t *testing.T) {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(100))
	b.WriteString("x")
	if _, err := Read(&b, 10); err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("err=%v", err)
	}
	b.Reset()
	binary.Write(&b, binary.BigEndian, uint32(1))
	b.WriteByte('{')
	if _, err := Read(&b, 10); err == nil {
		t.Fatal("accepted malformed JSON")
	}
}

func TestMultipleFrames(t *testing.T) {
	var b bytes.Buffer
	_ = Write(&b, Message{Type: "ping", Nonce: "1"}, 1000)
	_ = Write(&b, Message{Type: "pong", Nonce: "1"}, 1000)
	a, _ := Read(&b, 1000)
	c, _ := Read(&b, 1000)
	if a.Type != "ping" || c.Type != "pong" {
		t.Fatal(a, c)
	}
}

func TestWriteHandlesPartialWrites(t *testing.T) {
	var dst bytes.Buffer
	w := &shortWriter{dst: &dst, max: 3}
	want := Message{Type: "clipboard", Text: "partial write 🙂"}
	if err := Write(w, want, DefaultMaxFrame); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&dst, DefaultMaxFrame)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.Text != want.Text {
		t.Fatalf("got %+v", got)
	}
}

type shortWriter struct {
	dst *bytes.Buffer
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.dst.Write(p)
}
