// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package sipgw

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

func freePort(t *testing.T) int {
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.LocalAddr().(*net.UDPAddr).Port
}

// phone is a SIP device as the N9 is one: it registers with the gateway and receives MESSAGEs on its own port.
type phone struct {
	t     *testing.T
	login string
	pw    string
	port  int
	gw    string
	cli   *sipgo.Client
	got   chan *sip.Request
	reply int // what it answers to a MESSAGE (200 by default)
}

func newPhone(t *testing.T, ctx context.Context, gwAddr, login, pw string) *phone {
	p := &phone{t: t, login: login, pw: pw, port: freePort(t), gw: gwAddr, got: make(chan *sip.Request, 10), reply: 200}
	ua, _ := sipgo.NewUA(sipgo.WithUserAgent("test phone"))
	srv, _ := sipgo.NewServer(ua)
	srv.OnMessage(func(req *sip.Request, tx sip.ServerTransaction) {
		tx.Respond(sip.NewResponseFromRequest(req, p.reply, "x", nil))
		if p.reply < 300 {
			p.got <- req
		}
	})
	go srv.ListenAndServe(ctx, "udp", fmt.Sprintf("127.0.0.1:%d", p.port))
	p.cli, _ = sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"), sipgo.WithClientPort(p.port))
	t.Cleanup(func() { srv.Close(); p.cli.Close(); ua.Close() })
	time.Sleep(100 * time.Millisecond)
	return p
}

func (p *phone) uri(user string) sip.Uri {
	var u sip.Uri
	sip.ParseUri("sip:"+user+"@"+p.gw, &u)
	return u
}

// do sends a request, answering the gateway's challenge with the phone's password.
func (p *phone) do(req *sip.Request) *sip.Response {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := p.cli.Do(ctx, req)
	if err != nil {
		p.t.Fatal(err)
	}
	if res.StatusCode == 401 {
		res, err = p.cli.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{Username: p.login, Password: p.pw})
		if err != nil {
			p.t.Fatal(err)
		}
	}
	return res
}

func (p *phone) register(expires int) *sip.Response {
	req := sip.NewRequest(sip.REGISTER, p.uri(""))
	req.Recipient.User = ""
	req.AppendHeader(&sip.ToHeader{Address: p.uri(p.login)})
	req.AppendHeader(&sip.FromHeader{Address: p.uri(p.login), Params: params("tag", "t1")})
	var c sip.Uri
	sip.ParseUri(fmt.Sprintf("sip:%s@127.0.0.1:%d", p.login, p.port), &c)
	req.AppendHeader(&sip.ContactHeader{Address: c})
	req.AppendHeader(sip.NewHeader("Expires", fmt.Sprint(expires)))
	return p.do(req)
}

func (p *phone) message(to, body string) *sip.Response {
	req := sip.NewRequest(sip.MESSAGE, p.uri(to))
	req.AppendHeader(&sip.FromHeader{Address: p.uri(p.login), Params: params("tag", "t2")})
	req.AppendHeader(&sip.ToHeader{Address: p.uri(to)})
	req.AppendHeader(sip.NewHeader("Content-Type", "text/plain"))
	req.SetBody([]byte(body))
	return p.do(req)
}

func (p *phone) wait(what string) *sip.Request {
	p.t.Helper()
	select {
	case r := <-p.got:
		return r
	case <-time.After(5 * time.Second):
		p.t.Fatalf("%s: nothing arrived", what)
		return nil
	}
}

func (p *phone) none(what string) {
	p.t.Helper()
	select {
	case r := <-p.got:
		p.t.Fatalf("%s: unexpected %q", what, r.Body())
	case <-time.After(700 * time.Millisecond):
	}
}

func startGateway(t *testing.T) (*store.Store, *Gateway, string, context.Context) {
	st := store.OpenTest(t)
	hub := chat.NewHub(st)
	g, err := New(st, hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	g.Tick = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); g.Close() })
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	go g.Listen(ctx, addr)
	go g.Run(ctx)
	go hub.Run(ctx, 100*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	return st, g, addr, ctx
}

