// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// Pairing codes from the web app (Settings, "Pair a new device"), the same single-use codes `phecloud paircode [--admin]` prints.
//
//	POST /phe/pair/code {admin: bool}   (admin devices, signed)  -> {code, expires, ttl, admin, fingerprint}
//
// `expires` is a unix time, `ttl` the lifetime in seconds, `fingerprint` the SHA-256 of the phone-port certificate (what the CLI prints), which a new device can
// check at pairing. A normal code makes a device that can browse; an admin code makes a device that can also manage devices, so it is only given on request.

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/phorepoc/cloud-companion/internal/tlsutil"
)

const pairCodeTTL = 10 * time.Minute

func (a *auth) pairRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/phe/pair/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			bad(w, 405, "use POST")
			return
		}
		if !a.adminDevice(r) {
			bad(w, 403, "only an admin device can pair new devices")
			return
		}
		var in struct{ Admin bool }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
			bad(w, 400, "bad request")
			return
		}
		code, err := a.st.NewPairCode(pairCodeTTL, in.Admin)
		if err != nil {
			bad(w, 500, "could not make a code")
			return
		}
		dir := os.Getenv("PHECLOUD_TLSDIR")
		if dir == "" {
			dir = "/data"
		}
		fp, _ := tlsutil.FingerprintFile(dir) // empty when this server has no phone-port certificate (a test)
		reply(w, 200, map[string]any{"code": code, "expires": time.Now().Add(pairCodeTTL).Unix(), "ttl": int(pairCodeTTL.Seconds()), "admin": in.Admin, "fingerprint": fp})
	})
}
