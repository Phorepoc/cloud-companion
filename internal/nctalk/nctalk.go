// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package nctalk connects a Nextcloud Talk account as a chat connection (docs/messaging.md), through Talk's documented OCS API
// (https://nextcloud-talk.readthedocs.io/en/latest/): Cloud Companion looks at your conversations every few seconds, takes the new
// messages of one-to-one chats and groups into the chat inbox, and sends your answers back.
//
// Addresses: a user ID ("bob") for the one-to-one chat with them (made when there is none), "#token" for a group conversation.
package nctalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// Cfg is the account's configuration (store.Account.Config); an app password (Nextcloud → Settings → Security) is its secret.
type Cfg struct {
	URL  string `json:"url"`  // https://cloud.example.org
	User string `json:"user"` // your login name
}

func (c *Cfg) norm() error {
	c.URL, c.User = strings.TrimRight(strings.TrimSpace(c.URL), "/"), strings.TrimSpace(c.User)
	if c.URL == "" || c.User == "" {
		return errors.New("the Nextcloud address and the user are needed")
	}
	if !strings.Contains(c.URL, "://") {
		c.URL = "https://" + c.URL
	}
	return nil
}

type api struct {
	base, user, pass string
	hc               *http.Client
}

type ocsMeta struct {
	Status     string `json:"status"`
	StatusCode int    `json:"statuscode"`
	Message    string `json:"message"`
}

var errLogin = errors.New("login refused: check the user and the app password")

// call makes an OCS request and decodes its data into out; 304 (nothing new) gives (false, nil).
func (a *api) call(ctx context.Context, method, path string, form url.Values, out any) (bool, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+"/ocs/v2.php"+path, body)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(a.user, a.pass)
	req.Header.Set("OCS-APIRequest", "true")
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return false, errLogin
	}
	var env struct {
		OCS struct {
			Meta ocsMeta         `json:"meta"`
			Data json.RawMessage `json:"data"`
		} `json:"ocs"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&env); err != nil {
		return false, fmt.Errorf("%s: HTTP %d, not a Nextcloud answer", path, resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return false, &httpErr{code: resp.StatusCode, msg: env.OCS.Meta.Message}
	}
	if out != nil {
		return true, json.Unmarshal(env.OCS.Data, out)
	}
	return true, nil
}

type httpErr struct {
	code int
	msg  string
}

func (e *httpErr) Error() string { return fmt.Sprintf("HTTP %d %s", e.code, e.msg) }

func newAPI(c Cfg, pass string) *api {
	return &api{base: c.URL, user: c.User, pass: pass, hc: &http.Client{Timeout: 30 * time.Second}}
}

// Test checks the login and that Talk is there.
func Test(ctx context.Context, c Cfg, pass string) (string, error) {
	if err := c.norm(); err != nil {
		return "", err
	}
	a := newAPI(c, pass)
	var me struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayname"`
	}
	if _, err := a.call(ctx, "GET", "/cloud/user", nil, &me); err != nil {
		if errors.Is(err, errLogin) {
			return "", err
		}
		return "", fmt.Errorf("no answer from %s: %w", c.URL, err)
	}
	var rooms []room
	if _, err := a.call(ctx, "GET", "/apps/spreed/api/v4/room", nil, &rooms); err != nil {
		return "", errors.New("signed in, but Talk is not installed or not enabled for you")
	}
	return "login works as " + me.ID + ", with Talk", nil
}

// Talk's conversation types.
const (
	oneToOne     = 1
	group        = 2
	public       = 3
	changelog    = 4
	noteToSelf   = 6
	formerOneTo1 = 5
)

type room struct {
	Token       string `json:"token"`
	Type        int    `json:"type"`
	Name        string `json:"name"` // the other user's ID in a one-to-one chat
	DisplayName string `json:"displayName"`
	LastMessage struct {
		ID int64 `json:"id"`
	} `json:"lastMessage"`
}

type message struct {
	ID               int64           `json:"id"`
	ActorType        string          `json:"actorType"`
	ActorID          string          `json:"actorId"`
	ActorDisplayName string          `json:"actorDisplayName"`
	Timestamp        int64           `json:"timestamp"`
	Message          string          `json:"message"`
	RawParameters    json.RawMessage `json:"messageParameters"`
	SystemMessage    string          `json:"systemMessage"`
	MessageType      string          `json:"messageType"`
}

