// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gotd/log/logslog"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/proto"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgtest"
	"github.com/gotd/td/tgtest/cluster"
	"github.com/gotd/td/tgtest/services/config"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// A local Telegram (gotd's test cluster: the real protocol and encryption) answering the calls a signed-in device makes: who am I,
// my chats, the update state, resolving a username, sending; and pushing new messages as Telegram does.
type fakeTelegram struct {
	t       *testing.T
	c       *cluster.Cluster
	srv     *tgtest.Server
	d       *tgtest.Dispatcher
	mu      sync.Mutex
	session *tgtest.Session
	sent    []string
	unknown []string
	pts     int
	scanned bool // the QR code was scanned on the phone
}

// scan plays the phone accepting the QR code: Telegram tells the waiting device with updateLoginToken.
func (f *fakeTelegram) scan() {
	f.mu.Lock()
	f.scanned = true
	f.mu.Unlock()
	f.push(&tg.UpdateLoginToken{})
}

var (
	me   = &tg.User{ID: 100, AccessHash: 1, Self: true, FirstName: "Me", Username: "me_here"}
	bob  = &tg.User{ID: 200, AccessHash: 2, FirstName: "Bob", LastName: "B", Username: "BobbyB"}
	anon = &tg.User{ID: 201, AccessHash: 3, FirstName: "Anon"}
	team = &tg.Chat{ID: 300, Title: "Team", Photo: &tg.ChatPhotoEmpty{}}
	news = &tg.Channel{ID: 400, AccessHash: 4, Title: "News", Broadcast: true, Photo: &tg.ChatPhotoEmpty{}}
	sup  = &tg.Channel{ID: 500, AccessHash: 5, Title: "Big group", Megagroup: true, Photo: &tg.ChatPhotoEmpty{}}
)

func init() {
	for _, u := range []*tg.User{me, bob, anon} {
		u.SetFlags()
	}
	team.SetFlags()
	news.SetFlags()
	sup.SetFlags()
}

func (f *fakeTelegram) handle(id uint32, h func(req *tgtest.Request) (bin.Encoder, error)) {
	f.d.HandleFunc(id, func(s *tgtest.Server, req *tgtest.Request) error {
		f.mu.Lock()
		f.session = &req.Session
		f.mu.Unlock()
		res, err := h(req)
		var te *tgerr.Error
		if errors.As(err, &te) {
			return s.SendErr(req, te)
		}
		if err != nil {
			return err
		}
		return s.SendResult(req, res)
	})
}

