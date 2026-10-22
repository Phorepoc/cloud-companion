// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Command phecloud: the Phecloud server.
//
//	phecloud serve              run the phone-protocol server (and the cloud browser)
//	phecloud paircode           print a single-use pairing code (valid 10 minutes)
//	phecloud devices            list paired devices
//	phecloud revoke <id>        revoke a device
//
// Environment: PHECLOUD_DB (sqlite path), PHECLOUD_LISTEN (:7400), PHECLOUD_CHROME (chromium binary),
// PHECLOUD_MAX_PAGE_H (page height cap in px, 8192).
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/phorepoc/phecloud/internal/browser"
	"github.com/phorepoc/phecloud/internal/server"
	"github.com/phorepoc/phecloud/internal/store"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: phecloud serve|paircode|devices|revoke <id>")
		os.Exit(2)
	}
	st, err := store.Open(env("PHECLOUD_DB", "phecloud.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "db:", err)
		os.Exit(1)
	}
	defer st.Close()
	switch os.Args[1] {
	case "paircode":
		code, err := st.NewPairCode(10 * time.Minute)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(code)
	case "devices":
		ds, _ := st.List()
		for _, d := range ds {
			fmt.Printf("%s  %-20s created %s  revoked=%v\n", d.ID, d.Name, time.Unix(d.Created, 0).Format(time.RFC3339), d.Revoked)
		}
	case "revoke":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "revoke <id>")
			os.Exit(2)
		}
		if err := st.Revoke(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "serve":
		maxH, _ := strconv.Atoi(env("PHECLOUD_MAX_PAGE_H", "8192"))
		br := browser.NewCDP(os.Getenv("PHECLOUD_CHROME"), maxH)
		defer br.Close()
		log := slog.Default()
		ln, err := net.Listen("tcp", env("PHECLOUD_LISTEN", ":7400"))
		if err != nil {
			log.Error("listen", "err", err)
			os.Exit(1)
		}
		log.Info("phecloud listening", "addr", ln.Addr().String())
		srv := &server.Server{Store: st, Browser: br, Log: log}
		log.Error("serve", "err", srv.Serve(ln))
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
}