var placeholder = regexp.MustCompile(`\{([a-zA-Z0-9_-]+)\}`)

// text writes the message with its placeholders filled in ({mention-user1} → @bob, {file} → the file's name).
func (m message) text() string {
	var params map[string]struct {
		Type string `json:"type"`
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	json.Unmarshal(m.RawParameters, &params) // an empty list when there are none
	return placeholder.ReplaceAllStringFunc(m.Message, func(s string) string {
		p, ok := params[s[1:len(s)-1]]
		switch {
		case !ok:
			return s
		case p.Type == "user" || p.Type == "guest" || p.Type == "call" || p.Type == "user-group":
			return "@" + p.Name
		case p.Type == "file":
			return "[file: " + p.Name + "]"
		}
		return p.Name
	})
}

// Conn is one Nextcloud Talk account, a chat connection.
type Conn struct {
	Acc  string
	Name string
	Cfg  Cfg
	Pass string
	Log  *slog.Logger
	// Status reports the polling for the account's row.
	Status func(error)
	Every  time.Duration // how often conversations are looked at (default 5 s)

	hub    *chat.Hub
	a      *api
	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.Mutex
	last  map[string]int64 // the last message seen per conversation
	rooms map[string]room
}

func ConnID(acc string) string { return "nctalk:" + acc }

func (x *Conn) ID() string         { return ConnID(x.Acc) }
func (x *Conn) Label() string      { return x.Name + " (Nextcloud Talk)" }
func (x *Conn) SetHub(h *chat.Hub) { x.hub = h }

// Start is the chat.Starter for "nctalk" accounts.
func Start(log *slog.Logger) chat.Starter {
	return func(a store.Account, pass string, status func(error)) (chat.Running, error) {
		var c Cfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return nil, errors.New("bad configuration")
		}
		x := &Conn{Acc: a.ID, Name: a.Name, Cfg: c, Pass: pass, Log: log, Status: status}
		return x, x.Start()
	}
}

