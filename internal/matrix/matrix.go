// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package matrix connects a Matrix account as a chat connection (docs/messaging.md): Cloud Companion signs in as its own device of
// your account, end-to-end encryption included (mautrix-go, its pure-Go olm: build with -tags goolm), takes the messages of your
// direct chats and rooms into the chat inbox and sends your answers back to them.
//
// Addresses are written so a SIP phone can type them: a user on your own homeserver is just the name ("bob"), anyone else is
// "bob=example.org" (":" is not allowed in a SIP user part); a room is "!roomid=example.org". The web app may also use the plain
// Matrix forms (@bob:example.org, !room:example.org, #alias:example.org).
package matrix

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Cfg is the account's configuration (store.Account.Config); the password is its secret.
type Cfg struct {
	Homeserver string `json:"homeserver"` // https://matrix.example.org
	User       string `json:"user"`       // @you:example.org, or the name alone on that homeserver
}

// norm checks the settings and returns who to sign in as: the full ID, or the name alone (the homeserver then says who that is, which
// also covers a homeserver whose address is not its name, such as matrix.example.org for example.org).
func (c *Cfg) norm() (string, error) {
	c.Homeserver, c.User = strings.TrimRight(strings.TrimSpace(c.Homeserver), "/"), strings.TrimSpace(c.User)
	if c.Homeserver == "" || c.User == "" {
		return "", errors.New("the homeserver and the user name are needed")
	}
	if !strings.Contains(c.Homeserver, "://") {
		c.Homeserver = "https://" + c.Homeserver
	}
	if strings.HasPrefix(c.User, "@") && strings.Contains(c.User, ":") {
		return c.User, nil
	}
	return strings.TrimPrefix(c.User, "@"), nil
}

func login(u, pass string) *mautrix.ReqLogin {
	return &mautrix.ReqLogin{
		Type:                     mautrix.AuthTypePassword,
		Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: u},
		Password:                 pass,
		InitialDeviceDisplayName: "Cloud Companion",
		StoreCredentials:         true,
	}
}

// Test checks the login (the Connections page's Test button) and signs that test device out again.
func Test(ctx context.Context, c Cfg, pass string) (string, error) {
	u, err := c.norm()
	if err != nil {
		return "", err
	}
	cli, err := mautrix.NewClient(c.Homeserver, "", "")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r := login(u, pass)
	r.InitialDeviceDisplayName = "Cloud Companion (login check)"
	resp, err := cli.Login(ctx, r)
	if err != nil {
		if errors.Is(err, mautrix.MForbidden) {
			return "", errors.New("login refused: check the user name and password")
		}
		return "", fmt.Errorf("no answer from %s: %w", c.Homeserver, err)
	}
	cli.Logout(ctx)
	return "login works as " + resp.UserID.String(), nil
}

// Conn is one Matrix account, a chat connection.
type Conn struct {
	Acc  string
	Name string
	Cfg  Cfg
	Pass string
	Dir  string // where its encryption keys are kept (one SQLite file per account)
	Log  *slog.Logger
	// Status reports the sign-in and the sync for the account's row.
	Status func(error)
	// Trace, when set, gets mautrix's own debug log (tests).
	Trace io.Writer

	hub    *chat.Hub
	me     id.UserID
	cli    *mautrix.Client
	crypto *cryptohelper.CryptoHelper
	db     *sql.DB
	start  time.Time
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

func ConnID(acc string) string { return "matrix:" + acc }

func (m *Conn) ID() string              { return ConnID(m.Acc) }
func (m *Conn) Label() string           { return m.Name + " (Matrix)" }
func (m *Conn) SetHub(h *chat.Hub)      { m.hub = h }
func (m *Conn) Me() id.UserID           { return m.me }
func (m *Conn) Client() *mautrix.Client { return m.cli }

// Start is the chat.Starter for "matrix" accounts; dir holds the keys.
func Start(dir string, log *slog.Logger) chat.Starter {
	return func(a store.Account, pass string, status func(error)) (chat.Running, error) {
		var c Cfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return nil, errors.New("bad configuration")
		}
		m := &Conn{Acc: a.ID, Name: a.Name, Cfg: c, Pass: pass, Dir: dir, Log: log, Status: status}
		return m, m.Start()
	}
}

// pickleKey is the account's key for its stored encryption keys, made once and kept next to them.
func (m *Conn) pickleKey() ([]byte, error) {
	p := filepath.Join(m.Dir, "matrix-"+m.Acc+".key")
	if b, err := os.ReadFile(p); err == nil && len(b) == 32 {
		return b, nil
	}
	b := make([]byte, 32)
	rand.Read(b)
	return b, os.WriteFile(p, b, 0o600)
}

