// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package telegram connects a Telegram account as a chat connection (docs/messaging.md) with gotd/td, a pure-Go client of Telegram's
// own API (MTProto): Cloud Companion signs in as one more device of your account, the way Telegram Desktop does (a QR code you scan
// on your phone), takes the messages of your chats and groups into the chat inbox and sends your answers back. It needs your own
// API ID and hash from https://my.telegram.org.
//
// Addresses: a username without "@" ("bob"), a number ("+306912345678", someone in your contacts), "u123" for a user by ID,
// "g123" for a group, "c123" for a supergroup. Numbers go out as SMS unless a rule says otherwise: give the account a prefix.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// Cfg is the account's configuration (store.Account.Config); the API hash is its secret.
type Cfg struct {
	APIID  int   `json:"api_id"`
	UserID int64 `json:"user_id"` // who signed in (the session file is named after it); set by the sign-in
}

// sessionFile is where the signed-in device's session (its key with Telegram) is kept.
func sessionFile(dir string, user int64) string {
	return filepath.Join(dir, "telegram-"+strconv.FormatInt(user, 10)+".session")
}

var device = telegram.DeviceConfig{DeviceModel: "Cloud Companion", SystemVersion: "Linux", AppVersion: "1.0"}

// Test checks that the signed-in session is there and still valid.
func Test(ctx context.Context, dir string, c Cfg, hash string) (string, error) {
	if c.APIID == 0 || hash == "" {
		return "", errors.New("the API ID and hash are needed (my.telegram.org → API development tools)")
	}
	if c.UserID == 0 {
		return "", errors.New("not signed in yet: use Sign in with a QR code first")
	}
	f := sessionFile(dir, c.UserID)
	if _, err := os.Stat(f); err != nil {
		return "", errors.New("not signed in yet: use Sign in with a QR code first")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cl := telegram.NewClient(c.APIID, hash, telegram.Options{SessionStorage: &session.FileStorage{Path: f}, NoUpdates: true, Device: device})
	var who string
	err := cl.Run(ctx, func(ctx context.Context) error {
		st, err := cl.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !st.Authorized {
			return errors.New("signed out: this device was removed in Telegram; sign in again")
		}
		who = name(st.User)
		if u := st.User.Username; u != "" {
			who += " (@" + u + ")"
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return "signed in as " + who, nil
}

func name(u *tg.User) string {
	n := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if n == "" {
		n = u.Username
	}
	if n == "" {
		n = "u" + strconv.FormatInt(u.ID, 10)
	}
	return n
}

// Conn is one Telegram account, a chat connection.
type Conn struct {
	Acc  string
	Name string
	Cfg  Cfg
	Hash string
	Dir  string
	Log  *slog.Logger
	// Status reports the connection for the account's row.
	Status func(error)

	hub    *chat.Hub
	opts   func(*telegram.Options) // tests: a local test cluster instead of Telegram
	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.Mutex
	pm    *peers.Manager
	api   *tg.Client
	ready bool
}

func ConnID(acc string) string { return "telegram:" + acc }

func (x *Conn) ID() string         { return ConnID(x.Acc) }
func (x *Conn) Label() string      { return x.Name + " (Telegram)" }
func (x *Conn) SetHub(h *chat.Hub) { x.hub = h }

// Start is the chat.Starter for "telegram" accounts; dir holds the sessions.
func Start(dir string, log *slog.Logger) chat.Starter {
	return func(a store.Account, hash string, status func(error)) (chat.Running, error) {
		var c Cfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return nil, errors.New("bad configuration")
		}
		x := &Conn{Acc: a.ID, Name: a.Name, Cfg: c, Hash: hash, Dir: dir, Log: log, Status: status}
		return x, x.Start()
	}
}

// Start connects in the background until Stop; the state shows on the account's row.
func (x *Conn) Start() error {
	if x.Cfg.APIID == 0 || x.Cfg.UserID == 0 || x.Hash == "" {
		return errors.New("not signed in yet")
	}
	if _, err := os.Stat(sessionFile(x.Dir, x.Cfg.UserID)); err != nil {
		return errors.New("not signed in yet: the session is missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	x.cancel, x.done = cancel, make(chan struct{})
	go x.run(ctx)
	return nil
}

// Stop disconnects (the device stays signed in).
func (x *Conn) Stop() {
	if x.cancel != nil {
		x.cancel()
		<-x.done
	}
}

func (x *Conn) run(ctx context.Context) {
	defer close(x.done)
	for ctx.Err() == nil {
		err := x.session(ctx)
		if ctx.Err() != nil {
			return
		}
		x.mu.Lock()
		x.ready = false
		x.mu.Unlock()
		x.Log.Warn("telegram: disconnected, connecting again", "account", x.Name, "err", err)
		if x.Status != nil {
			x.Status(fmt.Errorf("disconnected: %v", err))
		}
		if tgerr.Is(err, "AUTH_KEY_UNREGISTERED", "SESSION_REVOKED", "USER_DEACTIVATED") {
			return // signed out from the phone: only a new sign-in helps
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// session is one connection: the peers (for names and sending), the updates with their gaps filled, until it ends.
func (x *Conn) session(ctx context.Context) error {
	d := tg.NewUpdateDispatcher()
	var h telegram.UpdateHandler
	o := telegram.Options{
		SessionStorage: &session.FileStorage{Path: sessionFile(x.Dir, x.Cfg.UserID)},
		Device:         device,
		UpdateHandler:  telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error { return h.Handle(ctx, u) }),
	}
	if x.opts != nil {
		x.opts(&o)
	}
	cl := telegram.NewClient(x.Cfg.APIID, x.Hash, o)
	pm := peers.Options{}.Build(cl.API())
	gaps := updates.New(updates.Config{Handler: d, AccessHasher: pm})
	h = pm.UpdateHook(gaps)
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		x.take(e, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		x.take(e, u.Message)
		return nil
	})
	return cl.Run(ctx, func(ctx context.Context) error {
		st, err := cl.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !st.Authorized {
			return tgerr.New(401, "AUTH_KEY_UNREGISTERED")
		}
		if err := pm.Init(ctx); err != nil {
			return err
		}
		x.loadDialogs(ctx, cl.API(), pm)
		x.mu.Lock()
		x.pm, x.api, x.ready = pm, cl.API(), true
		x.mu.Unlock()
		if x.Status != nil {
			x.Status(nil)
		}
		// what arrived while disconnected is not replayed: the inbox gets what comes from now on
		return gaps.Run(ctx, cl.API(), st.User.ID, updates.AuthOptions{Forget: true})
	})
}

// loadDialogs tells the peers manager about your recent chats, so answers can go to people and groups by ID after a restart.
func (x *Conn) loadDialogs(ctx context.Context, api *tg.Client, pm *peers.Manager) {
	r, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100})
	if err != nil {
		x.Log.Warn("telegram: chats not read", "account", x.Name, "err", err)
		return
	}
	if m, ok := r.AsModified(); ok {
		pm.Apply(ctx, m.GetUsers(), m.GetChats())
	}
}

// ---- incoming

// take passes a new message on; your own messages and channels that only broadcast are left out.
func (x *Conn) take(e tg.Entities, mc tg.MessageClass) {
	cm, ok := toChat(e, mc)
	if !ok {
		return
	}
	cm.Conn = x.ID()
	if err := x.hub.In(cm); err != nil {
		x.Log.Warn("telegram: message not kept", "account", x.Name, "err", err)
	}
}

// toChat turns a Telegram message into an inbox message: from a person ("bob" or "u123"), from a group ("g123", named
// "Bob (Group)") or a supergroup ("c123").
func toChat(e tg.Entities, mc tg.MessageClass) (store.ChatMessage, bool) {
	m, ok := mc.(*tg.Message)
	if !ok || m.Out {
		return store.ChatMessage{}, false
	}
	body := m.Message
	if m.Media != nil {
		if what := media(m.Media); what != "" {
			body = strings.TrimSpace(body + " [" + what + "]")
		}
	}
	if body == "" {
		return store.ChatMessage{}, false
	}
	sender := func() (string, string) {
		if from, ok := m.FromID.(*tg.PeerUser); ok {
			if u := e.Users[from.UserID]; u != nil {
				return userAddr(u), name(u)
			}
			return "u" + strconv.FormatInt(from.UserID, 10), "u" + strconv.FormatInt(from.UserID, 10)
		}
		return "", ""
	}
	cm := store.ChatMessage{Body: body, TS: int64(m.Date)}
	switch p := m.PeerID.(type) {
	case *tg.PeerUser:
		u := e.Users[p.UserID]
		if u == nil {
			cm.Peer, cm.PeerName = "u"+strconv.FormatInt(p.UserID, 10), "u"+strconv.FormatInt(p.UserID, 10)
		} else {
			cm.Peer, cm.PeerName = userAddr(u), name(u)
		}
	case *tg.PeerChat:
		_, who := sender()
		title := "group"
		if c := e.Chats[p.ChatID]; c != nil {
			title = c.Title
		}
		cm.Peer, cm.PeerName = "g"+strconv.FormatInt(p.ChatID, 10), who+" ("+title+")"
	case *tg.PeerChannel:
		c := e.Channels[p.ChannelID]
		if c != nil && c.Broadcast {
			return store.ChatMessage{}, false // a channel that only broadcasts: news, not a chat
		}
		_, who := sender()
		title := "group"
		if c != nil {
			title = c.Title
		}
		if who == "" {
			who = title
		}
		cm.Peer, cm.PeerName = "c"+strconv.FormatInt(p.ChannelID, 10), who+" ("+title+")"
	default:
		return store.ChatMessage{}, false
	}
	return cm, true
}

func userAddr(u *tg.User) string {
	if u.Username != "" {
		return strings.ToLower(u.Username)
	}
	return "u" + strconv.FormatInt(u.ID, 10)
}

func media(mm tg.MessageMediaClass) string {
	switch m := mm.(type) {
	case *tg.MessageMediaPhoto:
		return "photo"
	case *tg.MessageMediaDocument:
		if d, ok := m.Document.(*tg.Document); ok {
			for _, a := range d.Attributes {
				switch at := a.(type) {
				case *tg.DocumentAttributeFilename:
					return "file: " + at.FileName
				case *tg.DocumentAttributeSticker:
					return "sticker"
				case *tg.DocumentAttributeAudio:
					if at.Voice {
						return "voice message"
					}
				}
			}
		}
		return "file"
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return "location"
	case *tg.MessageMediaContact:
		return "contact: " + strings.TrimSpace(m.FirstName+" "+m.LastName) + " " + m.PhoneNumber
	case *tg.MessageMediaPoll:
		return "poll"
	case *tg.MessageMediaWebPage:
		return "" // the link is in the text already
	}
	return "attachment"
}

// ---- outgoing

// target reads an address.
type target struct {
	kind byte // 'u' user ID, 'g' group, 'c' supergroup, 'p' phone, 'n' username
	id   int64
	s    string
}

func parse(peer string) (target, bool) {
	p := strings.TrimPrefix(strings.TrimSpace(peer), "@")
	if p == "" || strings.ContainsAny(p, " /") {
		return target{}, false
	}
	if strings.HasPrefix(p, "+") {
		return target{kind: 'p', s: p}, len(p) > 4
	}
	if len(p) > 1 && strings.ContainsRune("ugc", rune(p[0])) {
		if id, err := strconv.ParseInt(p[1:], 10, 64); err == nil && id > 0 {
			return target{kind: p[0], id: id}, true
		}
	}
	return target{kind: 'n', s: p}, len(p) >= 4 // Telegram usernames have at least 4 characters (5 for new ones)
}

// Send passes a message to a person or a group.
func (x *Conn) Send(ctx context.Context, _, peer, body, _ string) error {
	t, ok := parse(peer)
	if !ok {
		return chat.ErrNoRoute
	}
	x.mu.Lock()
	pm, api, ready := x.pm, x.api, x.ready
	x.mu.Unlock()
	if !ready {
		return fmt.Errorf("%w: not connected", chat.ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var in tg.InputPeerClass
	var err error
	switch t.kind {
	case 'u':
		var u peers.User
		if u, err = pm.ResolveUserID(ctx, t.id); err == nil {
			in = u.InputPeer()
		}
	case 'g':
		var c peers.Chat
		if c, err = pm.ResolveChatID(ctx, t.id); err == nil {
			in = c.InputPeer()
		}
	case 'c':
		var c peers.Channel
		if c, err = pm.ResolveChannelID(ctx, t.id); err == nil {
			in = c.InputPeer()
		}
	case 'p':
		var u peers.User
		if u, err = pm.ResolvePhone(ctx, t.s); err == nil {
			in = u.InputPeer()
		}
	default:
		var p peers.Peer
		if p, err = pm.Resolve(ctx, "@"+t.s); err == nil {
			in = p.InputPeer()
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
		}
		return chat.ErrNoRoute // nobody by that name, or not someone you have talked to
	}
	if _, err := message.NewSender(api).To(in).Text(ctx, body); err != nil {
		if tgerr.Is(err, "PEER_ID_INVALID", "USER_IS_BLOCKED", "CHAT_WRITE_FORBIDDEN", "INPUT_USER_DEACTIVATED", "USER_PRIVACY_RESTRICTED") {
			return chat.ErrNoRoute
		}
		return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
	}
	return nil
}
