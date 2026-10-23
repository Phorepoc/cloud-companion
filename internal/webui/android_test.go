// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/phecloud/internal/store"
)

func TestAndroidTicket(t *testing.T) {
	st := store.OpenTest(t)
	mux := http.NewServeMux()
	a := newAuth(st)
	a.androidRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(admin bool) *fakeDevice {
		id, key, err := st.NewDevice("d", admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	admin, phone := mk(true), mk(false)
	if code, _ := phone.call(t, "GET", "/phe/android/ticket", nil); code != 403 {
		t.Fatalf("a non-admin device must be refused: %d", code)
	}
	t.Setenv("PHECLOUD_ANDROID_SECRET", "")
	if code, r := admin.call(t, "GET", "/phe/android/ticket", nil); code != 200 || r["configured"] != false {
		t.Fatalf("unconfigured: %d %v", code, r)
	}
	t.Setenv("PHECLOUD_ANDROID_SECRET", "testsecret")
	code, r := admin.call(t, "GET", "/phe/android/ticket", nil)
	tk, _ := r["ticket"].(string)
	if code != 200 || r["configured"] != true || r["path"] != "/android/" || !strings.Contains(tk, ".") {
		t.Fatalf("ticket: %d %v", code, r)
	}
	// the bridge (cmd/pheandroid) verifies this exact format; the vector below is checked there too
	if got := androidTicket("testsecret", time.Unix(4102444800, 0)); got != "4102444800.6f7556a99bf59d268dc8fd61eafb859495275d0209bd9228857115aa9d5a4027" {
		t.Fatalf("ticket format changed: %s", got)
	}
}
