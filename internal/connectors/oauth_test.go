// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/phorepoc/phecloud/internal/store"
)

// fakeOAuth is a token endpoint: authorization_code -> tok1/ref1, refresh_token -> tokN (N counts refreshes), or a refusal.
type fakeOAuth struct {
	mu        sync.Mutex
	refreshes int
	refuse    bool
	noRefresh bool
	verifier  string
}

func (f *fakeOAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ParseForm()
	w.Header().Set("Content-Type", "application/json")
	if r.PostForm.Get("client_id") != "cid" || r.PostForm.Get("client_secret") != "csecret" {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":"invalid_client"}`)
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		if r.PostForm.Get("code") != "the-code" || r.PostForm.Get("code_verifier") == "" || r.PostForm.Get("redirect_uri") == "" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_grant","error_description":"bad code"}`)
			return
		}
		f.verifier = r.PostForm.Get("code_verifier")
		idt := "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"me@gmail.test"}`)) + ".y"
		ref := "ref1"
		if f.noRefresh {
			ref = ""
		}
		fmt.Fprintf(w, `{"access_token":"tok1","refresh_token":%q,"expires_in":3600,"id_token":%q}`, ref, idt)
	case "refresh_token":
		if f.refuse || r.PostForm.Get("refresh_token") != "ref1" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
			return
		}
		f.refreshes++
		fmt.Fprintf(w, `{"access_token":"tok%d","expires_in":3600}`, f.refreshes+1)
	default:
		w.WriteHeader(400)
	}
}

// xserver is the server half of XOAUTH2 for the fake IMAP and SMTP servers: the token must be `want()`; then login(user) runs.
type xserver struct {
	want  func() string
	login func(user string) error
}

func (x *xserver) Next(resp []byte) ([]byte, bool, error) {
	var user, tok string
	for _, p := range strings.Split(string(resp), "\x01") {
		if strings.HasPrefix(p, "user=") {
			user = strings.TrimPrefix(p, "user=")
		}
		if strings.HasPrefix(p, "auth=Bearer ") {
			tok = strings.TrimPrefix(p, "auth=Bearer ")
		}
	}
	if tok == "" || tok != x.want() {
		return nil, false, fmt.Errorf("bad token")
	}
	return nil, true, x.login(user)
}

type xsess struct {
	imapserver.Session
	want func() string
}

func (s *xsess) AuthenticateMechanisms() []string { return []string{"XOAUTH2"} }
func (s *xsess) Authenticate(string) (sasl.Server, error) {
	return &xserver{want: s.want, login: func(string) error { return s.Session.Login("me", "secret") }}, nil
}

// startXIMAP is an IMAP server that accepts XOAUTH2 with the token returned by want().
func startXIMAP(t *testing.T, want func() string) (ServerCfg, *imapmemserver.User) {
	t.Helper()
	mem := imapmemserver.New()
	u := imapmemserver.NewUser("me", "secret")
	u.Create("INBOX", nil)
	u.Create("Sent", &imap.CreateOptions{SpecialUse: []imap.MailboxAttr{imap.MailboxAttrSent}})
	mem.AddUser(u)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &xsess{mem.NewSession(), want}, nil, nil
		},
		InsecureAuth: true,
	})
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ServerCfg{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLS: "plain", User: "me@gmail.test"}, u
}

