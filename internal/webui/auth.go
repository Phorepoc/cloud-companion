// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
}

func newAuth(st *store.Store) *auth {
	return &auth{st: st, sess: map[string]*pending{}, fail: map[string][]time.Time{}}
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
	a.sess[sid] = p
	return sid
}

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
	a.fail[ip] = keep
	return len(keep) >= 8
}

func (a *auth) failed(r *http.Request) {
	ip := infoOf(r).ClientIP
	a.mu.Lock()
	a.fail[ip] = append(a.fail[ip], time.Now())
	a.mu.Unlock()
}

// adminDevice checks the X-Phe-Auth header: "<dev>.<unix ts>.<hex HMAC-SHA256(device key, dev.ts.METHOD path)>" of an admin device, at most a
// minute old. The key never leaves the browser; the signature binds the request line.
func (a *auth) adminDevice(r *http.Request) bool {
	_, admin, ok := a.signedDevice(r)
	return ok && admin
}

// signedDevice verifies the X-Phe-Auth header of any paired device and returns its id and whether it is an admin.
func (a *auth) signedDevice(r *http.Request) (id string, admin, ok bool) {
	parts := strings.Split(r.Header.Get("X-Phe-Auth"), ".")
	if len(parts) != 3 {
		return "", false, false
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)).Abs() > time.Minute {
		return "", false, false
	}
	key, adm, err := a.st.Key(parts[0])
	if err != nil {
		return "", false, false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(parts[0] + "." + parts[1] + "." + r.Method + " " + r.URL.Path))
	if !hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(parts[2])) {
		return "", false, false
	}
	return parts[0], adm, true
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
		if !authed && !a.st.CheckAdminCode(code) {
			a.failed(r)
			bad(w, 403, "that code did not work: it must be an unused admin code from `phecloud paircode --admin`")
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
		reply(w, 200, map[string]any{"sid": a.keep(&pending{data: data, handle: o.handle, code: code, authed: authed}), "options": opts})
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
		} else if err := a.st.ConsumeAdminCode(p.code); err != nil {
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
		reply(w, 200, map[string]any{"sid": a.keep(&pending{data: data, handle: h}), "options": opts})
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
