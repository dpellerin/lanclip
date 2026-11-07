package main

import (
	"context"
	"time"

	"github.com/dpellerin/lanclip/internal/control"
)

func watchClipboardOrigin(ctx context.Context, socket string, update func(bool) error) error {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var last bool
	first := true
	for {
		callCtx, cancel := context.WithTimeout(ctx, time.Second)
		response, err := control.Call(callCtx, socket, control.Request{Command: "clipboard-origin"})
		cancel()
		remote, _ := response.Data.(bool)
		remote = remote && err == nil && response.OK
		if first || remote != last {
			if err := update(remote); err != nil {
				return err
			}
			last, first = remote, false
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
