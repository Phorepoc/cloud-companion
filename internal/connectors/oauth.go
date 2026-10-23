// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

// OAuth 2.0 sign-in for Gmail and Outlook with the OPERATOR's own client registrations (docs/connections.md, "Sign in with Google / Microsoft"):
// PHECLOUD_OAUTH_GOOGLE_ID / _SECRET and PHECLOUD_OAUTH_MS_ID / _SECRET (+ PHECLOUD_OAUTH_MS_TENANT, default "common"). Authorization code flow with
// PKCE; the tokens are sealed with the vault like passwords; IMAP and SMTP authenticate with XOAUTH2; Google CardDAV / CalDAV use a Bearer token.
// A stored credential that starts with "\x00xoauth2\x00" is an access token (a password cannot contain NUL): dialIMAP, Relay and the DAV client
// switch their login on that prefix.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
)

const tokenPrefix = "\x00xoauth2\x00"

// Token is what is kept (sealed) for an OAuth account.
type Token struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	Expiry  int64  `json:"expiry"` // unix seconds
	Email   string `json:"email,omitempty"`
	Scope   string `json:"scope,omitempty"` // Microsoft: one access token serves ONE resource, so each account renews its own with its own scopes
}

type Provider struct {
	ID, Label                 string
	AuthURL, TokenURL         string
	ClientID, Secret          string
	Extra                     url.Values // extra authorization parameters (offline access)
	MailScopes, ContactScopes []string
	CalendarScopes            []string
	IMAP, SMTP                ServerCfg
	CardDAVURL, CalDAVURL     func(email string) string // nil: not offered
}

func hostPort(env, def string) ServerCfg {
	v := os.Getenv(env)
	if v == "" {
		v = def
	}
	p := strings.Split(v, ":") // host:port:mode
	c := ServerCfg{Host: p[0], TLS: "tls"}
	if len(p) > 1 {
		c.Port, _ = strconv.Atoi(p[1])
	}
	if len(p) > 2 {
		c.TLS = p[2]
	}
	return c
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// Providers returns the providers the operator configured (client id and secret present). The endpoint variables are for tests and unusual setups.
func Providers() map[string]*Provider {
	out := map[string]*Provider{}
	if id := os.Getenv("PHECLOUD_OAUTH_GOOGLE_ID"); id != "" {
		p := &Provider{ID: "google", Label: "Google", ClientID: id, Secret: os.Getenv("PHECLOUD_OAUTH_GOOGLE_SECRET"),
			AuthURL:        envOr("PHECLOUD_OAUTH_GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/v2/auth"),
			TokenURL:       envOr("PHECLOUD_OAUTH_GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token"),
			Extra:          url.Values{"access_type": {"offline"}, "prompt": {"consent"}},
			MailScopes:     []string{"https://mail.google.com/"},
			ContactScopes:  []string{"https://www.googleapis.com/auth/carddav"},
			CalendarScopes: []string{"https://www.googleapis.com/auth/calendar"},
			IMAP:           hostPort("PHECLOUD_OAUTH_GOOGLE_IMAP", "imap.gmail.com:993:tls"),
			SMTP:           hostPort("PHECLOUD_OAUTH_GOOGLE_SMTP", "smtp.gmail.com:465:tls"),
			CardDAVURL: func(e string) string {
				return envOr("PHECLOUD_OAUTH_GOOGLE_CARDDAV", "https://www.googleapis.com/carddav/v1/principals/"+url.PathEscape(e)+"/lists/default/")
			},
			CalDAVURL: func(e string) string {
				return envOr("PHECLOUD_OAUTH_GOOGLE_CALDAV", "https://apidata.googleusercontent.com/caldav/v2/"+url.PathEscape(e)+"/events/")
			},
		}
		out[p.ID] = p
	}
	if id := os.Getenv("PHECLOUD_OAUTH_MS_ID"); id != "" {
		tenant := envOr("PHECLOUD_OAUTH_MS_TENANT", "common")
		p := &Provider{ID: "microsoft", Label: "Microsoft", ClientID: id, Secret: os.Getenv("PHECLOUD_OAUTH_MS_SECRET"),
			AuthURL:        envOr("PHECLOUD_OAUTH_MS_AUTH_URL", "https://login.microsoftonline.com/"+tenant+"/oauth2/v2.0/authorize"),
			TokenURL:       envOr("PHECLOUD_OAUTH_MS_TOKEN_URL", "https://login.microsoftonline.com/"+tenant+"/oauth2/v2.0/token"),
			MailScopes:     []string{"https://outlook.office.com/IMAP.AccessAsUser.All", "https://outlook.office.com/SMTP.Send", "offline_access"},
			ContactScopes:  []string{"https://graph.microsoft.com/Contacts.Read", "offline_access"},
			CalendarScopes: []string{"https://graph.microsoft.com/Calendars.Read", "offline_access"},
			CardDAVURL:     func(string) string { return GraphURL() + "/me/contacts" },
			CalDAVURL:      func(string) string { return GraphURL() + "/me/calendarView" },
			IMAP:           hostPort("PHECLOUD_OAUTH_MS_IMAP", "outlook.office365.com:993:tls"),
			SMTP:           hostPort("PHECLOUD_OAUTH_MS_SMTP", "smtp.office365.com:587:starttls"),
			// contacts and calendar of Outlook.com / Microsoft 365 are Microsoft Graph (JSON), not CardDAV / CalDAV: graph.go
		}
		out[p.ID] = p
	}
	return out
}

// Kinds lists what this provider can connect.
func (p *Provider) Kinds() []string {
	k := []string{"mail"}
	if p.CardDAVURL != nil {
		k = append(k, "contacts")
	}
	if p.CalDAVURL != nil {
		k = append(k, "calendar")
	}
	return k
}

func (p *Provider) scopes(kinds []string) []string {
	var s []string
	for _, k := range kinds {
		switch k {
		case "mail":
			s = append(s, p.MailScopes...)
		case "contacts":
			s = append(s, p.ContactScopes...)
		case "calendar":
			s = append(s, p.CalendarScopes...)
		}
	}
	return append(s, "openid", "email")
}

// NewPKCE returns a code verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string) {
	b := make([]byte, 32)
	rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(h[:])
}

