// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// "Sign in with Google / Microsoft" for the Connections page (docs/connections.md). The operator registers their own OAuth client and gives its id and
// secret in PHECLOUD_OAUTH_GOOGLE_ID/_SECRET and PHECLOUD_OAUTH_MS_ID/_SECRET; only configured providers are offered.
//
//	GET  /phe/oauth/providers   (admin, signed)  [{id, label, kinds}]
//	POST /phe/oauth/start       (admin, signed)  {provider, kinds, name} -> {url}   the browser is sent to the provider
//	GET  /phe/oauth/callback    the provider's redirect: ?code&state. No signature here (a redirect cannot carry one): the one-use random `state`
//	                            made by /start, kept on the server for 10 minutes with the PKCE verifier, is the proof.
//
// The redirect URI is <public origin>/phe/oauth/callback: the origin the browser used (behind a proxy: from the trusted proxy's headers, see
// proxy.go), or PHECLOUD_PUBLIC_URL. Register exactly that address with the provider.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/connectors"
)

type oauthState struct {
	provider, name, redirect, verifier string
	kinds                              []string
	exp                                time.Time
}

type oauthAPI struct {
	a  *auth
	m  *connectors.Manager
	mu sync.Mutex
	st map[string]*oauthState
}

func (o *oauthAPI) redirectURI(r *http.Request) string {
	if v := strings.TrimRight(os.Getenv("PHECLOUD_PUBLIC_URL"), "/"); v != "" {
		return v + "/phe/oauth/callback"
	}
	in := infoOf(r)
	return in.Scheme + "://" + in.Host + "/phe/oauth/callback"
}

func (o *oauthAPI) routes(mux *http.ServeMux) {
	mux.HandleFunc("/phe/oauth/providers", func(w http.ResponseWriter, r *http.Request) {
		if !o.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		out := []map[string]any{}
		for _, id := range []string{"google", "microsoft"} {
			if p := connectors.Providers()[id]; p != nil {
				out = append(out, map[string]any{"id": p.ID, "label": p.Label, "kinds": p.Kinds()})
			}
		}
		reply(w, 200, map[string]any{"providers": out, "redirect": o.redirectURI(r)})
	})
	mux.HandleFunc("/phe/oauth/start", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Provider, Name string
			Kinds          []string
		}
		if r.Method != http.MethodPost || !o.a.adminDevice(r) || !readJSON(w, r, &in) {
			if r.Method == http.MethodPost && !o.a.adminDevice(r) {
				bad(w, 403, "sign in as the owner")
			}
			return
		}
		p := connectors.Providers()[in.Provider]
		if p == nil {
			bad(w, 404, "that sign-in is not set up on this server")
			return
		}
		if len(in.Kinds) == 0 {
			bad(w, 400, "choose what to connect")
			return
		}
		b := make([]byte, 24)
		rand.Read(b)
		state := hex.EncodeToString(b)
		verifier, challenge := connectors.NewPKCE()
		redirect := o.redirectURI(r)
		o.mu.Lock()
		for k, v := range o.st {
			if time.Now().After(v.exp) {
				delete(o.st, k)
			}
		}
		o.st[state] = &oauthState{provider: in.Provider, name: strings.TrimSpace(in.Name), redirect: redirect, verifier: verifier, kinds: in.Kinds, exp: time.Now().Add(10 * time.Minute)}
		o.mu.Unlock()
		reply(w, 200, map[string]any{"url": p.AuthCodeURL(redirect, state, challenge, in.Kinds)})
	})
	mux.HandleFunc("/phe/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		fail := func(msg string) {
			http.Redirect(w, r, "/#/connections?oauth=error&msg="+url.QueryEscape(msg), http.StatusFound)
		}
		q := r.URL.Query()
		o.mu.Lock()
		s := o.st[q.Get("state")]
		delete(o.st, q.Get("state")) // one use
		o.mu.Unlock()
		if s == nil || time.Now().After(s.exp) {
			fail("The sign-in expired or was not started here. Start it again from the Connections page.")
			return
		}
		if e := q.Get("error"); e != "" {
			fail("The provider said: " + e)
			return
		}
		p := connectors.Providers()[s.provider]
		if p == nil || q.Get("code") == "" {
			fail("The provider sent no authorization code.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		tok, err := p.Exchange(ctx, q.Get("code"), s.verifier, s.redirect)
		if err != nil {
			fail(err.Error())
			return
		}
		accts, err := o.m.AddOAuth(p, s.kinds, s.name, tok)
		if err != nil {
			fail(err.Error())
			return
		}
		for _, a := range accts {
			a := a
			go func() {
				bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				o.m.SyncNow(bg, a.ID)
			}()
		}
		http.Redirect(w, r, "/#/connections?oauth=ok", http.StatusFound)
	})
}
