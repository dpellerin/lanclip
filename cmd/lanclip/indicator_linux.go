//go:build linux

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func runIndicator(socket string, output io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	encoder := json.NewEncoder(output)
	return watchClipboardOrigin(ctx, socket, func(remote bool) error {
		class := "local"
		if remote {
			class = "remote"
		}
		return encoder.Encode(struct {
			Text  string `json:"text"`
			Class string `json:"class"`
		}{Text: "LC", Class: class})
	})
}