// Start looks at the conversations once (a refused login is reported right away; what is there already is not replayed) and then
// keeps looking until Stop.
func (x *Conn) Start() error {
	if err := x.Cfg.norm(); err != nil {
		return err
	}
	if x.Every == 0 {
		x.Every = 5 * time.Second
	}
	x.a = newAPI(x.Cfg, x.Pass)
	x.last = map[string]int64{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	rooms, err := x.listRooms(ctx)
	cancel()
	if err != nil {
		return err
	}
	for _, r := range rooms {
		x.last[r.Token] = r.LastMessage.ID
	}
	pctx, pcancel := context.WithCancel(context.Background())
	x.cancel, x.done = pcancel, make(chan struct{})
	go x.run(pctx)
	return nil
}

// Stop ends the polling.
func (x *Conn) Stop() {
	if x.cancel != nil {
		x.cancel()
		<-x.done
	}
}

func (x *Conn) listRooms(ctx context.Context) ([]room, error) {
	var rooms []room
	if _, err := x.a.call(ctx, "GET", "/apps/spreed/api/v4/room", nil, &rooms); err != nil {
		if errors.Is(err, errLogin) {
			return nil, err
		}
		return nil, fmt.Errorf("could not read your conversations: %w", err)
	}
	m := map[string]room{}
	for _, r := range rooms {
		m[r.Token] = r
	}
	x.mu.Lock()
	x.rooms = m
	x.mu.Unlock()
	return rooms, nil
}

func (x *Conn) run(ctx context.Context) {
	defer close(x.done)
	t := time.NewTicker(x.Every)
	defer t.Stop()
	reported, lastErr := false, ""
	for {
		err := x.poll(ctx)
		if ctx.Err() != nil {
			return
		}
		e := ""
		if err != nil {
			e = err.Error()
			x.Log.Warn("nctalk: polling failed", "account", x.Name, "err", err)
		}
		if x.Status != nil && (!reported || e != lastErr) { // only when it changes: not a write every few seconds
			x.Status(err)
			reported, lastErr = true, e
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// poll takes the new messages of every conversation whose last message moved on.
func (x *Conn) poll(ctx context.Context) error {
	rooms, err := x.listRooms(ctx)
	if err != nil {
		return err
	}
	for _, r := range rooms {
		if r.Type == changelog || r.Type == noteToSelf {
			continue
		}
		x.mu.Lock()
		known, seen := x.last[r.Token]
		x.mu.Unlock()
		if !seen { // a conversation that is new since we started: everything in it is new
			known = 0
		}
		if r.LastMessage.ID <= known {
			continue
		}
		if err := x.fetch(ctx, r, known); err != nil {
			return err
		}
	}
	return nil
}

func (x *Conn) fetch(ctx context.Context, r room, after int64) error {
	q := url.Values{"lookIntoFuture": {"1"}, "timeout": {"0"}, "limit": {"100"}, "includeLastKnown": {"0"}, "setReadMarker": {"0"}}
	if after > 0 {
		q.Set("lastKnownMessageId", strconv.FormatInt(after, 10))
	}
	var msgs []message
	if _, err := x.a.call(ctx, "GET", "/apps/spreed/api/v1/chat/"+r.Token+"?"+q.Encode(), nil, &msgs); err != nil {
		return err
	}
	for _, m := range msgs {
		if m.ID > after {
			after = m.ID
		}
		if m.SystemMessage != "" || m.MessageType == "system" || m.MessageType == "command" || m.ActorType == "bots" ||
			(m.ActorType == "users" && m.ActorID == x.Cfg.User) {
			continue
		}
		cm := store.ChatMessage{Conn: x.ID(), Body: m.text(), TS: m.Timestamp}
		if r.Type == oneToOne || r.Type == formerOneTo1 {
			cm.Peer, cm.PeerName = r.Name, m.ActorDisplayName
		} else {
			cm.Peer, cm.PeerName = "#"+r.Token, m.ActorDisplayName+" ("+r.DisplayName+")"
		}
		if err := x.hub.In(cm); err != nil {
			x.Log.Warn("nctalk: message not kept", "account", x.Name, "err", err)
		}
	}
	x.mu.Lock()
	x.last[r.Token] = after
	x.mu.Unlock()
	return nil
}

// token is the conversation for an address: "#token", or the one-to-one chat with a user (made when there is none).
func (x *Conn) token(ctx context.Context, peer string) (string, error) {
	if t, ok := strings.CutPrefix(peer, "#"); ok {
		x.mu.Lock()
		_, known := x.rooms[t]
		x.mu.Unlock()
		if !known {
			return "", chat.ErrNoRoute
		}
		return t, nil
	}
	x.mu.Lock()
	for _, r := range x.rooms {
		if r.Type == oneToOne && strings.EqualFold(r.Name, peer) {
			x.mu.Unlock()
			return r.Token, nil
		}
	}
	x.mu.Unlock()
	var r room
	_, err := x.a.call(ctx, "POST", "/apps/spreed/api/v4/room", url.Values{"roomType": {"1"}, "invite": {peer}}, &r)
	var he *httpErr
	if errors.As(err, &he) && (he.code == 404 || he.code == 400) {
		return "", chat.ErrNoRoute // nobody by that name
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
	}
	x.mu.Lock()
	if x.rooms == nil {
		x.rooms = map[string]room{}
	}
	x.rooms[r.Token] = r
	x.last[r.Token] = r.LastMessage.ID // nothing in it to pass on
	x.mu.Unlock()
	return r.Token, nil
}

// Send posts a message to a user's one-to-one chat or a group conversation.
func (x *Conn) Send(ctx context.Context, _, peer, body, _ string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	peer = strings.TrimSpace(peer)
	if peer == "" {
		return chat.ErrNoRoute
	}
	t, err := x.token(ctx, peer)
	if err != nil {
		return err
	}
	var posted message
	_, err = x.a.call(ctx, "POST", "/apps/spreed/api/v1/chat/"+t, url.Values{"message": {body}}, &posted)
	var he *httpErr
	if errors.As(err, &he) && (he.code == 403 || he.code == 404) {
		return chat.ErrNoRoute
	}
	if err != nil {
		return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
	}
	return nil
}
