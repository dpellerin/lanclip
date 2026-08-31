package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"time"
)

type Request struct {
	Command  string `json:"command"`
	Argument string `json:"argument,omitempty"`
}
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}
type Handler func(context.Context, Request) Response
type Server struct {
	path string
	ln   net.Listener
}

func Listen(path string, h Handler) (*Server, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{path: path, ln: ln}
	go s.serve(h)
	return s, nil
}
func (s *Server) serve(h Handler) {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(15 * time.Second))
			var req Request
			if err := json.NewDecoder(bufio.NewReader(c)).Decode(&req); err != nil {
				_ = json.NewEncoder(c).Encode(Response{Error: "invalid request: " + err.Error()})
				return
			}
			_ = json.NewEncoder(c).Encode(h(context.Background(), req))
		}()
	}
}
func (s *Server) Close() error {
	if s.ln != nil {
		_ = s.ln.Close()
	}
	return os.Remove(s.path)
}

func Call(ctx context.Context, path string, req Request) (Response, error) {
	d := net.Dialer{}
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return Response{}, err
	}
	defer c.Close()
	deadline := time.Now().Add(15 * time.Second)
	_ = c.SetDeadline(deadline)
	if err = json.NewEncoder(c).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err = json.NewDecoder(c).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK && resp.Error == "" {
		return resp, errors.New("daemon returned an invalid response")
	}
	return resp, nil
}
