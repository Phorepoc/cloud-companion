// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package irc connects an IRC network as a chat connection (docs/messaging.md): Cloud Companion stays connected with your nick
// (TLS by default, SASL when you have an account), joins your channels, takes private messages and the channel lines that name
// you (or every line, if you ask) into the chat inbox, and sends your answers back.
//
// Addresses: a nick ("bob") for a private message, "#channel" for a channel.
package irc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// Cfg is the account's configuration (store.Account.Config); the password (SASL, or the server password without an account) is
// its secret.
type Cfg struct {
	Server   string `json:"server"`             // irc.libera.chat, or host:port (6697 with TLS, 6667 without)
	Plain    bool   `json:"plain,omitempty"`    // no TLS (a server of your own on a network you trust)
	Insecure bool   `json:"insecure,omitempty"` // TLS with a certificate that cannot be checked
	Nick     string `json:"nick"`
	Account  string `json:"account,omitempty"` // the SASL account, when it is not the nick
	// ServerPass sends the password as the server password (PASS: a bouncer such as ZNC) instead of signing in with SASL.
	ServerPass bool     `json:"server_pass,omitempty"`
	Channels   []string `json:"channels,omitempty"`
	// All passes every channel line on, not only those that name you (busy channels flood a phone).
	All bool `json:"all,omitempty"`
}

func (c *Cfg) norm() error {
	c.Server, c.Nick, c.Account = strings.TrimSpace(c.Server), strings.TrimSpace(c.Nick), strings.TrimSpace(c.Account)
	if c.Server == "" || c.Nick == "" {
		return errors.New("the server and the nick are needed")
	}
	if strings.ContainsAny(c.Nick, " ,*?!@#:") {
		return errors.New("a nick cannot hold spaces or , * ? ! @ # :")
	}
	if _, _, err := net.SplitHostPort(c.Server); err != nil {
		port := "6697"
		if c.Plain {
			port = "6667"
		}
		c.Server = net.JoinHostPort(c.Server, port)
	}
	var chans []string
	for _, ch := range c.Channels {
		if ch = strings.TrimSpace(ch); ch != "" {
			if !strings.HasPrefix(ch, "#") && !strings.HasPrefix(ch, "&") {
				ch = "#" + ch
			}
			chans = append(chans, ch)
		}
	}
	c.Channels = chans
	return nil
}

func (c Cfg) conn(pass string) *ircevent.Connection {
	host, _, _ := net.SplitHostPort(c.Server)
	ic := &ircevent.Connection{
		Server:        c.Server,
		Nick:          c.Nick,
		User:          c.Nick,
		RealName:      "Cloud Companion",
		UseTLS:        !c.Plain,
		TLSConfig:     &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.Insecure},
		RequestCaps:   []string{"server-time", "message-tags"},
		Timeout:       20 * time.Second,
		KeepAlive:     2 * time.Minute,
		ReconnectFreq: 15 * time.Second,
		QuitMessage:   "Cloud Companion",
		Log:           log.New(io.Discard, "", 0),
	}
	switch {
	case pass == "":
	case c.ServerPass:
		ic.Password = pass
	default:
		ic.UseSASL, ic.SASLLogin, ic.SASLPassword = true, c.Nick, pass
		if c.Account != "" {
			ic.SASLLogin = c.Account
		}
	}
	return ic
}

func loginErr(err error) error {
	s := err.Error()
	if strings.Contains(s, "SASL") || strings.Contains(s, "904") {
		return errors.New("login refused: check the account and password")
	}
	return fmt.Errorf("could not connect: %w", err)
}

// Test checks that the server takes the nick (and the password), then leaves.
func Test(ctx context.Context, c Cfg, pass string) (string, error) {
	if err := c.norm(); err != nil {
		return "", err
	}
	ic := c.conn(pass)
	if err := ic.Connect(); err != nil {
		return "", loginErr(err)
	}
	nick := ic.CurrentNick()
	ic.Quit()
	if nick != c.Nick {
		return "connected, but " + c.Nick + " is taken: you would be " + nick, nil
	}
	return "connected as " + nick, nil
}

// Conn is one IRC network, a chat connection.
type Conn struct {
	Acc  string
	Name string
	Cfg  Cfg
	Pass string
	Log  *slog.Logger
	// Status reports the connection for the account's row.
	Status func(error)

	hub  *chat.Hub
	ic   *ircevent.Connection
	done chan struct{}
	mu   sync.Mutex
}

func ConnID(acc string) string { return "irc:" + acc }

func (x *Conn) ID() string         { return ConnID(x.Acc) }
func (x *Conn) Label() string      { return x.Name + " (IRC)" }
func (x *Conn) SetHub(h *chat.Hub) { x.hub = h }

// Start is the chat.Starter for "irc" accounts.
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

