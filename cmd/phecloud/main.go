// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Command phecloud: the Cloud Companion server.
//
//	phecloud serve              run the phone-protocol server (and the cloud browser)
//	phecloud paircode [--admin] print a single-use pairing code (valid 10 minutes); --admin: the new device may manage devices
//	phecloud devices            list paired devices
//	phecloud revoke <id>        revoke a device
//	phecloud hub ...            personal hub: per-device app passwords (see hub.go)
//
// Environment: PHECLOUD_DB (sqlite path), PHECLOUD_LISTEN (:7400), PHECLOUD_CHROME (chromium binary),
// PHECLOUD_MAX_PAGE_H (page height cap in px, 8192).
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/phorepoc/cloud-companion/internal/browser"
	"github.com/phorepoc/cloud-companion/internal/catalogue"
	"github.com/phorepoc/cloud-companion/internal/connectors"
	"github.com/phorepoc/cloud-companion/internal/davhub"
	"github.com/phorepoc/cloud-companion/internal/server"
	"github.com/phorepoc/cloud-companion/internal/store"
	"github.com/phorepoc/cloud-companion/internal/tlsutil"
	"github.com/phorepoc/cloud-companion/internal/webui"
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
	if os.Args[1] == "hub" { // personal hub: app passwords in Stalwart (hub.go); no local database needed
		hubMain(os.Args[2:])
		return
	}
	st, err := store.Open(env("PHECLOUD_DB", "phecloud.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "db:", err)
		os.Exit(1)
	}
	defer st.Close()
	switch os.Args[1] {
	case "paircode":
		code, err := st.NewPairCode(10*time.Minute, len(os.Args) > 2 && os.Args[2] == "--admin")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(code)
		if fp, err := tlsutil.FingerprintFile(env("PHECLOUD_TLSDIR", "/data")); err == nil {
			fmt.Fprintln(os.Stderr, "server certificate fingerprint (sha256):", fp)
		}
	case "snapshot": // phecloud snapshot <path>: a consistent copy of the database (used by hub backup)
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "snapshot <path>")
			os.Exit(2)
		}
		if err := st.Snapshot(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if errors.Is(err, store.ErrExternalDB) {
				os.Exit(3) // hub backup: not a failure, the operator backs the external database up
			}
			os.Exit(1)
		}
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
		br := browser.NewCDP(os.Getenv("PHECLOUD_CHROME"), maxH, 2)
		defer br.Close()
		log := slog.Default()
		tcfg, fp, err := tlsutil.LoadOrCreate(env("PHECLOUD_TLSDIR", "/data"))
		if err != nil {
			log.Error("tls", "err", err)
			os.Exit(1)
		}
		log.Info("TLS 1.3 certificate", "fingerprint", fp)
		ln, err := tls.Listen("tcp", env("PHECLOUD_LISTEN", ":7400"), tcfg)
		if err != nil {
			log.Error("listen", "err", err)
			os.Exit(1)
		}
		log.Info("phecloud listening", "addr", ln.Addr().String())
		srv := &server.Server{Store: st, Browser: br, Log: log, Hub: davhub.FromEnv()}
		if pl := os.Getenv("PHECLOUD_PLAIN_LISTEN"); pl != "" {
			// DEVELOPMENT ONLY: an unencrypted listener for the C client's host test (no TLS stack there).
			pln, err := net.Listen("tcp", pl)
			if err != nil {
				log.Error("plain listen", "err", err)
				os.Exit(1)
			}
			log.Warn("PLAIN (no TLS) listener enabled, development only", "addr", pln.Addr().String())
			go srv.Serve(pln)
		}
		if hl := env("PHECLOUD_HTTP_LISTEN", ":8080"); hl != "" && hl != "off" {
			// the web app and its WebSocket. Plain HTTP by default: put it behind a TLS reverse proxy for anything but
			// localhost, or set PHECLOUD_HTTP_TLS=1 to use the same self-signed certificate as the phone port.
			var hln net.Listener
			var herr error
			if os.Getenv("PHECLOUD_HTTP_TLS") == "1" {
				hln, herr = tls.Listen("tcp", hl, tcfg)
			} else {
				hln, herr = net.Listen("tcp", hl)
			}
			if herr != nil {
				log.Error("http listen", "err", herr)
				os.Exit(1)
			}
			log.Info("web app listening", "addr", hln.Addr().String())
			vault, verr := connectors.OpenVault(env("PHECLOUD_TLSDIR", "/data"))
			var mgr *connectors.Manager
			if verr != nil {
				log.Error("connectors disabled", "err", verr)
			} else {
				mgr = connectors.NewManager(st, srv.Hub, vault)
				srv.Sender = mgr
				if srv.Hub != nil {
					go mgr.Loop(context.Background(), 15*time.Minute)
				}
			}
			// self-hosted catalogues (docs/catalogue.md): the files live next to the other data; their federated sources sync in the background
			cat := catalogue.New(st, env("PHECLOUD_CATALOGUE_DIR", filepath.Join(env("PHECLOUD_TLSDIR", "/data"), "catalogue")))
			if v, _ := strconv.ParseInt(os.Getenv("PHECLOUD_CATALOGUE_MAX_FILE"), 10, 64); v > 0 {
				cat.MaxFile = v
			}
			go cat.Loop(context.Background(), time.Minute)
			// timeouts: a client that never finishes its request headers must not hold a connection (and a goroutine) forever; WebSockets and long uploads are not limited by these
			hs := &http.Server{Handler: webui.Handler(srv, mgr, cat), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
			go hs.Serve(hln)
		}
		log.Error("serve", "err", srv.Serve(ln))
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
}
