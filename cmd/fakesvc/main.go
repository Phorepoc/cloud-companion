// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Command fakesvc runs the stand-in services for the browser suites (internal/fakes), all at once or the ones named:
//
//	fakesvc [name ...]
//
// It is a test tool: the release image does not contain it (Dockerfile target "fakes", web/test/compose.fakes.yml).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/phorepoc/cloud-companion/internal/fakes"
	// each fake registers itself
	_ "github.com/phorepoc/cloud-companion/internal/fakes/sip"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	want := os.Args[1:]
	if len(want) == 1 && want[0] == "all" {
		want = nil
	}
	errs := make(chan error)
	n := 0
	for _, f := range fakes.All() {
		if len(want) > 0 && !slices.Contains(want, f.Name) {
			continue
		}
		n++
		log.Printf("fake %s", f.Name)
		go func() { errs <- f.Run(ctx) }()
	}
	if n == 0 {
		log.Fatal("no fakes to run")
	}
	select {
	case <-ctx.Done():
	case err := <-errs:
		log.Fatal(err)
	}
}
