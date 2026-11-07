// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Command phehost: the host manager (docs/host-manager.md). It runs on the board as a system service, outside Docker, and
// switches Cloud Companion's features on and off for Cloud Companion, which asks over a Unix socket.
//
//	phehost -compose /opt/cloud -features /opt/cloud/deploy/features -state /var/lib/phehost -socket /run/phehost/api.sock
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/phorepoc/cloud-companion/internal/host"
)

func main() {
	compose := flag.String("compose", "/opt/cloud", "the folder with docker-compose.yml")
	features := flag.String("features", "", "the feature catalogue (default: <compose>/deploy/features)")
	stateDir := flag.String("state", "/var/lib/phehost", "where the chosen features are kept")
	sock := flag.String("socket", "/run/phehost/api.sock", "the API socket (mounted into Cloud Companion's container)")
	flag.Parse()
	if *features == "" {
		*features = *compose + "/deploy/features"
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cat, err := host.LoadCatalogue(os.DirFS(*features), ".")
	if err != nil {
		log.Error("catalogue", "err", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		log.Error("state", "err", err)
		os.Exit(1)
	}
	m := &host.Manager{Cat: cat, Board: host.DetectBoard(), ComposeDir: *compose, StateDir: *stateDir, Run: host.ExecRunner{}}
	if err := m.Load(); err != nil {
		log.Error("state", "err", err)
		os.Exit(1)
	}
	log.Info("board", "model", m.Board.Model, "arch", m.Board.Arch, "memory", m.Board.Memory)

	os.Remove(*sock)
	l, err := net.Listen("unix", *sock)
	if err != nil {
		log.Error("socket", "err", err)
		os.Exit(1)
	}
	os.Chmod(*sock, 0o660) // the socket's group is the one Cloud Companion's container runs with

	j := m.Apply() // the features as they were chosen
	go func() {
		<-j.Done()
		st := j.Status()
		log.Info("features applied", "state", st.State, "error", st.Error)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Handler: m.Handler()}
	go func() { <-ctx.Done(); srv.Close() }()
	log.Info("listening", "socket", *sock)
	if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
