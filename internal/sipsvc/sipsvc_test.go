// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package sipsvc

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// fakeService plays a SIP service (an Asterisk): digest logins, a registrar that delivers to where the REGISTER came from (like
// Asterisk's rewrite_contact), and a proxy that keeps the MESSAGEs it is given.
type fakeService struct {
	addr string
	user string
	pass string
	cli  *sipgo.Client

	mu     sync.Mutex
	source string // where the registered account is reached
	regs   int
	got    []string
}

func md5hex(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }

func (f *fakeService) authOK(req *sip.Request, tx sip.ServerTransaction, code int) bool {
	hn, chn := "Authorization", "WWW-Authenticate"
	if code == 407 {
		hn, chn = "Proxy-Authorization", "Proxy-Authenticate"
	}
	if h := req.GetHeader(hn); h != nil {
		a := map[string]string{}
		for _, part := range strings.Split(strings.TrimPrefix(h.Value(), "Digest "), ",") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
			a[k] = strings.Trim(v, `"`)
		}
		ha1 := md5hex(f.user + ":test:" + f.pass)
		ha2 := md5hex(string(req.Method) + ":" + a["uri"])
		want := md5hex(ha1 + ":" + a["nonce"] + ":" + ha2)
		if a["qop"] != "" {
			want = md5hex(ha1 + ":" + a["nonce"] + ":" + a["nc"] + ":" + a["cnonce"] + ":" + a["qop"] + ":" + ha2)
		}
		if a["username"] == f.user && a["response"] == want {
			return true
		}
		tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		return false
	}
	res := sip.NewResponseFromRequest(req, code, "Auth", nil)
	res.AppendHeader(sip.NewHeader(chn, `Digest realm="test", nonce="n0nce", algorithm=MD5`))
	tx.Respond(res)
	return false
}

func startFake(t *testing.T) *fakeService {
	f := &fakeService{user: "cc", pass: "secret", addr: freeAddr(t)}
	ua, _ := sipgo.NewUA()
	srv, _ := sipgo.NewServer(ua)
	f.cli, _ = sipgo.NewClient(ua)
	srv.OnRegister(func(req *sip.Request, tx sip.ServerTransaction) {
		if !f.authOK(req, tx, 401) {
			return
		}
		f.mu.Lock()
		f.regs++
		if req.Contact() != nil {
			f.source = req.Source()
			if h := req.GetHeader("Expires"); h != nil && h.Value() == "0" {
				f.source = ""
			}
		}
		f.mu.Unlock()
		tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnMessage(func(req *sip.Request, tx sip.ServerTransaction) {
		if !f.authOK(req, tx, 407) {
			return
		}
		if req.Recipient.User == "nobody" {
			tx.Respond(sip.NewResponseFromRequest(req, 404, "Not Found", nil))
			return
		}
		f.mu.Lock()
		f.got = append(f.got, req.Recipient.User+"@"+req.Recipient.Host+":"+string(req.Body()))
		f.mu.Unlock()
		tx.Respond(sip.NewResponseFromRequest(req, 202, "Accepted", nil))
	})
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	go srv.ListenAndServe(context.WithValue(ctx, sipgo.ListenReadyCtxKey, sipgo.ListenReadyCtxValue(ready)), "udp", f.addr)
	<-ready
	t.Cleanup(func() { cancel(); srv.Close(); ua.Close() })
	return f
}

// deliver sends a MESSAGE from someone on the service to the registered account.
func (f *fakeService) deliver(t *testing.T, fromUser, name, body string) int {
	f.mu.Lock()
	dst := f.source
	f.mu.Unlock()
	if dst == "" {
		t.Fatal("nothing registered")
	}
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{Scheme: "sip", User: "cc", Host: "test.local"})
	req.AppendHeader(&sip.FromHeader{DisplayName: name, Address: sip.Uri{Scheme: "sip", User: fromUser, Host: "test.local"}, Params: params("tag", "x1")})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: "cc", Host: "test.local"}})
	req.AppendHeader(sip.NewHeader("Content-Type", "text/plain"))
	req.SetBody([]byte(body))
	req.SetDestination(dst)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := f.cli.Do(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode
}