func TestOAuthExchangeRefreshXOAuth2(t *testing.T) {
	fo := &fakeOAuth{}
	srv := httptest.NewServer(fo)
	defer srv.Close()
	p := &Provider{ID: "google", Label: "Google", ClientID: "cid", Secret: "csecret", TokenURL: srv.URL, AuthURL: srv.URL + "/auth",
		MailScopes: []string{"https://mail.google.com/"}}
	ctx := context.Background()

	// the authorization URL carries PKCE, the state, the redirect and the scopes
	v, ch := NewPKCE()
	u := p.AuthCodeURL("https://cloud.example.org/phe/oauth/callback", "st8", ch, []string{"mail"})
	for _, want := range []string{"client_id=cid", "state=st8", "code_challenge=" + ch, "code_challenge_method=S256", "redirect_uri=https%3A%2F%2Fcloud.example.org%2Fphe%2Foauth%2Fcallback", "mail.google.com", "openid"} {
		if !strings.Contains(u, want) {
			t.Fatalf("authorization URL lacks %q: %s", want, u)
		}
	}
	tok, err := p.Exchange(ctx, "the-code", v, "https://cloud.example.org/phe/oauth/callback")
	if err != nil || tok.Access != "tok1" || tok.Refresh != "ref1" || tok.Email != "me@gmail.test" {
		t.Fatalf("exchange: %+v %v", tok, err)
	}
	if fo.verifier != v {
		t.Fatal("the PKCE verifier was not sent")
	}
	if _, err := p.Exchange(ctx, "wrong", v, "x"); err == nil || !strings.Contains(err.Error(), "bad code") {
		t.Fatalf("a refused code must say why: %v", err)
	}
	fo.noRefresh = true
	if _, err := p.Exchange(ctx, "the-code", v, "x"); err == nil || !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("no refresh token: %v", err)
	}
	fo.noRefresh = false
	tok.Expiry = 0
	if err := p.Refresh(ctx, &tok); err != nil || tok.Access != "tok2" || tok.Refresh != "ref1" || tok.Expiry < time.Now().Unix() {
		t.Fatalf("refresh: %+v %v", tok, err)
	}

	// IMAP with XOAUTH2: the right token logs in, a wrong one is refused
	cur := "tok2"
	cfg, u2 := startXIMAP(t, func() string { return cur })
	raw := msg("o1", "via oauth")
	u2.Append("INBOX", lit{bytes.NewReader(raw)}, &imap.AppendOptions{Time: time.Now()})
	if fl, err := TestIMAP(cfg, Cred("tok2")); err != nil || len(fl) < 2 {
		t.Fatalf("XOAUTH2 login: %v %v", fl, err)
	}
	if _, err := TestIMAP(cfg, Cred("stolen")); err == nil {
		t.Fatal("a wrong access token was accepted")
	}
	sink, seen := &fakeSink{}, &memSeen{map[string]string{}}
	if n, err := SyncIMAP(ctx, "acc", "Gmail", MailCfg{IMAP: cfg, Months: 3}, Cred("tok2"), sink, seen, 10); err != nil || n != 1 {
		t.Fatalf("sync with a token: %d %v", n, err)
	}
}

func TestSMTPXOAuth2(t *testing.T) {
	icfg, _ := startXIMAP(t, func() string { return "tok-smtp" })
	be := &smtpBackend{}
	srv := smtp.NewServer(be)
	srv.AllowInsecureAuth = true
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(ln)
	defer srv.Close()
	xbe := &xsmtp{smtpBackend: be}
	srv.Backend = xbe
	sc := ServerCfg{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLS: "plain", User: "me@gmail.test"}
	if err := TestSMTP(sc, Cred("tok-smtp")); err != nil {
		t.Fatal("smtp xoauth2:", err)
	}
	if TestSMTP(sc, Cred("nope")) == nil {
		t.Fatal("a wrong SMTP token was accepted")
	}
	mc := MailCfg{IMAP: icfg, SMTP: sc, From: "me@gmail.test"}
	if err := Relay(mc, Cred("tok-smtp"), "me@gmail.test", []string{"you@example.net"}, BuildMessage("me@gmail.test", []string{"you@example.net"}, "OAuth", "hi")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(be.data), "Subject: OAuth") {
		t.Fatalf("the relayed message: %q", be.data)
	}
}

type xsmtp struct{ *smtpBackend }

func (x *xsmtp) NewSession(*smtp.Conn) (smtp.Session, error) {
	return &xsmtpSession{smtpSession{b: x.smtpBackend}}, nil
}

type xsmtpSession struct{ smtpSession }

func (s *xsmtpSession) AuthMechanisms() []string { return []string{"XOAUTH2"} }
func (s *xsmtpSession) Auth(string) (sasl.Server, error) {
	return &xserver{want: func() string { return "tok-smtp" }, login: func(string) error { return nil }}, nil
}

