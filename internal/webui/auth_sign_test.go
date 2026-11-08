// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// A signed request is good once, for its own address and body; admin routes take only the web app's v2 signatures, while a
// phone's v1 signature still reaches the routes for any paired device.
func TestSignedRequests(t *testing.T) {
	st := store.OpenTest(t)
	a := newAuth(st)
	id, key, err := st.NewDevice("admin", true)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		if !a.adminDevice(r) {
			w.WriteHeader(403)
		}
	})
	mux.HandleFunc("/any", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := a.signedDevice(r); !ok {
			w.WriteHeader(403)
		}
	})
	mux.HandleFunc("/readfirst", func(w http.ResponseWriter, r *http.Request) { // as /passkey/register/begin: the body, then the check
		io.ReadAll(r.Body)
		if !a.adminDevice(r) {
			w.WriteHeader(403)
		}
	})
	srv := httptest.NewServer(keepSignedBody(mux))
	defer srv.Close()
	send := func(path, body, hdr string) int {
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewBufferString(body))
		req.Header.Set("X-Phe-Auth", hdr)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	sign := func(path, body string) string {
		req, _ := http.NewRequest("POST", srv.URL+path, nil)
		return signV2(id, key, "POST", req.URL, []byte(body))
	}

	h := sign("/admin?x=1", `{"on":true}`)
	if c := send("/admin?x=1", `{"on":true}`, h); c != 200 {
		t.Fatalf("a good request was refused: %d", c)
	}
	if c := send("/readfirst", `{"code":""}`, sign("/readfirst", `{"code":""}`)); c != 200 {
		t.Errorf("a handler that reads the body before the check refused a good request: %d", c)
	}
	if c := send("/admin?x=1", `{"on":true}`, h); c != 403 {
		t.Error("the same header was taken twice")
	}
	if c := send("/admin?x=1", `{"on":true,"password":"theirs"}`, sign("/admin?x=1", `{"on":true}`)); c != 403 {
		t.Error("a changed body was taken")
	}
	if c := send("/admin?x=2", `{}`, sign("/admin?x=1", `{}`)); c != 403 {
		t.Error("a changed query was taken")
	}
	if c := send("/admin", `{}`, signV2Hash(id, key, "POST", mustURL(srv.URL+"/admin"), "u")); c != 403 {
		t.Error("an unread body was taken for a JSON request")
	}

	ts := fmt.Sprint(time.Now().Unix())
	m := hmac.New(sha256.New, key)
	m.Write([]byte(id + "." + ts + ".POST /admin"))
	v1 := id + "." + ts + "." + hex.EncodeToString(m.Sum(nil))
	if c := send("/admin", `{}`, v1); c != 403 {
		t.Error("an admin route took a v1 signature")
	}
	m = hmac.New(sha256.New, key)
	m.Write([]byte(id + "." + ts + ".POST /any"))
	if c := send("/any", `{}`, id+"."+ts+"."+hex.EncodeToString(m.Sum(nil))); c != 200 {
		t.Errorf("a phone's v1 signature no longer works: %d", c)
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

// Passkey sign-ins can be started by anyone, so how many may start from one address, and how many may wait, is capped.
func TestPasskeyStartsLimited(t *testing.T) {
	a := newAuth(store.OpenTest(t))
	r := httptest.NewRequest("POST", "/passkey/login/begin", nil)
	for i := 0; i < 20; i++ {
		if !a.starts(r) {
			t.Fatalf("start %d refused", i+1)
		}
	}
	if a.starts(r) {
		t.Error("a 21st start within a minute was taken")
	}
	other := httptest.NewRequest("POST", "/passkey/login/begin", nil)
	other.RemoteAddr = "192.0.2.9:1234"
	if !a.starts(other) {
		t.Error("another address was refused")
	}
	for i := 0; i < maxPending; i++ {
		if a.keep(&pending{}) == "" {
			t.Fatalf("pending %d refused", i+1)
		}
	}
	if a.keep(&pending{}) != "" {
		t.Error("more sign-ins waiting than the cap")
	}
}
