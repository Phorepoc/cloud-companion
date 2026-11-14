// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package signal connects a Signal account as a chat connection (docs/messaging.md) through signal-cli-rest-api
// (https://github.com/bbernhard/signal-cli-rest-api, the compose profile "signal"): Cloud Companion is linked as one more device of
// your Signal account, takes the messages of your chats and groups into the chat inbox and sends your answers back.
//
// Addresses: a phone number ("+306912345678", or the account's ID when the number is hidden) for a person, the group's ID
// ("group.abc…", as the API gives it) for a group. Numbers go out as SMS unless a rule says otherwise: give the account a prefix.
package signal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// Cfg is the account's configuration (store.Account.Config); it has no secret (the API is on your own network, in the compose
// profile on Docker's internal network only).
type Cfg struct {
	API    string `json:"api"`    // http://signal-cli:8080 with the compose profile
	Number string `json:"number"` // your account's number, +306912345678
}

func (c *Cfg) norm() error {
	c.API, c.Number = strings.TrimRight(strings.TrimSpace(c.API), "/"), strings.ReplaceAll(strings.TrimSpace(c.Number), " ", "")
	if c.API == "" {
		c.API = "http://signal-cli:8080"
	}
	if !strings.Contains(c.API, "://") {
		c.API = "http://" + c.API
	}
	if !strings.HasPrefix(c.Number, "+") || len(c.Number) < 8 {
		return errors.New("your number is needed with its country code, as +306912345678")
	}
	return nil
}

type api struct {
	base string
	hc   *http.Client
}

func newAPI(base string) *api { return &api{base: base, hc: &http.Client{Timeout: 30 * time.Second}} }

type apiErr struct {
	code int
	msg  string
}

func (e *apiErr) Error() string { return fmt.Sprintf("HTTP %d: %s", e.code, e.msg) }

func (a *api) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(b))
		}
		return &apiErr{code: resp.StatusCode, msg: e.Error}
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

type about struct {
	Mode    string `json:"mode"` // normal, native or json-rpc
	Version string `json:"version"`
}

// check reaches the API and finds the number among its accounts.
func check(ctx context.Context, c Cfg) (about, error) {
	a := newAPI(c.API)
	var ab about
	if err := a.call(ctx, "GET", "/v1/about", nil, &ab); err != nil {
		return ab, fmt.Errorf("no answer from signal-cli-rest-api at %s: %w", c.API, err)
	}
	var accounts []string
	if err := a.call(ctx, "GET", "/v1/accounts", nil, &accounts); err != nil {
		return ab, err
	}
	for _, n := range accounts {
		if n == c.Number {
			return ab, nil
		}
	}
	return ab, errors.New(c.Number + " is not linked yet: link Cloud Companion from your phone first (Signal → Settings → Linked devices)")
}