// The manager: accounts from a sign-in, tokens sealed, refreshed when they are about to expire, a revoked grant explained.
func TestManagerOAuth(t *testing.T) {
	fo := &fakeOAuth{}
	srv := httptest.NewServer(fo)
	defer srv.Close()
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_ID", "cid")
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_SECRET", "csecret")
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_TOKEN_URL", srv.URL)
	cur := "tok1"
	icfg, _ := startXIMAP(t, func() string { return cur })
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_IMAP", fmt.Sprintf("127.0.0.1:%d:plain", icfg.Port))
	t.Setenv("PHECLOUD_OAUTH_GOOGLE_SMTP", "127.0.0.1:1:plain")
	p := Providers()["google"]
	if p == nil {
		t.Fatal("the configured provider is missing")
	}
	if got := strings.Join(p.Kinds(), ","); got != "mail,contacts,calendar" {
		t.Fatalf("Google kinds: %s", got)
	}
	if ms := func() *Provider { t.Setenv("PHECLOUD_OAUTH_MS_ID", "m"); return Providers()["microsoft"] }(); ms == nil || strings.Join(ms.Kinds(), ",") != "mail" {
		t.Fatalf("Microsoft offers mail only: %+v", ms)
	}
	st := store.OpenTest(t)
	v, err := OpenVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(st, nil, v)
	vr, _ := NewPKCE()
	tok, err := p.Exchange(context.Background(), "the-code", vr, "https://x/cb")
	if err != nil {
		t.Fatal(err)
	}
	accts, err := m.AddOAuth(p, []string{"mail", "contacts", "calendar"}, "", tok)
	if err != nil || len(accts) != 3 {
		t.Fatalf("accounts: %v %v", accts, err)
	}
	kinds := map[string]string{}
	for _, a := range accts {
		kinds[a.Kind] = a.Config
		if bytes.Contains(a.Secret, []byte("tok1")) || bytes.Contains(a.Secret, []byte("ref1")) {
			t.Fatal("the token is stored in the clear")
		}
	}
	if !strings.Contains(kinds["carddav"], "googleapis.com/carddav/v1/principals/me@gmail.test") || !strings.Contains(kinds["caldav"], "apidata.googleusercontent.com/caldav/v2/me@gmail.test/events") || !strings.Contains(kinds["imap"], `"oauth":"google"`) {
		t.Fatalf("configs: %v", kinds)
	}
	var mail store.Account
	for _, a := range accts {
		if a.Kind == "imap" {
			mail = a
		}
	}
	// a fresh token is used as is; the login works through XOAUTH2
	c1, err := m.secret(mail)
	if err != nil || c1 != Cred("tok1") || fo.refreshes != 0 {
		t.Fatalf("fresh token: %q %v refreshes=%d", c1, err, fo.refreshes)
	}
	var mc MailCfg
	json.Unmarshal([]byte(mail.Config), &mc)
	if _, err := TestIMAP(mc.IMAP, c1); err != nil {
		t.Fatal("login with the account's own configuration:", err)
	}
	// the token is about to expire: it is refreshed, re-sealed, and the next call does not refresh again
	expired, _ := json.Marshal(Token{Access: "tok1", Refresh: "ref1", Expiry: time.Now().Unix() + 10, Email: "me@gmail.test"})
	st.SetAccountSecret(mail.ID, v.Seal(expired))
	mail, _ = st.Account(mail.ID)
	cur = "tok2"
	c2, err := m.secret(mail)
	if err != nil || c2 != Cred("tok2") || fo.refreshes != 1 {
		t.Fatalf("refresh: %q %v refreshes=%d", c2, err, fo.refreshes)
	}
	mail, _ = st.Account(mail.ID)
	if c3, _ := m.secret(mail); c3 != Cred("tok2") || fo.refreshes != 1 {
		t.Fatalf("the refreshed token was not kept (%q, %d refreshes)", c3, fo.refreshes)
	}
	if _, err := TestIMAP(mc.IMAP, c2); err != nil {
		t.Fatal("login with the refreshed token:", err)
	}
	// the user revokes the grant at the provider: the error says what to do
	st.SetAccountSecret(mail.ID, v.Seal(expired))
	mail, _ = st.Account(mail.ID)
	fo.refuse = true
	if _, err := m.secret(mail); err == nil || !strings.Contains(err.Error(), "sign in again") || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revoked grant: %v", err)
	}
}

// Google CardDAV / CalDAV: the same sync code with a Bearer token instead of Basic.
func TestDAVBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok2" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(207)
		fmt.Fprint(w, "<multistatus xmlns=\"DAV:\"><response><href>/book/</href><propstat><prop><resourcetype><collection/></resourcetype></prop></propstat></response></multistatus>")
	}))
	defer srv.Close()
	cfg := DAVCfg{URL: srv.URL + "/book/", User: "me@gmail.test", OAuth: "google"}
	if _, err := TestDAV(context.Background(), cfg, Cred("tok2")); err != nil {
		t.Fatal("bearer:", err)
	}
	if _, err := TestDAV(context.Background(), cfg, Cred("old")); err == nil {
		t.Fatal("an old token was accepted")
	}
}