// Start signs in (as the same device each time, so the keys stay valid), sets up encryption and syncs until Stop.
func (m *Conn) Start() error {
	u, err := m.Cfg.norm()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	key, err := m.pickleKey()
	if err != nil {
		return err
	}
	m.db, err = sql.Open("sqlite", "file:"+filepath.Join(m.Dir, "matrix-"+m.Acc+".db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return err
	}
	m.db.SetMaxOpenConns(1)
	db, err := dbutil.NewWithDB(m.db, "sqlite3")
	if err != nil {
		return err
	}
	if m.cli, err = mautrix.NewClient(m.Cfg.Homeserver, "", ""); err != nil {
		return err
	}
	m.cli.Log = zerolog.Nop()
	if m.Trace != nil {
		m.cli.Log = zerolog.New(zerolog.ConsoleWriter{Out: m.Trace, NoColor: true}).With().Str("acc", m.Name).Logger().Level(zerolog.DebugLevel)
	}
	if m.crypto, err = cryptohelper.NewCryptoHelper(m.cli, key, db); err != nil {
		return err
	}
	m.crypto.LoginAs = login(u, m.Pass)
	m.crypto.DecryptErrorCallback = func(evt *event.Event, err error) {
		m.Log.Warn("matrix: a message could not be decrypted", "account", m.Name, "room", evt.RoomID, "err", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.crypto.Init(ctx); err != nil {
		m.db.Close()
		if errors.Is(err, mautrix.MForbidden) {
			return errors.New("login refused: check the user name and password")
		}
		return fmt.Errorf("could not sign in to %s: %w", m.Cfg.Homeserver, err)
	}
	m.cli.Crypto = m.crypto
	m.me = m.cli.UserID
	sy := m.cli.Syncer.(*mautrix.DefaultSyncer)
	sy.OnEvent(m.cli.StateStoreSyncHandler)
	sy.OnEventType(event.StateMember, m.onMember)
	sy.OnEventType(event.EventMessage, m.onMessage)
	sy.OnSync(m.cli.DontProcessOldEvents) // what came before this account was connected is not replayed
	m.start = time.Now()
	sctx, scancel := context.WithCancel(context.Background())
	m.cancel, m.done = scancel, make(chan struct{})
	go m.sync(sctx)
	return nil
}

// sync runs the long poll, reporting the state and trying again after an error.
func (m *Conn) sync(ctx context.Context) {
	defer close(m.done)
	for ctx.Err() == nil {
		if m.Status != nil {
			m.Status(nil)
		}
		err := m.cli.SyncWithContext(ctx)
		if ctx.Err() != nil {
			return
		}
		if m.Status != nil && err != nil {
			m.Status(fmt.Errorf("sync: %w", err))
		}
		m.Log.Warn("matrix: sync stopped, retrying", "account", m.Name, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}

// Stop ends the sync and closes the key store (the device stays signed in, to keep its keys).
func (m *Conn) Stop() {
	if m.cancel != nil {
		m.cancel()
		<-m.done
	}
	if m.crypto != nil {
		m.crypto.Close()
	}
	if m.db != nil {
		m.db.Close()
	}
}

// members are the room's joined and invited users, loaded from the homeserver when not known yet (after a join, sync alone does not
// bring the whole list, and encryption needs it: keys go to every member's devices).
func (m *Conn) members(ctx context.Context, room id.RoomID) []id.UserID {
	ms, _ := m.cli.StateStore.GetRoomJoinedOrInvitedMembers(ctx, room)
	if len(ms) < 2 {
		if _, err := m.cli.Members(ctx, room); err == nil {
			ms, _ = m.cli.StateStore.GetRoomJoinedOrInvitedMembers(ctx, room)
		}
	}
	return ms
}

// onMember joins the rooms you are invited to, so people can start a chat with you.
func (m *Conn) onMember(ctx context.Context, evt *event.Event) {
	mem := evt.Content.AsMember()
	if evt.GetStateKey() != m.me.String() || mem.Membership != event.MembershipInvite {
		return
	}
	if _, err := m.cli.JoinRoomByID(ctx, evt.RoomID); err != nil {
		m.Log.Warn("matrix: could not join", "account", m.Name, "room", evt.RoomID, "err", err)
		return
	}
	m.members(ctx, evt.RoomID)
	if mem.IsDirect {
		m.rememberDirect(ctx, evt.Sender, evt.RoomID)
	}
}

func (m *Conn) onMessage(ctx context.Context, evt *event.Event) {
	if evt.Sender == m.me || time.UnixMilli(evt.Timestamp).Before(m.start.Add(-time.Minute)) {
		return
	}
	msg := evt.Content.AsMessage()
	body := msg.Body
	switch msg.MsgType {
	case event.MsgText, event.MsgNotice:
	case event.MsgEmote:
		body = "* " + body
	case event.MsgImage, event.MsgFile, event.MsgVideo, event.MsgAudio:
		body = "[" + strings.TrimPrefix(string(msg.MsgType), "m.") + ": " + msg.Body + "]"
	default:
		return
	}
	if msg.RelatesTo != nil && msg.RelatesTo.Type == event.RelReplace {
		return // an edit: the original was passed on already
	}
	name := m.displayName(ctx, evt.RoomID, evt.Sender)
	members := m.members(ctx, evt.RoomID)
	cm := store.ChatMessage{Conn: m.ID(), Body: body, TS: evt.Timestamp / 1000}
	if len(members) <= 2 {
		cm.Peer, cm.PeerName = m.userAddr(evt.Sender), name
		m.rememberDirect(ctx, evt.Sender, evt.RoomID)
	} else {
		cm.Peer = roomAddr(evt.RoomID)
		cm.PeerName = name + " (" + m.roomName(ctx, evt.RoomID) + ")"
	}
	if err := m.hub.In(cm); err != nil {
		m.Log.Warn("matrix: message not kept", "account", m.Name, "err", err)
	}
}

func (m *Conn) displayName(ctx context.Context, room id.RoomID, u id.UserID) string {
	if mem, _ := m.cli.StateStore.GetMember(ctx, room, u); mem != nil && mem.Displayname != "" {
		return mem.Displayname
	}
	return u.Localpart()
}

func (m *Conn) roomName(ctx context.Context, room id.RoomID) string {
	var n event.RoomNameEventContent
	if m.cli.StateEvent(ctx, room, event.StateRoomName, "", &n) == nil && n.Name != "" {
		return n.Name
	}
	return "group"
}

// ---- addresses

func (m *Conn) userAddr(u id.UserID) string {
	if u.Homeserver() == m.me.Homeserver() {
		return u.Localpart()
	}
	return u.Localpart() + "=" + u.Homeserver()
}

func roomAddr(r id.RoomID) string { return strings.Replace(string(r), ":", "=", 1) }

// target reads an address: a room id, an alias, or a user.
func (m *Conn) target(peer string) (room id.RoomID, alias id.RoomAlias, user id.UserID) {
	p := strings.Replace(peer, "=", ":", 1)
	switch {
	case strings.HasPrefix(p, "!"):
		return id.RoomID(p), "", ""
	case strings.HasPrefix(p, "#"):
		return "", id.RoomAlias(p), ""
	}
	p = strings.TrimPrefix(p, "@")
	if !strings.Contains(p, ":") {
		p += ":" + m.me.Homeserver()
	}
	return "", "", id.UserID("@" + strings.ToLower(p))
}

// ---- direct chats (m.direct, as other clients keep it)

func (m *Conn) directs(ctx context.Context) event.DirectChatsEventContent {
	d := event.DirectChatsEventContent{}
	m.cli.GetAccountData(ctx, event.AccountDataDirectChats.Type, &d)
	return d
}

func (m *Conn) rememberDirect(ctx context.Context, u id.UserID, room id.RoomID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.directs(ctx)
	for _, r := range d[u] {
		if r == room {
			return
		}
	}
	d[u] = append(d[u], room)
	m.cli.SetAccountData(ctx, event.AccountDataDirectChats.Type, d)
}

// directRoom is the direct chat with u: one you already have, else a new encrypted one.
func (m *Conn) directRoom(ctx context.Context, u id.UserID) (id.RoomID, error) {
	for _, r := range m.directs(ctx)[u] {
		if m.cli.StateStore.IsInRoom(ctx, r, m.me) {
			return r, nil
		}
	}
	if _, err := m.cli.GetProfile(ctx, u); errors.Is(err, mautrix.MNotFound) {
		return "", chat.ErrNoRoute // no such user: no empty room for a mistyped name
	}
	resp, err := m.cli.CreateRoom(ctx, &mautrix.ReqCreateRoom{
		Preset:   "trusted_private_chat",
		IsDirect: true,
		Invite:   []id.UserID{u},
		InitialState: []*event.Event{{
			Type:    event.StateEncryption,
			Content: event.Content{Parsed: &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}},
		}},
	})
	if err != nil {
		var he mautrix.HTTPError
		if errors.As(err, &he) && he.RespError != nil && (he.RespError.ErrCode == "M_NOT_FOUND" || he.RespError.ErrCode == "M_INVALID_PARAM") {
			return "", chat.ErrNoRoute
		}
		return "", err
	}
	m.rememberDirect(ctx, u, resp.RoomID)
	return resp.RoomID, nil
}

// Send passes a message to a user (their direct chat, made when there is none) or a room.
func (m *Conn) Send(ctx context.Context, _, peer, body, _ string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	room, alias, user := m.target(peer)
	switch {
	case alias != "":
		r, err := m.cli.ResolveAlias(ctx, alias)
		if err != nil {
			return chat.ErrNoRoute
		}
		room = r.RoomID
	case user != "":
		r, err := m.directRoom(ctx, user)
		if err != nil {
			return err
		}
		room = r
	}
	m.members(ctx, room)
	if _, err := m.cli.SendText(ctx, room, body); err != nil {
		var he mautrix.HTTPError
		if errors.As(err, &he) && he.RespError != nil && he.RespError.ErrCode == "M_FORBIDDEN" {
			return chat.ErrNoRoute // not in that room
		}
		return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
	}
	return nil
}