// Start connects once (a refused login is reported right away) and then stays connected until Stop; the library reconnects.
func (x *Conn) Start() error {
	if err := x.Cfg.norm(); err != nil {
		return err
	}
	x.ic = x.Cfg.conn(x.Pass)
	x.ic.AddConnectCallback(func(ircmsg.Message) {
		for _, ch := range x.Cfg.Channels {
			x.ic.Join(ch)
		}
		if x.Status != nil {
			x.Status(nil)
		}
	})
	x.ic.AddDisconnectCallback(func(ircmsg.Message) {
		if x.Status != nil {
			x.Status(errors.New("disconnected, connecting again"))
		}
	})
	x.ic.AddCallback("PRIVMSG", x.onMessage)
	x.ic.AddCallback("NOTICE", func(m ircmsg.Message) {
		if len(m.Params) == 2 && !strings.HasPrefix(m.Params[0], "#") && strings.Contains(m.Source, "!") { // a person's notice to you, not the server's
			x.onMessage(m)
		}
	})
	x.ic.AddCallback(ircevent.ERR_NOSUCHNICK, func(m ircmsg.Message) {
		x.Log.Warn("irc: no such nick or channel", "account", x.Name, "target", strings.Join(m.Params[1:], " "))
	})
	if err := x.ic.Connect(); err != nil {
		return loginErr(err)
	}
	x.done = make(chan struct{})
	go func() { defer close(x.done); x.ic.Loop() }()
	return nil
}

// Stop leaves the network.
func (x *Conn) Stop() {
	if x.ic != nil && x.done != nil {
		x.ic.Quit()
		<-x.done
	}
}

func (x *Conn) onMessage(m ircmsg.Message) {
	if len(m.Params) < 2 {
		return
	}
	nick, target, body := m.Nick(), m.Params[0], m.Params[1]
	me := x.ic.CurrentNick()
	if strings.EqualFold(nick, me) {
		return
	}
	if strings.HasPrefix(body, "\x01ACTION ") { // /me
		body = "* " + strings.TrimSuffix(strings.TrimPrefix(body, "\x01ACTION "), "\x01")
	} else if strings.HasPrefix(body, "\x01") {
		return // other CTCP
	}
	ts := time.Now()
	if ok, t := m.GetTag("time"); ok { // server-time: when it was said, as with a replayed history
		if d, err := time.Parse(time.RFC3339Nano, t); err == nil {
			ts = d
		}
	}
	cm := store.ChatMessage{Conn: x.ID(), Body: body, TS: ts.Unix()}
	if strings.HasPrefix(target, "#") || strings.HasPrefix(target, "&") {
		if !x.Cfg.All && !mentions(body, me) {
			return
		}
		cm.Peer, cm.PeerName = target, nick+" ("+target+")"
	} else {
		cm.Peer, cm.PeerName = nick, nick
	}
	if err := x.hub.In(cm); err != nil {
		x.Log.Warn("irc: message not kept", "account", x.Name, "err", err)
	}
}

// mentions is whether a line names the nick as a word ("bob: hi", "hi bob!"), as IRC clients highlight.
func mentions(line, nick string) bool {
	l, n := strings.ToLower(line), strings.ToLower(nick)
	for i := strings.Index(l, n); i >= 0; {
		end := i + len(n)
		if (i == 0 || !word(l[i-1])) && (end == len(l) || !word(l[end])) {
			return true
		}
		j := strings.Index(l[i+1:], n)
		if j < 0 {
			break
		}
		i += 1 + j
	}
	return false
}

func word(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || strings.IndexByte("_-[]\\`^{}|", b) >= 0
}

// Send passes a message to a nick or a channel; long messages are split as IRC lines must be.
func (x *Conn) Send(_ context.Context, _, peer, body, _ string) error {
	peer = strings.TrimSpace(peer)
	if peer == "" || strings.ContainsAny(peer, " ,") {
		return chat.ErrNoRoute
	}
	if !x.ic.Connected() {
		return fmt.Errorf("%w: not connected", chat.ErrUnavailable)
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r", ""), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, part := range split(line, x.ic.MaxMessageLength(peer)) {
			if err := x.ic.Privmsg(peer, part); err != nil {
				return fmt.Errorf("%w: %v", chat.ErrUnavailable, err)
			}
		}
	}
	return nil
}

// split cuts a line into parts of at most n bytes, at spaces where it can and never inside a character.
func split(s string, n int) []string {
	if n < 32 {
		n = 400
	}
	var out []string
	for len(s) > n {
		cut := strings.LastIndexByte(s[:n], ' ')
		if cut < n/2 {
			cut = n
			for cut > 0 && s[cut]&0xC0 == 0x80 { // a UTF-8 continuation byte
				cut--
			}
		}
		out = append(out, strings.TrimRight(s[:cut], " "))
		s = strings.TrimLeft(s[cut:], " ")
	}
	if s = strings.TrimRight(s, " "); s != "" {
		out = append(out, s)
	}
	return out
}