func startTelegram(t *testing.T) *fakeTelegram {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f := &fakeTelegram{t: t, c: cluster.NewCluster(cluster.Options{}), pts: 1}
	srv, d := f.c.DC(2, "server")
	f.srv, f.d = srv, d
	config.NewService(&tg.Config{}, &tg.CDNConfig{}).Register(d)
	f.handle(tg.HelpGetConfigRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		return &tg.Config{DCOptions: f.c.List().Options, ThisDC: 2, Expires: int(time.Now().Add(time.Hour).Unix())}, nil // the cluster's own addresses
	})
	d.Fallback(tgtest.HandlerFunc(func(s *tgtest.Server, req *tgtest.Request) error {
		id, _ := req.Buf.PeekID()
		f.mu.Lock()
		f.unknown = append(f.unknown, fmt.Sprintf("%#x", id))
		f.mu.Unlock()
		return s.SendErr(req, tgerr.New(400, "NOT_IMPLEMENTED"))
	}))
	f.handle(tg.UsersGetUsersRequestTypeID, func(req *tgtest.Request) (bin.Encoder, error) {
		var r tg.UsersGetUsersRequest
		if err := r.Decode(req.Buf); err != nil {
			return nil, err
		}
		var out []tg.UserClass
		for _, in := range r.ID {
			switch u := in.(type) {
			case *tg.InputUserSelf:
				out = append(out, me)
			case *tg.InputUser:
				for _, known := range []*tg.User{me, bob, anon} {
					if known.ID == u.UserID {
						out = append(out, known)
					}
				}
			}
		}
		return &tg.UserClassVector{Elems: out}, nil
	})
	f.handle(tg.MessagesGetDialogsRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		return &tg.MessagesDialogs{Users: []tg.UserClass{bob, anon}, Chats: []tg.ChatClass{team, sup}}, nil
	})
	f.handle(tg.UpdatesGetStateRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return &tg.UpdatesState{Pts: f.pts, Date: int(time.Now().Unix()), Seq: 0}, nil
	})
	f.handle(tg.UpdatesGetDifferenceRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return &tg.UpdatesDifferenceEmpty{Date: int(time.Now().Unix()), Seq: 0}, nil
	})
	f.handle(tg.MessagesGetChatsRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		return &tg.MessagesChats{Chats: []tg.ChatClass{team}}, nil
	})
	f.handle(tg.ChannelsGetChannelsRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		return &tg.MessagesChats{Chats: []tg.ChatClass{sup}}, nil
	})
	f.handle(tg.UpdatesGetChannelDifferenceRequestTypeID, func(req *tgtest.Request) (bin.Encoder, error) {
		var r tg.UpdatesGetChannelDifferenceRequest
		if err := r.Decode(req.Buf); err != nil {
			return nil, err
		}
		return &tg.UpdatesChannelDifferenceEmpty{Final: true, Pts: r.Pts}, nil
	})
	f.handle(tg.ContactsResolveUsernameRequestTypeID, func(req *tgtest.Request) (bin.Encoder, error) {
		var r tg.ContactsResolveUsernameRequest
		if err := r.Decode(req.Buf); err != nil {
			return nil, err
		}
		if r.Username == "bobbyb" || r.Username == "BobbyB" {
			return &tg.ContactsResolvedPeer{Peer: &tg.PeerUser{UserID: bob.ID}, Users: []tg.UserClass{bob}}, nil
		}
		return nil, tgerr.New(400, "USERNAME_NOT_OCCUPIED")
	})
	f.handle(tg.ContactsResolvePhoneRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		return nil, tgerr.New(400, "PHONE_NOT_OCCUPIED")
	})
	f.handle(tg.MessagesSendMessageRequestTypeID, func(req *tgtest.Request) (bin.Encoder, error) {
		var r tg.MessagesSendMessageRequest
		if err := r.Decode(req.Buf); err != nil {
			return nil, err
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var to string
		switch p := r.Peer.(type) {
		case *tg.InputPeerUser:
			to = fmt.Sprintf("user %d (hash %d)", p.UserID, p.AccessHash)
		case *tg.InputPeerChat:
			to = fmt.Sprintf("chat %d", p.ChatID)
		case *tg.InputPeerChannel:
			to = fmt.Sprintf("channel %d (hash %d)", p.ChannelID, p.AccessHash)
		default:
			to = fmt.Sprintf("%T", p)
		}
		f.sent = append(f.sent, to+": "+r.Message)
		f.pts++
		return &tg.UpdateShortSentMessage{Out: true, ID: f.pts, Pts: f.pts, PtsCount: 1, Date: int(time.Now().Unix())}, nil
	})
	f.handle(tg.AuthExportLoginTokenRequestTypeID, func(*tgtest.Request) (bin.Encoder, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.scanned {
			return &tg.AuthLoginTokenSuccess{Authorization: &tg.AuthAuthorization{User: me}}, nil
		}
		return &tg.AuthLoginToken{Expires: int(time.Now().Add(30 * time.Second).Unix()), Token: []byte("login-token-1")}, nil
	})
	go f.c.Up(ctx)
	select {
	case <-f.c.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("the test cluster did not start")
	}
	return f
}

