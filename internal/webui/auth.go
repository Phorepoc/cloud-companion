// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

// Owner sign-in for the web app: passkeys (WebAuthn) with a recovery code as the fallback.
//
//	GET  /passkey/status                  {owner, passkeys}
//	POST /passkey/register/begin {code}   an unused ADMIN pairing code (from `phecloud paircode --admin`) allows registering a passkey
//	POST /passkey/register/finish?sid&name   (body: the credential)  -> {dev, key, recovery}   recovery only for the first passkey
//	POST /passkey/login/begin
//	POST /passkey/login/finish?sid&name   (body: the assertion)       -> {dev, key}
//	POST /passkey/recover {code, name}    a recovery code (one use, replaced by a new one) -> {dev, key, recovery}
//
// A successful sign-in creates an admin web "device" (id + key, as a pairing would) which the app keeps and uses for the usual
// frame connection: device pairing is unchanged, this is one more way to obtain a device. WebAuthn needs a secure origin
// (https, or http://localhost) and a host name as the relying party id (not an IP address); PHECLOUD_RP_ID and
// PHECLOUD_RP_ORIGINS (comma separated) override what is derived from the request.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/phorepoc/cloud-companion/internal/store"
)

type owner struct {
	handle []byte
	creds  []webauthn.Credential
}

func (o *owner) WebAuthnID() []byte                         { return o.handle }
func (o *owner) WebAuthnName() string                       { return "owner" }
func (o *owner) WebAuthnDisplayName() string                { return "Cloud Companion owner" }
func (o *owner) WebAuthnCredentials() []webauthn.Credential { return o.creds }
func (o *owner) WebAuthnIcon() string                       { return "" }

type pending struct {
	authed bool // started by a signed-in admin device (no pairing code)
	data   *webauthn.SessionData
	handle []byte
	code   string
	exp    time.Time
}

type auth struct {
	st   *store.Store
	mu   sync.Mutex
	sess map[string]*pending
	fail map[string][]time.Time
	used map[string]time.Time // v2 signatures seen in the last two minutes: each is good for one request
}

func newAuth(st *store.Store) *auth {
	return &auth{st: st, sess: map[string]*pending{}, fail: map[string][]time.Time{}, used: map[string]time.Time{}}
}

func (a *auth) wa(r *http.Request) (*webauthn.WebAuthn, error) {
	in := infoOf(r) // the public host and scheme (from a trusted proxy's headers, else the request itself)
	host := in.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	rpid, origins := strings.Trim(host, "[]"), []string{in.Scheme + "://" + in.Host}
	if v := os.Getenv("PHECLOUD_RP_ID"); v != "" {
		rpid = v
	}
	if v := os.Getenv("PHECLOUD_RP_ORIGINS"); v != "" {
		origins = strings.Split(v, ",")
	}
	return webauthn.New(&webauthn.Config{RPDisplayName: "Cloud Companion", RPID: rpid, RPOrigins: origins})
}

func (a *auth) keep(p *pending) string {
	b := make([]byte, 16)
	rand.Read(b)
	sid := hex.EncodeToString(b)
	p.exp = time.Now().Add(5 * time.Minute)
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range a.sess {
		if time.Now().After(v.exp) {
			delete(a.sess, k)
		}
	}
	if len(a.sess) >= maxPending { // anyone may start a sign-in: the waiting ones are capped, not left to fill the memory
		return ""
	}
	a.sess[sid] = p
	return sid
}

// maxPending is how many passkey sign-ins may wait at once (each lasts 5 minutes).
const maxPending = 1000

func (a *auth) take(sid string) *pending {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.sess[sid]
	delete(a.sess, sid)
	if p == nil || time.Now().After(p.exp) {
		return nil
	}
	return p
}