func TestRegisterNeedsTheRightPassword(t *testing.T) {
	st, _, addr, ctx := startGateway(t)
	pw, err := st.AddSIPUser("n9")
	if err != nil {
		t.Fatal(err)
	}
	if res := newPhone(t, ctx, addr, "n9", "wrong").register(600); res.StatusCode != 401 {
		t.Fatalf("a wrong password registered: %d", res.StatusCode)
	}
	if res := newPhone(t, ctx, addr, "n9", pw).register(600); res.StatusCode != 200 {
		t.Fatalf("the right password did not register: %d %s", res.StatusCode, res.Reason)
	}
	if res := newPhone(t, ctx, addr, "nobody", pw).register(600); res.StatusCode != 401 {
		t.Fatalf("an unknown login registered: %d", res.StatusCode)
	}
}

func TestLoginNames(t *testing.T) {
	st := store.OpenTest(t)
	for _, bad := range []string{"", "Has Space", "+4917155", "12345", strings.Repeat("a", 33)} {
		if _, err := st.AddSIPUser(bad); err == nil {
			t.Errorf("%q was accepted as a login", bad)
		}
	}
	if _, err := st.AddSIPUser("N9"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSIPUser("n9"); err == nil {
		t.Error("a second n9 was accepted")
	}
	if pw, _ := st.AddSIPUser("e7"); st.SIPUserHA1("e7") == "" || strings.Contains(st.SIPUserHA1("e7"), pw) {
		t.Error("the password is kept, or no HA1")
	}
}

func TestMessageToANumberGoesOutAsSMS(t *testing.T) {
	st, _, addr, ctx := startGateway(t)
	pw, _ := st.AddSIPUser("n9")
	p := newPhone(t, ctx, addr, "n9", pw)
	if res := p.message("+491715550100", "hi from the N9"); res.StatusCode != 480 {
		t.Fatalf("with no SMS phone: %d, want 480", res.StatusCode)
	}
	st.SMSUpload("pixel", []store.SMS{{DevMsgID: "1", Number: "+49 171 5550199", Dir: "in", Body: "old", TS: 1}})
	st.SMSUpload("fairphone", []store.SMS{{DevMsgID: "1", Number: "+49 171 5550100", Dir: "in", Body: "earlier", TS: 2}})
	st.SMSUpload("pixel", []store.SMS{{DevMsgID: "2", Number: "+49 171 5550199", Dir: "in", Body: "newer sync", TS: 3}})
	if res := p.message("+491715550100", "hi from the N9"); res.StatusCode != 202 {
		t.Fatalf("message to a number: %d %s", res.StatusCode, res.Reason)
	}
	// it goes through the phone that carries that conversation, not the one that synced last
	q := st.SMSPending("fairphone")
	if len(q) != 1 || q[0].Body != "hi from the N9" || q[0].Number != "+491715550100" {
		t.Fatalf("fairphone queue: %+v", q)
	}
	if len(st.SMSPending("pixel")) != 0 {
		t.Fatal("queued on the wrong phone")
	}
	if res := p.message("someone-else", "x"); res.StatusCode != 404 {
		t.Fatalf("unknown login: %d", res.StatusCode)
	}
}

func TestIncomingSMSReachesTheDeviceAndRepliesRouteBack(t *testing.T) {
	st, _, addr, ctx := startGateway(t)
	st.SMSUpload("pixel", []store.SMS{{DevMsgID: "0", Number: "+49 171 5550100", Dir: "in", Body: "before the login existed", TS: 1}})
	time.Sleep(400 * time.Millisecond) // the SMS connection has taken it into the inbox
	pw, _ := st.AddSIPUser("n9")
	p := newPhone(t, ctx, addr, "n9", pw)
	if res := p.register(600); res.StatusCode != 200 {
		t.Fatalf("register: %d", res.StatusCode)
	}
	p.none("SMS from before the login")
	st.SMSUpload("pixel", []store.SMS{
		{DevMsgID: "1", Number: "+49 171 5550100", Dir: "in", Body: "Are we still on for lunch?", TS: 10},
		{DevMsgID: "2", Number: "+49 171 5550100", Dir: "out", Body: "sent from the Pixel itself", TS: 11},
	})
	r := p.wait("incoming SMS")
	if string(r.Body()) != "Are we still on for lunch?" || r.From().Address.User != "+491715550100" || r.From().DisplayName != "+49 171 5550100" {
		t.Fatalf("got %q from %s", r.Body(), r.From().Value())
	}
	p.none("a message the Pixel sent itself")
	// answering it (to the From address) queues the SMS back through the same phone
	if res := p.message(r.From().Address.User, "Yes, 12:30"); res.StatusCode != 202 {
		t.Fatalf("reply: %d", res.StatusCode)
	}
	if q := st.SMSPending("pixel"); len(q) != 1 || q[0].Body != "Yes, 12:30" {
		t.Fatalf("reply not queued: %+v", q)
	}
}

func TestDeviceToDeviceWaitsUntilRegistered(t *testing.T) {
	st, _, addr, ctx := startGateway(t)
	pa, _ := st.AddSIPUser("n9")
	pb, _ := st.AddSIPUser("e7")
	a := newPhone(t, ctx, addr, "n9", pa)
	if res := a.message("e7", "are you there?"); res.StatusCode != 202 {
		t.Fatalf("to e7: %d", res.StatusCode)
	}
	b := newPhone(t, ctx, addr, "e7", pb)
	b.reply = 480 // the device refuses at first: the message must stay
	b.register(600)
	time.Sleep(600 * time.Millisecond)
	if len(st.ChatFor("e7")) != 1 {
		t.Fatal("a refused message was dropped")
	}
	b.reply = 200
	r := b.wait("message from n9")
	if string(r.Body()) != "are you there?" || r.From().Address.User != "n9" {
		t.Fatalf("got %q from %s", r.Body(), r.From().Value())
	}
	for i := 0; len(st.ChatFor("e7")) != 0; i++ { // the gateway drops it once the device's 200 is in
		if i == 20 {
			t.Fatal("a delivered message stayed queued")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// unregistered: nothing is delivered any more
	b.register(0)
	a.message("e7", "later")
	b.none("after unregistering")
}

func TestOnlyTextAndNoEmptyMessages(t *testing.T) {
	st, _, addr, ctx := startGateway(t)
	pw, _ := st.AddSIPUser("n9")
	st.AddSIPUser("e7")
	p := newPhone(t, ctx, addr, "n9", pw)
	if res := p.message("e7", "   "); res.StatusCode != 400 {
		t.Fatalf("empty: %d", res.StatusCode)
	}
	req := sip.NewRequest(sip.MESSAGE, p.uri("e7"))
	req.AppendHeader(&sip.FromHeader{Address: p.uri("n9"), Params: params("tag", "t3")})
	req.AppendHeader(&sip.ToHeader{Address: p.uri("e7")})
	req.AppendHeader(sip.NewHeader("Content-Type", "application/im-iscomposing+xml"))
	req.SetBody([]byte("<x/>"))
	if res := p.do(req); res.StatusCode != 415 {
		t.Fatalf("non-text: %d", res.StatusCode)
	}
}

func TestRegisterOnlyYourOwnLogin(t *testing.T) {
	st, _, addr, ctx := startGateway(t)
	pw, _ := st.AddSIPUser("n9")
	st.AddSIPUser("e7")
	p := newPhone(t, ctx, addr, "n9", pw)
	req := sip.NewRequest(sip.REGISTER, p.uri(""))
	req.AppendHeader(&sip.ToHeader{Address: p.uri("e7")})
	req.AppendHeader(&sip.FromHeader{Address: p.uri("e7"), Params: params("tag", "t4")})
	var c sip.Uri
	sip.ParseUri(fmt.Sprintf("sip:e7@127.0.0.1:%d", p.port), &c)
	req.AppendHeader(&sip.ContactHeader{Address: c})
	if res := p.do(req); res.StatusCode != 403 {
		t.Fatalf("n9 registered as e7: %d", res.StatusCode)
	}
}
