// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// Android in the browser (cmd/pheandroid is the stream bridge; docs/android.md).
//
//	GET /phe/android/ticket   (admin devices, signed)  -> {configured, ticket, path}
//
// The bridge shares a secret (PHECLOUD_ANDROID_SECRET here, PHE_ANDROID_SECRET there) and refuses a stream without a ticket: "<expiry unix>.<hex HMAC-SHA256(secret, "android."+expiry)>".
// A ticket is good for two minutes and only opens the stream (the viewer then stays connected), so a signed-in admin device is the only way to reach the phone.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (a *auth) androidRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/phe/android/ticket", func(w http.ResponseWriter, r *http.Request) {
		if !a.adminDevice(r) {
			bad(w, 403, "only an admin device can open Android")
			return
		}
		secret := os.Getenv("PHECLOUD_ANDROID_SECRET")
		if secret == "" {
			reply(w, 200, map[string]any{"configured": false})
			return
		}
		reply(w, 200, map[string]any{"configured": true, "ticket": androidTicket(secret, time.Now().Add(2*time.Minute)), "path": "/android/"})
	})
}

func androidTicket(secret string, exp time.Time) string {
	e := strconv.FormatInt(exp.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("android." + e))
	return e + "." + hex.EncodeToString(m.Sum(nil))
}