// limited: at most 8 failed attempts per client address per minute
func (a *auth) limited(r *http.Request) bool {
	ip := infoOf(r).ClientIP
	a.mu.Lock()
	defer a.mu.Unlock()
	var keep []time.Time
	for _, t := range a.fail[ip] {
		if time.Since(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(a.fail, ip) // addresses that stop trying are not kept for ever
	} else {
		a.fail[ip] = keep
	}
	return len(keep) >= 8
}

// starts: at most 20 passkey sign-ins started per client address per minute (each one is kept for 5 minutes)
func (a *auth) starts(r *http.Request) bool {
	key := "start " + infoOf(r).ClientIP
	a.mu.Lock()
	defer a.mu.Unlock()
	var keep []time.Time
	for _, t := range a.fail[key] {
		if time.Since(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 20 {
		a.fail[key] = keep
		return false
	}
	a.fail[key] = append(keep, time.Now())
	return true
}

func (a *auth) failed(r *http.Request) {
	ip := infoOf(r).ClientIP
	a.mu.Lock()
	a.fail[ip] = append(a.fail[ip], time.Now())
	a.mu.Unlock()
}

// adminDevice checks that the request is signed (v2, see signedDevice) by an admin device.
func (a *auth) adminDevice(r *http.Request) bool {
	_, admin, v2, ok := a.signed(r)
	return ok && admin && v2
}

// signedDevice verifies the X-Phe-Auth header of any paired device and returns its id and whether it is an admin.
//
// v2 (the web app): "v2.<dev>.<unix ts>.<nonce>.<body>.<hex HMAC-SHA256(device key, v2.dev.ts.nonce.body.METHOD path?query)>",
// body being the hex SHA-256 of the request body, or "u" for a multipart upload (not read twice). It is good once, within a
// minute: a header seen on the network cannot be sent again, nor with another body or query. Admin routes take only v2.
//
// v1 (the phones, for their own routes): "<dev>.<unix ts>.<hex HMAC-SHA256(device key, dev.ts.METHOD path)>", within a minute.
func (a *auth) signedDevice(r *http.Request) (id string, admin, ok bool) {
	id, admin, _, ok = a.signed(r)
	return id, admin, ok
}

type bodyKey struct{}

// keepSignedBody keeps a copy of the body of a v2-signed request (up to 1 MiB) before any handler reads it, so the signature
// check sees the body whether a handler checks first and reads after, or reads first.
func keepSignedBody(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := strings.Split(r.Header.Get("X-Phe-Auth"), "."); len(p) == 6 && p[0] == "v2" && p[4] != "u" {
			b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
			r.Body.Close()
			if err != nil || len(b) > 1<<20 {
				bad(w, 413, "The request is too large.")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
			r = r.WithContext(context.WithValue(r.Context(), bodyKey{}, b))
		}
		h.ServeHTTP(w, r)
	})
}

func (a *auth) signed(r *http.Request) (id string, admin, v2, ok bool) {
	parts := strings.Split(r.Header.Get("X-Phe-Auth"), ".")
	v2 = len(parts) == 6 && parts[0] == "v2"
	if !v2 && len(parts) != 3 {
		return "", false, false, false
	}
	if v2 {
		parts = parts[1:]
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)).Abs() > time.Minute {
		return "", false, false, false
	}
	key, adm, err := a.st.Key(parts[0])
	if err != nil {
		return "", false, false, false
	}
	m := hmac.New(sha256.New, key)
	if !v2 {
		m.Write([]byte(parts[0] + "." + parts[1] + "." + r.Method + " " + r.URL.Path))
	} else {
		nonce, body := parts[2], parts[3]
		if len(nonce) < 16 || len(nonce) > 64 {
			return "", false, false, false
		}
		if body == "u" {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				return "", false, false, false
			}
		} else {
			b, kept := r.Context().Value(bodyKey{}).([]byte)
			if !kept { // not through Handler (a test): read it here, for the handler to read again
				var err error
				if b, err = io.ReadAll(io.LimitReader(r.Body, 1<<20+1)); err != nil || len(b) > 1<<20 {
					return "", false, false, false
				}
				r.Body.Close()
				r.Body = io.NopCloser(bytes.NewReader(b))
			}
			h := sha256.Sum256(b)
			if !hmac.Equal([]byte(hex.EncodeToString(h[:])), []byte(body)) {
				return "", false, false, false
			}
		}
		target := r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		m.Write([]byte("v2." + parts[0] + "." + parts[1] + "." + nonce + "." + body + "." + r.Method + " " + target))
	}
	sig := parts[len(parts)-1]
	if !hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(sig)) {
		return "", false, false, false
	}
	if v2 && !a.once(sig) {
		return "", false, false, false
	}
	return parts[0], adm, v2, true
}

