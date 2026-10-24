// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/connectors"
	"github.com/phorepoc/cloud-companion/internal/store"
)

func TestOAuthFlow(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("code") != "good-code" || r.PostForm.Get("code_verifier") == "" || r.PostForm.Get("redirect_uri") != "https://cloud.example.org/phe/oauth/callback" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_grant","error_description":"bad code or redirect"}`)
			return
		}
		idt := "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"me@gmail.test"}`)) + ".y"
		fmt.Fprintf(w, `{"access_token":"tok1","refresh_token":"ref1","expires_in":3600,"id_token":%q}`, idt)
	}))
	defer tokenSrv.Close()
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_ID", "cid")
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_SECRET", "csecret")
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_AUTH_URL", "https://accounts.example/auth")
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_TOKEN_URL", tokenSrv.URL)
	t.Setenv("PHECLOUD_PUBLIC_URL", "https://cloud.example.org/")

	st := store.OpenTest(t)
	v, _ := connectors.OpenVault(t.TempDir())
	m := connectors.NewManager(st, nil, v)
	a := newAuth(st)
	mux := http.NewServeMux()
	(&oauthAPI{a: a, m: m, st: map[string]*oauthState{}}).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	id, key, _ := st.NewDevice("web", true)
	admin := &fakeDevice{id, key, srv}
	pid, pkey, _ := st.NewDevice("phone", false)
	phone := &fakeDevice{pid, pkey, srv}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	code, r := admin.call(t, "GET", "/phe/oauth/providers", nil)
	provs, _ := r["providers"].([]any)
	if code != 200 || len(provs) != 1 || provs[0].(map[string]any)["id"] != "google" || r["redirect"] != "https://cloud.example.org/phe/oauth/callback" {
		t.Fatalf("providers (only the configured one, the public redirect): %d %v", code, r)
	}
	if code, _ := phone.call(t, "POST", "/phe/oauth/start", map[string]any{"provider": "google", "kinds": []string{"mail"}}); code != 403 {
		t.Fatalf("a phone must not start a sign-in: %d", code)
	}
	if code, _ := admin.call(t, "POST", "/phe/oauth/start", map[string]any{"provider": "microsoft", "kinds": []string{"mail"}}); code != 404 {
		t.Fatalf("an unconfigured provider: %d", code)
	}
	_, r = admin.call(t, "POST", "/phe/oauth/start", map[string]any{"provider": "google", "kinds": []string{"mail", "contacts"}, "name": "Gmail"})
	u, err := url.Parse(r["url"].(string))
	if err != nil || u.Host != "accounts.example" {
		t.Fatalf("authorization URL: %v %v", r, err)
	}
	q := u.Query()
	state := q.Get("state")
	if q.Get("redirect_uri") != "https://cloud.example.org/phe/oauth/callback" || q.Get("client_id") != "cid" || q.Get("code_challenge") == "" || state == "" ||
		!strings.Contains(q.Get("scope"), "mail.google.com") || !strings.Contains(q.Get("scope"), "carddav") || q.Get("access_type") != "offline" {
		t.Fatalf("authorization parameters: %v", q)
	}
	call := func(query string) *http.Response {
		resp, err := noFollow.Get(srv.URL + "/phe/oauth/callback?" + query) // an unsigned browser redirect
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if loc := call("code=good-code&state=forged").Header.Get("Location"); !strings.Contains(loc, "oauth=error") {
		t.Fatalf("a forged state: %s", loc)
	}
	if len(st.Accounts()) != 0 {
		t.Fatal("a forged callback created an account")
	}
	resp := call("code=good-code&state=" + state)
	if loc := resp.Header.Get("Location"); resp.StatusCode != 302 || loc != "/#/connections?oauth=ok" {
		t.Fatalf("the callback: %d %s", resp.StatusCode, loc)
	}
	accts := st.Accounts()
	if len(accts) != 2 || accts[0].Provider != "google" || !strings.HasPrefix(accts[0].Name, "Gmail ") {
		t.Fatalf("accounts: %+v", accts)
	}
	if loc := call("code=good-code&state=" + state).Header.Get("Location"); !strings.Contains(loc, "oauth=error") {
		t.Fatalf("a state is good once: %s", loc)
	}
	// the provider reports an error (the user pressed Cancel)
	_, r = admin.call(t, "POST", "/phe/oauth/start", map[string]any{"provider": "google", "kinds": []string{"mail"}})
	u, _ = url.Parse(r["url"].(string))
	if loc := call("error=access_denied&state=" + u.Query().Get("state")).Header.Get("Location"); !strings.Contains(loc, "access_denied") {
		t.Fatalf("provider error: %s", loc)
	}
	// a refused code is explained on the page, and creates nothing
	_, r = admin.call(t, "POST", "/phe/oauth/start", map[string]any{"provider": "google", "kinds": []string{"mail"}})
	u, _ = url.Parse(r["url"].(string))
	loc := call("code=bad&state=" + u.Query().Get("state")).Header.Get("Location")
	if !strings.Contains(loc, "oauth=error") || !strings.Contains(loc, "bad+code+or+redirect") || len(st.Accounts()) != 2 {
		t.Fatalf("refused code: %s (%d accounts)", loc, len(st.Accounts()))
	}
}
