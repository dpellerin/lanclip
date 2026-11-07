//go:build darwin

package main

/*
#cgo LDFLAGS: -framework AppKit
void lanclip_run_indicator(void);
void lanclip_update_indicator(int remote);
*/
import "C"

import (
	"context"
	"io"
	"os"
	"runtime"
)

func init() {
	if len(os.Args) == 2 && os.Args[1] == "indicator" {
		runtime.LockOSThread()
	}
}

func runIndicator(socket string, _ io.Writer) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = watchClipboardOrigin(ctx, socket, func(remote bool) error {
			value := C.int(0)
			if remote {
				value = 1
			}
			C.lanclip_update_indicator(value)
			return nil
		})
	}()
	C.lanclip_run_indicator()
	return nil
}