func (f *fakeTelegram) options(o *telegram.Options) {
	o.PublicKeys, o.DC, o.DCList, o.Resolver = f.c.Keys(), 2, f.c.List(), f.c.Resolver()
	o.SessionStorage = &session.StorageMemory{}
	o.RetryInterval = 100 * time.Millisecond
	if os.Getenv("TG_LOG") != "" {
		o.Logger = logslog.New(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}
}

// push sends new messages to the signed-in device, with their users and chats, as Telegram does.
func (f *fakeTelegram) push(ms ...tg.UpdateClass) {
	f.mu.Lock()
	k := *f.session
	f.mu.Unlock()
	if err := f.srv.Send(context.Background(), k, proto.MessageFromServer, &tg.Updates{
		Updates: ms, Users: []tg.UserClass{me, bob, anon}, Chats: []tg.ChatClass{team, news, sup}, Date: int(time.Now().Unix()),
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeTelegram) newMsg(from int64, peer tg.PeerClass, text string) tg.UpdateClass {
	f.mu.Lock()
	f.pts++
	pts := f.pts
	f.mu.Unlock()
	m := &tg.Message{ID: pts, FromID: &tg.PeerUser{UserID: from}, PeerID: peer, Date: int(time.Now().Unix()), Message: text}
	m.SetFlags()
	if _, ok := peer.(*tg.PeerChannel); ok {
		return &tg.UpdateNewChannelMessage{Message: m, Pts: pts, PtsCount: 1}
	}
	return &tg.UpdateNewMessage{Message: m, Pts: pts, PtsCount: 1}
}

func (f *fakeTelegram) wasSent(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.sent {
		if x == s {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; !ok(); i++ {
		if i > 200 {
			t.Fatal("timed out: " + what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func inbox(st *store.Store, peer, body string) *store.ChatMessage {
	for _, m := range st.ChatFor("me") {
		if m.Peer == peer && m.Body == body {
			return &m
		}
	}
	return nil
}

func TestAgainstALocalTelegram(t *testing.T) {
	f := startTelegram(t)
	dir := t.TempDir()
	os.WriteFile(sessionFile(dir, me.ID), []byte("{}"), 0o600) // signed in (the test cluster's session is kept in memory)
	st := store.OpenTest(t)
	st.AddSIPUser("me")
	hub := chat.NewHub(st)
	var mu sync.Mutex
	connected, lastErr := 0, error(nil)
	x := &Conn{Acc: "a", Name: "tg", Cfg: Cfg{APIID: telegram.TestAppID, UserID: me.ID}, Hash: telegram.TestAppHash, Dir: dir,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), opts: f.options,
		Status: func(err error) {
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				connected++
			}
		}}
	hub.Add(x)
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	defer x.Stop()
	for i := 0; ; i++ {
		mu.Lock()
		c, e := connected, lastErr
		mu.Unlock()
		if c > 0 {
			break
		}
		if i > 200 {
			f.mu.Lock()
			t.Fatalf("not connected: %v (calls the test cluster does not answer: %v)", e, f.unknown)
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // the update state is read

	f.push(f.newMsg(bob.ID, &tg.PeerUser{UserID: bob.ID}, "hello"))
	f.push(f.newMsg(anon.ID, &tg.PeerUser{UserID: anon.ID}, "no username"))
	f.push(f.newMsg(bob.ID, &tg.PeerChat{ChatID: team.ID}, "team?"))
	f.push(f.newMsg(bob.ID, &tg.PeerChannel{ChannelID: sup.ID}, "big group"))
	f.push(f.newMsg(0, &tg.PeerChannel{ChannelID: news.ID}, "breaking news"))
	waitFor(t, "the messages", func() bool { return inbox(st, "c500", "big group") != nil })
	if m := inbox(st, "bobbyb", "hello"); m == nil || m.PeerName != "Bob B" {
		t.Fatalf("bob's message: %+v (unknown calls %v)", m, f.unknown)
	}
	if inbox(st, "u201", "no username") == nil {
		t.Fatal("someone without a username")
	}
	if m := inbox(st, "g300", "team?"); m == nil || m.PeerName != "Bob B (Team)" {
		t.Fatalf("the group message: %+v", m)
	}
	if m := inbox(st, "c500", "big group"); m.PeerName != "Bob B (Big group)" {
		t.Fatalf("the supergroup message: %+v", m)
	}
	time.Sleep(200 * time.Millisecond)
	if inbox(st, "c400", "breaking news") != nil {
		t.Fatal("a broadcast channel reached the inbox")
	}
	t.Log("people (by username or ID), a group and a supergroup arrive; a broadcast channel does not")

	for to, want := range map[string]string{"bobbyb": "user 200 (hash 2)", "u201": "user 201 (hash 3)", "g300": "chat 300", "c500": "channel 500 (hash 5)"} {
		if err := x.Send(context.Background(), "me", to, "hi "+to, ""); err != nil {
			t.Fatalf("to %s: %v (unknown calls %v)", to, err, f.unknown)
		}
		if !f.wasSent(want + ": hi " + to) {
			t.Fatalf("to %s: sent %v", to, f.sent)
		}
	}
	for _, to := range []string{"nobody_here", "+306900000009", "u999"} {
		if err := x.Send(context.Background(), "me", to, "anyone?", ""); !errors.Is(err, chat.ErrNoRoute) {
			t.Fatalf("to %s: %v", to, err)
		}
	}
	t.Log("answers go to a username, a user ID, a group and a supergroup; unknown names, numbers and IDs are refused")
}

// The sign-in: a QR code (a PNG of tg://login?token=…) while waiting; once scanned on the phone, the session is kept, named after
// the user, and Test reads it back as signed in.
func TestQRSignIn(t *testing.T) {
	f := startTelegram(t)
	dir := t.TempDir()
	if _, err := NewLogin(dir, 0, ""); err == nil {
		t.Fatal("a sign-in without an API ID")
	}
	l, err := newLogin(dir, telegram.TestAppID, telegram.TestAppHash, f.options)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Cancel()
	waitFor(t, "the QR code", func() bool { return l.State().State == "waiting" })
	st := l.State()
	if len(st.PNG) < 100 || string(st.PNG[1:4]) != "PNG" {
		t.Fatalf("no QR code: %+v", st)
	}
	f.scan()
	waitFor(t, "signed in", func() bool { s := l.State(); return s.State == "done" || s.State == "failed" })
	if st := l.State(); st.State != "done" || st.UserID != me.ID || st.Name != "Me" {
		t.Fatalf("%+v", st)
	}
	if fi, err := os.Stat(sessionFile(dir, me.ID)); err != nil || fi.Mode().Perm() != 0o600 || fi.Size() < 100 {
		t.Fatalf("the session file: %v %v", fi, err)
	}
	if _, err := Test(context.Background(), dir, Cfg{APIID: telegram.TestAppID}, telegram.TestAppHash); err == nil {
		t.Fatal("Test without a signed-in user")
	}
	t.Log("QR code shown, scanned, session kept for the user")
}