// AuthCodeURL is where the browser is sent.
func (p *Provider) AuthCodeURL(redirect, state, challenge string, kinds []string) string {
	q := url.Values{"client_id": {p.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {strings.Join(p.scopes(kinds), " ")},
		"state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	for k, v := range p.Extra {
		q[k] = v
	}
	sep := "?"
	if strings.Contains(p.AuthURL, "?") {
		sep = "&"
	}
	return p.AuthURL + sep + q.Encode()
}

type tokenResp struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Expires int64  `json:"expires_in"`
	IDToken string `json:"id_token"`
	Error   string `json:"error"`
	Desc    string `json:"error_description"`
}

func (p *Provider) tokenCall(ctx context.Context, form url.Values) (tokenResp, error) {
	form.Set("client_id", p.ClientID)
	if p.Secret != "" {
		form.Set("client_secret", p.Secret)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", p.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return tokenResp{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var t tokenResp
	if err := json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("the provider answered HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 || t.Error != "" {
		d := t.Desc
		if d == "" {
			d = t.Error
		}
		return t, fmt.Errorf("the provider refused: %s", d)
	}
	return t, nil
}

// Exchange trades the authorization code for tokens (with the PKCE verifier).
func (p *Provider) Exchange(ctx context.Context, code, verifier, redirect string) (Token, error) {
	t, err := p.tokenCall(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}})
	if err != nil {
		return Token{}, err
	}
	if t.Refresh == "" {
		return Token{}, errors.New("the provider gave no refresh token (for Google the consent must be granted offline; try again)")
	}
	return Token{Access: t.Access, Refresh: t.Refresh, Expiry: time.Now().Unix() + t.Expires, Email: emailFromIDToken(t.IDToken)}, nil
}

// Refresh gets a new access token with the refresh token and updates tok in place.
func (p *Provider) Refresh(ctx context.Context, tok *Token) error {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.Refresh}}
	if tok.Scope != "" {
		form.Set("scope", tok.Scope)
	}
	t, err := p.tokenCall(ctx, form)
	if err != nil {
		return err
	}
	tok.Access, tok.Expiry = t.Access, time.Now().Unix()+t.Expires
	if t.Refresh != "" { // some providers rotate it
		tok.Refresh = t.Refresh
	}
	return nil
}

// emailFromIDToken reads the address out of the id_token that came straight from the token endpoint over TLS (OIDC allows trusting that
// channel instead of checking the signature).
func emailFromIDToken(j string) string {
	parts := strings.Split(j, ".")
	if len(parts) < 2 {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var c struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
	}
	json.Unmarshal(b, &c)
	if c.Email != "" {
		return c.Email
	}
	return c.PreferredUsername
}

// ---- XOAUTH2 ----

type xoauth2 struct{ user, token string }

func (x xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}
func (x xoauth2) Next([]byte) ([]byte, error) { return []byte{}, nil } // the server's error JSON: answer empty, the login then fails with its status

var _ sasl.Client = xoauth2{}

// smtpXOAuth2 is net/smtp's Auth for XOAUTH2.
type smtpXOAuth2 struct{ user, token string }

func (x smtpXOAuth2) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}
func (x smtpXOAuth2) Next(_ []byte, more bool) ([]byte, error) {
	if more { // the server's error JSON: answer empty, the login then fails with its status
		return []byte{}, nil
	}
	return nil, nil // the final success message needs no answer (a non-nil reply would be sent as an empty command)
}

// Cred wraps an access token as the "password" the connectors take.
func Cred(access string) string { return tokenPrefix + access }

func isCred(pass string) (string, bool) {
	if strings.HasPrefix(pass, tokenPrefix) {
		return strings.TrimPrefix(pass, tokenPrefix), true
	}
	return "", false
}
