// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
	"github.com/phorepoc/cloud-companion/internal/tlsutil"
)

func TestPairCodeFromTheWebApp(t *testing.T) {
	st := store.OpenTest(t)
	dir := t.TempDir()
	t.Setenv("PHECLOUD_TLSDIR", dir)
	_, fp, err := tlsutil.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	newAuth(st).pairRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(admin bool) *fakeDevice {
		id, key, err := st.NewDevice("d", admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	admin, plain := mk(true), mk(false)

	// who may ask
	if code, _ := plain.call(t, "POST", "/phe/pair/code", map[string]any{"admin": false}); code != 403 {
		t.Fatalf("a non-admin device must be refused: %d", code)
	}
	if code, _ := (&fakeDevice{id: "nobody", key: []byte("k"), srv: srv}).call(t, "POST", "/phe/pair/code", map[string]any{}); code != 403 {
		t.Fatalf("an unknown device must be refused: %d", code)
	}
	if code, _ := admin.call(t, "GET", "/phe/pair/code", nil); code != 405 {
		t.Fatalf("only POST: %d", code)
	}
	resp, err := http.Post(srv.URL+"/phe/pair/code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("an unsigned request must be refused: %d", resp.StatusCode)
	}

	// a normal code: shape, expiry, fingerprint, and it works exactly once
	code, r := admin.call(t, "POST", "/phe/pair/code", map[string]any{"admin": false})
	if code != 200 {
		t.Fatalf("admin asks: %d %v", code, r)
	}
	c, _ := r["code"].(string)
	exp, _ := r["expires"].(float64)
	if len(c) != 9 || c[4] != '-' || r["admin"] != false || r["ttl"].(float64) != 600 || r["fingerprint"] != fp {
		t.Fatalf("answer: %v (want fingerprint %s)", r, fp)
	}
	if d := time.Until(time.Unix(int64(exp), 0)); d < 9*time.Minute || d > 10*time.Minute+5*time.Second {
		t.Fatalf("expiry is %v away, want about 10 minutes", d)
	}
	id, _, isAdmin, err := st.Pair(c, "new phone")
	if err != nil || id == "" || isAdmin {
		t.Fatalf("pairing with the code: %v admin=%v", err, isAdmin)
	}
	if _, _, _, err := st.Pair(c, "again"); err == nil {
		t.Fatal("a pairing code must work only once")
	}

	// an admin code makes an admin device; a second request makes a different code
	_, r2 := admin.call(t, "POST", "/phe/pair/code", map[string]any{"admin": true})
	c2, _ := r2["code"].(string)
	if r2["admin"] != true || c2 == c {
		t.Fatalf("admin code: %v", r2)
	}
	if _, _, isAdmin, err := st.Pair(c2, "second admin"); err != nil || !isAdmin {
		t.Fatalf("an admin code must make an admin device: %v %v", err, isAdmin)
	}
}
