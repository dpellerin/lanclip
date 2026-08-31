package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const Version = 1

// The application text limit is 1 MiB. encoding/json can expand some bytes to
// six-byte escapes, so the wire limit leaves bounded headroom while the daemon
// separately enforces the tighter decoded-text limit.
const DefaultMaxFrame = 7 << 20

type Message struct {
	Type        string `json:"type"`
	Protocol    int    `json:"protocol"`
	DeviceID    string `json:"device_id,omitempty"`
	Name        string `json:"name,omitempty"`
	EventID     string `json:"event_id,omitempty"`
	MIME        string `json:"mime,omitempty"`
	Text        string `json:"text,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
	Code        string `json:"code,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	ErrorCode   string `json:"code_value,omitempty"`
}

func Write(w io.Writer, m Message, max int) error {
	if m.Protocol == 0 {
		m.Protocol = Version
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) > max {
		return fmt.Errorf("frame exceeds %d bytes", max)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if err := writeAll(w, hdr[:]); err != nil {
		return err
	}
	return writeAll(w, b)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

func Read(r io.Reader, max int) (Message, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Message{}, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || uint64(n) > uint64(max) {
		return Message{}, fmt.Errorf("invalid frame length %d", n)
	}
	b := make([]byte, int(n))
	if _, err := io.ReadFull(r, b); err != nil {
		return Message{}, err
	}
	if !utf8.Valid(b) {
		return Message{}, errors.New("frame is not UTF-8")
	}
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return Message{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if m.Protocol != Version {
		return Message{}, fmt.Errorf("unsupported protocol %d", m.Protocol)
	}
	if m.Type == "" {
		return Message{}, errors.New("message type is required")
	}
	return m, nil
}
