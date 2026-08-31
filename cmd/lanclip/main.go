package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/dpellerin/lanclip/internal/config"
	"github.com/dpellerin/lanclip/internal/daemon"
	"github.com/dpellerin/lanclip/internal/identity"
	"github.com/dpellerin/lanclip/internal/pairing"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lanclip:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	return runCLI(args, os.Stdin, os.Stdout)
}
func runDaemon(paths config.Paths) error {
	cfg, err := config.LoadOrCreate(paths)
	if err != nil {
		return err
	}
	id, err := identity.LoadOrCreate(paths.Identity)
	if err != nil {
		return err
	}
	if err = config.CheckPrivate(paths.Identity); err != nil {
		return err
	}
	store, err := pairing.Load(paths.Peers)
	if err != nil {
		return err
	}
	d, err := daemon.New(cfg, paths, id, store, version)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	return d.Run(ctx)
}