// Test checks that the API is there and the number is linked.
func Test(ctx context.Context, c Cfg) (string, error) {
	if err := c.norm(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ab, err := check(ctx, c)
	if err != nil {
		return "", err
	}
	return "linked as " + c.Number + " (signal-cli-rest-api " + ab.Version + ", " + ab.Mode + " mode)", nil
}

// LinkQR is the QR code (PNG) to scan on your phone in Signal → Settings → Linked devices → Link a new device; once scanned, the
// API holds the new device's keys and its number shows up as an account.
func LinkQR(ctx context.Context, apiURL string) ([]byte, error) {
	c := Cfg{API: apiURL, Number: "+00000000"}
	c.norm()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", c.API+"/v1/qrcodelink?device_name="+url.QueryEscape("Cloud Companion"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no answer from signal-cli-rest-api at %s: %w", c.API, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 || !bytes.HasPrefix(b, []byte("\x89PNG")) {
		return nil, fmt.Errorf("signal-cli-rest-api gave no QR code (HTTP %d)", resp.StatusCode)
	}
	return b, nil
}

// Conn is one Signal account, a chat connection.
type Conn struct {
	Acc  string
	Name string
	Cfg  Cfg
	Log  *slog.Logger
	// Status reports the link for the account's row.
	Status func(error)
	Every  time.Duration // how often messages are fetched in the API's normal and native modes (default 3 s)

	hub    *chat.Hub
	a      *api
	mode   string
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	groups map[string]string // group names by internal ID (as envelopes carry it) and by ID
	ids    map[string]string // group IDs (group.…) by internal ID
}

func ConnID(acc string) string { return "signal:" + acc }

func (x *Conn) ID() string         { return ConnID(x.Acc) }
func (x *Conn) Label() string      { return x.Name + " (Signal)" }
func (x *Conn) SetHub(h *chat.Hub) { x.hub = h }

// Start is the chat.Starter for "signal" accounts.
func Start(log *slog.Logger) chat.Starter {
	return func(a store.Account, _ string, status func(error)) (chat.Running, error) {
		var c Cfg
		if err := json.Unmarshal([]byte(a.Config), &c); err != nil {
			return nil, errors.New("bad configuration")
		}
		x := &Conn{Acc: a.ID, Name: a.Name, Cfg: c, Log: log, Status: status}
		return x, x.Start()
	}
}

// Start checks the link once and then receives until Stop: over the API's websocket in json-rpc mode, else by fetching.
func (x *Conn) Start() error {
	if err := x.Cfg.norm(); err != nil {
		return err
	}
	if x.Every == 0 {
		x.Every = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	ab, err := check(ctx, x.Cfg)
	cancel()
	if err != nil {
		return err
	}
	x.a, x.mode = newAPI(x.Cfg.API), ab.Mode
	x.loadGroups(context.Background())
	rctx, rcancel := context.WithCancel(context.Background())
	x.cancel, x.done = rcancel, make(chan struct{})
	go x.run(rctx)
	return nil
}

// Stop ends receiving.
func (x *Conn) Stop() {
	if x.cancel != nil {
		x.cancel()
		<-x.done
	}
}

func (x *Conn) run(ctx context.Context) {
	defer close(x.done)
	for ctx.Err() == nil {
		var err error
		if x.mode == "json-rpc" {
			err = x.listen(ctx)
		} else {
			err = x.fetchLoop(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		x.Log.Warn("signal: receiving stopped, trying again", "account", x.Name, "err", err)
		if x.Status != nil {
			x.Status(fmt.Errorf("receiving: %v", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (x *Conn) wsURL() string {
	u := strings.Replace(strings.Replace(x.Cfg.API, "https://", "wss://", 1), "http://", "ws://", 1)
	return u + "/v1/receive/" + url.PathEscape(x.Cfg.Number)
}

// listen takes envelopes from the json-rpc mode's websocket, one per frame.
func (x *Conn) listen(ctx context.Context) error {
	c, _, err := websocket.Dial(ctx, x.wsURL(), nil)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(4 << 20)
	if x.Status != nil {
		x.Status(nil)
	}
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return err
		}
		var in received
		if json.Unmarshal(b, &in) == nil {
			x.take(ctx, in)
		}
	}
}

// fetchLoop fetches waiting envelopes now and then (normal and native modes).
func (x *Conn) fetchLoop(ctx context.Context) error {
	reported := false
	t := time.NewTicker(x.Every)
	defer t.Stop()
	for {
		var ins []received
		if err := x.a.call(ctx, "GET", "/v1/receive/"+url.PathEscape(x.Cfg.Number)+"?timeout=1", nil, &ins); err != nil {
			return err
		}
		if !reported && x.Status != nil {
			x.Status(nil)
			reported = true
		}
		for _, in := range ins {
			x.take(ctx, in)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

type received struct {
	Envelope struct {
		Source       string `json:"source"`
		SourceNumber string `json:"sourceNumber"`
		SourceUUID   string `json:"sourceUuid"`
		SourceName   string `json:"sourceName"`
		Timestamp    int64  `json:"timestamp"`
		DataMessage  *struct {
			Timestamp   int64  `json:"timestamp"`
			Message     string `json:"message"`
			Attachments []struct {
				ContentType string `json:"contentType"`
				Filename    string `json:"filename"`
			} `json:"attachments"`
			GroupInfo *struct {
				GroupID   string `json:"groupId"`
				GroupName string `json:"groupName"`
			} `json:"groupInfo"`
			Reaction *struct{} `json:"reaction"`
			Sticker  *struct{} `json:"sticker"`
		} `json:"dataMessage"`
	} `json:"envelope"`
	Account string `json:"account"`
}

func (x *Conn) take(ctx context.Context, in received) {
	e := in.Envelope
	d := e.DataMessage
	if d == nil || d.Reaction != nil { // receipts, typing, your own other devices' sync messages, reactions
		return
	}
	body := d.Message
	for _, at := range d.Attachments {
		name := at.Filename
		if name == "" {
			name = at.ContentType
		}
		body = strings.TrimSpace(body + " [file: " + name + "]")
	}
	if d.Sticker != nil && body == "" {
		body = "[sticker]"
	}
	if body == "" {
		return
	}
	from := e.SourceNumber
	if from == "" {
		from = e.SourceUUID
	}
	if from == "" {
		from = e.Source
	}
	name := e.SourceName
	if name == "" {
		name = from
	}
	ts := d.Timestamp
	if ts == 0 {
		ts = e.Timestamp
	}
	cm := store.ChatMessage{Conn: x.ID(), Peer: from, PeerName: name, Body: body, TS: ts / 1000}
	if g := d.GroupInfo; g != nil {
		id, gname := x.group(ctx, g.GroupID)
		if gname == "" {
			gname = g.GroupName
		}
		if gname == "" {
			gname = "group"
		}
		cm.Peer, cm.PeerName = id, name+" ("+gname+")"
	}
	if err := x.hub.In(cm); err != nil {
		x.Log.Warn("signal: message not kept", "account", x.Name, "err", err)
	}
}

func (x *Conn) loadGroups(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var gs []struct {
		Name       string `json:"name"`
		ID         string `json:"id"`
		InternalID string `json:"internal_id"`
	}
	if err := x.a.call(ctx, "GET", "/v1/groups/"+url.PathEscape(x.Cfg.Number), nil, &gs); err != nil {
		x.Log.Warn("signal: groups not read", "account", x.Name, "err", err)
		return
	}
	names, ids := map[string]string{}, map[string]string{}
	for _, g := range gs {
		names[g.ID], names[g.InternalID], ids[g.InternalID] = g.Name, g.Name, g.ID
	}
	x.mu.Lock()
	x.groups, x.ids = names, ids
	x.mu.Unlock()
}

// group is a group's address (its "group.…" ID) and name from the internal ID an envelope carries; a group joined since the last
// look is looked up again.
func (x *Conn) group(ctx context.Context, internal string) (string, string) {
	for try := 0; try < 2; try++ {
		x.mu.Lock()
		id, name := x.ids[internal], x.groups[internal]
		x.mu.Unlock()
		if id != "" {
			return id, name
		}
		if try == 0 {
			x.loadGroups(ctx)
		}
	}
	return "group." + internal, ""
}

// Send passes a message to a number, an account ID or a group ID.
func (x *Conn) Send(ctx context.Context, _, peer, body, _ string) error {
	peer = strings.TrimSpace(peer)
	if peer == "" || strings.ContainsAny(peer, " /") {
		return chat.ErrNoRoute
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second) // signal-cli can take a while for a first message to someone
	defer cancel()
	err := x.a.call(ctx, "POST", "/v2/send", map[string]any{"message": body, "number": x.Cfg.Number, "recipients": []string{peer}}, nil)
	var ae *apiErr
	if errors.As(err, &ae) && ae.code == 400 && (strings.Contains(ae.msg, "not registered") || strings.Contains(ae.msg, "Invalid") ||
		strings.Contains(ae.msg, "Unregistered") || strings.Contains(ae.msg, "not found") || strings.Contains(ae.msg, "doesn't exist")) {
		return chat.ErrNoRoute
	}
	if err != nil {
		return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
	}
	return nil
}