func freeAddr(t *testing.T) string {
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; !ok(); i++ {
		if i > 100 {
			t.Fatal("timed out: " + what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func plain(a store.Account) (string, error) { return string(a.Secret), nil }

func TestLoginCheck(t *testing.T) {
	f := startFake(t)
	if _, err := Test(context.Background(), Cfg{Server: f.addr, Domain: "test.local", User: "cc"}, "wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("a wrong password: %v", err)
	}
	d, err := Test(context.Background(), Cfg{Server: f.addr, Domain: "test.local", User: "cc"}, "secret")
	if err != nil || !strings.Contains(d, "cc@test.local") {
		t.Fatalf("the right password: %q %v", d, err)
	}
	if f.source != "" {
		t.Fatal("the check registered something")
	}
	if _, err := Test(context.Background(), Cfg{User: "cc"}, "x"); err == nil {
		t.Fatal("no server accepted")
	}
}

func TestMessagesBothWays(t *testing.T) {
	f := startFake(t)
	st := store.OpenTest(t)
	hub := chat.NewHub(st)
	st.AddSIPUser("n9")
	s := &Service{Acc: "a1", Name: "Home PBX", Cfg: Cfg{Server: f.addr, Domain: "test.local", User: "cc", Listen: freeAddr(t)}, Pass: "secret", Hub: hub, Log: quiet}
	var status error = errors.New("not yet")
	var smu sync.Mutex
	s.Status = func(err error) { smu.Lock(); status = err; smu.Unlock() }
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	hub.Add(s)
	waitFor(t, "registered", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.source != "" })
	waitFor(t, "status ok", func() bool { smu.Lock(); defer smu.Unlock(); return status == nil })

	// from the service to your devices, answerable through the rule
	st.SetChatRoutes([]store.ChatRoute{{Kind: "prefix", Value: "pbx.", Conn: s.ID()}})
	if code := f.deliver(t, "201", "Alice", "hello from the desk phone"); code != 200 {
		t.Fatalf("delivery answered %d", code)
	}
	m := st.ChatFor("n9")
	if len(m) != 1 || m[0].Conn != "sipsvc:a1" || m[0].Peer != "201" || m[0].PeerName != "Alice" || m[0].Body != "hello from the desk phone" {
		t.Fatalf("inbox: %+v", m)
	}
	if a := hub.Address(m[0].Conn, m[0].Peer); a != "pbx.201" {
		t.Fatalf("reply address %q", a)
	}

	// from your devices through the service
	if err := hub.Send(context.Background(), "n9", "pbx.201", "hi Alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.Send(context.Background(), "n9", "pbx.bob@example.org", "hi Bob"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := append([]string(nil), f.got...)
	f.mu.Unlock()
	if len(got) != 2 || got[0] != "201@test.local:hi Alice" || got[1] != "bob@example.org:hi Bob" {
		t.Fatalf("the service got %v", got)
	}
	if err := hub.Send(context.Background(), "n9", "pbx.nobody", "x"); !errors.Is(err, chat.ErrNoRoute) {
		t.Fatalf("unknown extension: %v", err)
	}

	// only the service delivers
	if s.fromService("192.0.2.7:5060") || !s.fromService("127.0.0.1:5060") {
		t.Fatal("the source check")
	}

	s.Stop()
	waitFor(t, "unregistered", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.source == "" })
}

func TestWrongPasswordShowsOnTheAccount(t *testing.T) {
	f := startFake(t)
	st := store.OpenTest(t)
	a, _ := st.AddAccount(store.Account{Kind: "sip", Name: "PBX", Config: `{"server":"` + f.addr + `","domain":"test.local","user":"cc","listen":"` + freeAddr(t) + `"}`, Secret: []byte("wrong")})
	p := &chat.Accounts{St: st, Hub: chat.NewHub(st), Log: quiet, Secret: plain, Kinds: map[string]chat.Starter{"sip": Start(quiet)}}
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx, time.Hour)
	defer cancel()
	waitFor(t, "error status", func() bool {
		x, _ := st.Account(a.ID)
		return x.Status == "error" && strings.Contains(x.LastError, "login refused")
	})
}

func TestConnectionFollowsTheAccount(t *testing.T) {
	f := startFake(t)
	st := store.OpenTest(t)
	hub := chat.NewHub(st)
	p := &chat.Accounts{St: st, Hub: hub, Log: quiet, Secret: plain, Kinds: map[string]chat.Starter{"sip": Start(quiet)}}
	a, err := st.AddAccount(store.Account{Kind: "sip", Name: "PBX", Config: `{"server":"` + f.addr + `","domain":"test.local","user":"cc","listen":"` + freeAddr(t) + `"}`, Secret: []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	p.Sync()
	has := func() bool {
		for _, c := range hub.Conns() {
			if c.ID == ConnID(a.ID) && c.Label == "PBX (SIP)" {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Fatalf("not a connection: %+v", hub.Conns())
	}
	waitFor(t, "account ok", func() bool { x, _ := st.Account(a.ID); return x.Status == "ok" })
	p.Sync() // unchanged: keeps running, no second listener
	if !has() {
		t.Fatal("dropped on a second sync")
	}
	st.RemoveAccount(a.ID)
	p.Sync()
	if has() {
		t.Fatal("still a connection after the account was removed")
	}
	waitFor(t, "unregistered", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.source == "" })
}