// once records a v2 signature and says whether it was new. Signatures older than two minutes are past their timestamp check
// anyway and are dropped; the map is capped so a flood of valid requests cannot grow it without end.
func (a *auth) once(sig string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if _, seen := a.used[sig]; seen {
		return false
	}
	if len(a.used)%1024 == 1023 || len(a.used) >= 100000 {
		for k, t := range a.used {
			if now.Sub(t) > 2*time.Minute {
				delete(a.used, k)
			}
		}
		if len(a.used)%1024 == 1023 || len(a.used) >= 100000 {
			return false
		}
	}
	a.used[sig] = now
	return true
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func bad(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

func (a *auth) device(w http.ResponseWriter, name string, extra map[string]any) {
	if name = strings.TrimSpace(name); name == "" || len(name) > 40 {
		name = "Web browser"
	}
	id, key, err := a.st.NewDevice(name, true)
	if err != nil {
		bad(w, 500, "could not create a device")
		return
	}
	out := map[string]any{"dev": id, "key": hex.EncodeToString(key)}
	for k, v := range extra {
		out[k] = v
	}
	reply(w, 200, out)
}

func (a *auth) routes(mux *http.ServeMux) {
	mux.HandleFunc("/passkey/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"owner": a.st.OwnerExists(), "passkeys": len(a.st.Passkeys())})
	})
	mux.HandleFunc("/passkey/register/begin", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code string }
		if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			bad(w, 400, "bad request")
			return
		}
		if a.limited(r) {
			bad(w, 429, "too many attempts, wait a minute")
			return
		}
		code := strings.ToUpper(strings.TrimSpace(in.Code))
		authed := a.adminDevice(r)
		if !authed && !a.st.CheckAdminCode(code) && !a.st.CheckFirstCode(code) {
			a.failed(r)
			bad(w, 403, "that code did not work: use the first-start code from the server log (`docker logs phe-cloud`), or an unused admin code from `phecloud paircode --admin`")
			return
		}
		wa, err := a.wa(r)
		if err != nil {
			bad(w, 500, err.Error())
			return
		}
		o := &owner{creds: a.st.Passkeys()}
		if h, err := a.st.OwnerHandle(); err == nil {
			o.handle = h
		} else {
			o.handle = store.NewHandle()
		}
		var excl []protocol.CredentialDescriptor
		for _, c := range o.creds {
			excl = append(excl, c.Descriptor())
		}
		opts, data, err := wa.BeginRegistration(o,
			webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementPreferred, UserVerification: protocol.VerificationPreferred}),
			webauthn.WithExclusions(excl))
		if err != nil {
			bad(w, 500, err.Error())
			return
		}
		sid := a.keep(&pending{data: data, handle: o.handle, code: code, authed: authed})
		if sid == "" {
			bad(w, 503, "Too many sign-ins at once. Try again in a few minutes.")
			return
		}
		reply(w, 200, map[string]any{"sid": sid, "options": opts})
	})
	mux.HandleFunc("/passkey/register/finish", func(w http.ResponseWriter, r *http.Request) {
		p := a.take(r.URL.Query().Get("sid"))
		if r.Method != http.MethodPost || p == nil {
			bad(w, 400, "the registration expired, start again")
			return
		}
		wa, err := a.wa(r)
		if err != nil {
			bad(w, 500, err.Error())
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		cred, err := wa.FinishRegistration(&owner{handle: p.handle, creds: a.st.Passkeys()}, *p.data, r)
		if err != nil {
			a.failed(r)
			bad(w, 400, "the passkey could not be verified")
			return
		}
		if p.authed {
			if !a.adminDevice(r) {
				bad(w, 403, "sign in again")
				return
			}
		} else if err := a.st.ConsumeAdminCode(p.code); err != nil && a.st.ConsumeFirstCode(p.code) != nil {
			a.failed(r)
			bad(w, 403, "the code was used or expired")
			return
		}
		rec, err := a.st.AddPasskey(p.handle, cred, r.URL.Query().Get("name"))
		if err != nil {
			bad(w, 500, "could not store the passkey")
			return
		}
		if p.authed { // a signed-in admin adds a passkey: no new device, no new recovery code
			reply(w, 200, map[string]any{"ok": true, "recovery": rec})
			return
		}
		a.device(w, "Web browser", map[string]any{"recovery": rec})
	})
	mux.HandleFunc("/passkey/list", func(w http.ResponseWriter, r *http.Request) {
		if !a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		reply(w, 200, map[string]any{"passkeys": a.st.PasskeyList(), "owner": a.st.OwnerExists()})
	})
	mux.HandleFunc("/passkey/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		if r.Method != http.MethodPost || !a.adminDevice(r) || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			bad(w, 403, "sign in as the owner")
			return
		}
		if err := a.st.RemovePasskey(in.ID); err != nil {
			bad(w, 404, err.Error())
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/passkey/recovery", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		code, err := a.st.RegenerateRecovery()
		if err != nil {
			bad(w, 409, "there is no owner passkey yet")
			return
		}
		reply(w, 200, map[string]any{"recovery": code})
	})
	mux.HandleFunc("/passkey/login/begin", func(w http.ResponseWriter, r *http.Request) {
		if !a.starts(r) {
			bad(w, 429, "Too many sign-in attempts. Wait a minute and try again.")
			return
		}
		h, err := a.st.OwnerHandle()
		if err != nil {
			bad(w, 404, "no passkey has been registered yet")
			return
		}
		wa, err := a.wa(r)
		if err != nil {
			bad(w, 500, err.Error())
			return
		}
		opts, data, err := wa.BeginLogin(&owner{handle: h, creds: a.st.Passkeys()}, webauthn.WithUserVerification(protocol.VerificationPreferred))
		if err != nil {
			bad(w, 500, err.Error())
			return
		}
		sid := a.keep(&pending{data: data, handle: h})
		if sid == "" {
			bad(w, 503, "Too many sign-ins at once. Try again in a few minutes.")
			return
		}
		reply(w, 200, map[string]any{"sid": sid, "options": opts})
	})
	mux.HandleFunc("/passkey/login/finish", func(w http.ResponseWriter, r *http.Request) {
		p := a.take(r.URL.Query().Get("sid"))
		if r.Method != http.MethodPost || p == nil {
			bad(w, 400, "the sign-in expired, start again")
			return
		}
		if a.limited(r) {
			bad(w, 429, "too many attempts, wait a minute")
			return
		}
		wa, err := a.wa(r)
		if err != nil {
			bad(w, 500, err.Error())
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		cred, err := wa.FinishLogin(&owner{handle: p.handle, creds: a.st.Passkeys()}, *p.data, r)
		if err != nil {
			a.failed(r)
			bad(w, 401, "the passkey was not accepted")
			return
		}
		a.st.PasskeyUsed(cred)
		a.device(w, r.URL.Query().Get("name"), nil)
	})
	mux.HandleFunc("/passkey/recover", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code, Name string }
		if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			bad(w, 400, "bad request")
			return
		}
		if a.limited(r) {
			bad(w, 429, "too many attempts, wait a minute")
			return
		}
		rec, err := a.st.UseRecoveryCode(in.Code)
		if err != nil {
			a.failed(r)
			bad(w, 403, "that recovery code did not work")
			return
		}
		a.device(w, in.Name, map[string]any{"recovery": rec})
	})
}
