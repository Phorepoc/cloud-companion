// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/store"
)

func TestPrefsAPI(t *testing.T) {
	st := store.OpenTest(t)
	mux := http.NewServeMux()
	(&prefsAPI{a: newAuth(st), st: st}).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(admin bool) *fakeDevice {
		id, key, err := st.NewDevice("d", admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	laptop, phone := mk(true), mk(false)

	if code, _ := (&fakeDevice{"nobody", []byte("x"), srv}).call(t, "GET", "/phe/prefs", nil); code != 403 {
		t.Fatalf("an unpaired caller got %d", code)
	}
	if code, _ := (&fakeDevice{"nobody", []byte("x"), srv}).call(t, "PUT", "/phe/prefs/notes", map[string]string{"value": "x"}); code != 403 {
		t.Fatalf("an unpaired caller wrote: %d", code)
	}
	if code, _ := laptop.call(t, "PUT", "/phe/prefs/desktop.notes", map[string]string{"value": `["milk"]`}); code != 200 {
		t.Fatalf("put: %d", code)
	}
	// another device of the same server sees it
	code, j := phone.call(t, "GET", "/phe/prefs", nil)
	if p, _ := j["prefs"].(map[string]any); code != 200 || p["desktop.notes"] != `["milk"]` {
		t.Fatalf("get from the other device: %d %v", code, j)
	}
	// only the app's own keys: never the server's settings
	if st.SetSetting("weather.key", "secret") != nil {
		t.Fatal("setting")
	}
	if _, j := phone.call(t, "GET", "/phe/prefs", nil); len(j["prefs"].(map[string]any)) != 1 {
		t.Fatalf("other settings leaked: %v", j)
	}
	for _, k := range []string{"Bad", "a/b", strings.Repeat("a", 41)} {
		if code, _ := laptop.call(t, "PUT", "/phe/prefs/"+k, map[string]string{"value": "x"}); code == 200 {
			t.Fatalf("key %q accepted", k)
		}
	}
	if code, _ := laptop.call(t, "PUT", "/phe/prefs/big", map[string]string{"value": strings.Repeat("x", prefsMax+1)}); code != 400 {
		t.Fatalf("too large: %d", code)
	}
}
