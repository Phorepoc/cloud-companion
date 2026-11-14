// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package signal

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The replies the browser suite relies on: the API is there, nothing is linked, and the link QR code is a PNG.
func TestFake(t *testing.T) {
	m := New("json-rpc")
	srv := httptest.NewServer(m)
	defer srv.Close()
	get := func(p string) (int, string) {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	if c, b := get("/v1/about"); c != 200 || !strings.Contains(b, `"mode":"json-rpc"`) {
		t.Fatalf("about: %d %s", c, b)
	}
	if c, b := get("/v1/accounts"); c != 200 || b != "[]" {
		t.Fatalf("accounts: %d %s", c, b)
	}
	if c, b := get("/v1/qrcodelink?device_name=x"); c != 200 || !bytes.HasPrefix([]byte(b), []byte("\x89PNG")) || len(b) < 200 {
		t.Fatalf("QR code: %d, %d bytes", c, len(b))
	}
	if c, _ := get("/v1/groups/+306900000001"); c != 400 {
		t.Fatalf("groups of an unlinked number: %d", c)
	}
	m.Link("+306900000001", "")
	if c, b := get("/v1/accounts"); b != `["+306900000001"]` {
		t.Fatalf("accounts after linking: %d %s", c, b)
	}
	r, _ := http.Post(srv.URL+"/v2/send", "application/json", strings.NewReader(`{"number":"+306900000001","recipients":["`+Unregistered+`"],"message":"x"}`))
	if r.StatusCode != 400 {
		t.Fatalf("send to an unregistered user: %d", r.StatusCode)
	}
	r, _ = http.Post(srv.URL+"/v2/send", "application/json", strings.NewReader(`{"number":"+306900000001","recipients":["+306900000002"],"message":"x"}`))
	if r.StatusCode != 201 || len(m.Sent()) != 1 {
		t.Fatalf("send: %d %v", r.StatusCode, m.Sent())
	}
}
