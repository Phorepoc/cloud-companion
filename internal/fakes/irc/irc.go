// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package irc is a fake IRC server for web/test/irc.sh, in place of Ergo: TLS on 6697 (a certificate made at start; the suite
// accepts any) and plain on 6667. It does what internal/irc needs: CAP negotiation (server-time, message-tags, sasl), SASL PLAIN
// against the registered accounts (none, as on the suite's fresh Ergo, so any password is refused with 904), NICK/USER registration
// (a taken nick gets 433), JOIN, PING and QUIT. Messages are taken and dropped.
package irc

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/phorepoc/cloud-companion/internal/fakes"
	"github.com/phorepoc/cloud-companion/internal/tlsutil"
)

// Accounts are the SASL accounts and their passwords: none, as on the suite's Ergo where nobody registered.
var Accounts = map[string]string{}

const server = "irc.fake"

var caps = []string{"sasl=PLAIN", "server-time", "message-tags"}

func init() {
	fakes.Register(fakes.Fake{Name: "irc", Run: func(ctx context.Context) error {
		tc, err := TLSConfig()
		if err != nil {
			return err
		}
		plain, err := net.Listen("tcp", ":6667")
		if err != nil {
			return err
		}
		secure, err := tls.Listen("tcp", ":6697", tc)
		if err != nil {
			return err
		}
		s := New()
		errs := make(chan error, 2)
		go func() { errs <- s.Serve(ctx, plain) }()
		go func() { errs <- s.Serve(ctx, secure) }()
		return <-errs
	}})
}

// TLSConfig is a self-signed certificate made now.
func TLSConfig() (*tls.Config, error) {
	dir, err := os.MkdirTemp("", "fake-tls")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	tc, _, err := tlsutil.LoadOrCreate(dir)
	return tc, err
}

// Server is the network: the nicks in use.
type Server struct {
	mu    sync.Mutex
	nicks map[string]bool
}

func New() *Server { return &Server{nicks: map[string]bool{}} }

// Serve answers the clients on ln until ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func() {
			defer c.Close()
			cl := &client{s: s, c: c}
			cl.run()
			s.mu.Lock()
			if cl.registered {
				delete(s.nicks, strings.ToLower(cl.nick))
			}
			s.mu.Unlock()
		}()
	}
}

// client is one connection.
type client struct {
	s          *Server
	c          net.Conn
	nick, user string
	capping    bool // CAP LS came: registration waits for CAP END
	saslMech   bool // AUTHENTICATE PLAIN came: the next AUTHENTICATE is the response
	registered bool
}

func (cl *client) send(format string, a ...any) {
	fmt.Fprintf(cl.c, format+"\r\n", a...)
}

// num sends a numeric reply to the client.
func (cl *client) num(code, text string) {
	n := cl.nick
	if n == "" {
		n = "*"
	}
	cl.send(":%s %s %s %s", server, code, n, text)
}

func (cl *client) run() {
	sc := bufio.NewScanner(cl.c)
	for sc.Scan() {
		m, err := ircmsg.ParseLine(sc.Text())
		if err != nil {
			continue
		}
		if !cl.handle(m) {
			return
		}
	}
}

// handle answers one line; false ends the connection.
func (cl *client) handle(m ircmsg.Message) bool {
	p := func(i int) string {
		if i < len(m.Params) {
			return m.Params[i]
		}
		return ""
	}
	switch m.Command {
	case "CAP":
		switch strings.ToUpper(p(0)) {
		case "LS":
			cl.capping = true
			cl.send(":%s CAP * LS :%s", server, strings.Join(caps, " "))
		case "REQ":
			for _, want := range strings.Fields(p(1)) {
				known := false
				for _, c := range caps {
					known = known || strings.SplitN(c, "=", 2)[0] == want
				}
				if known {
					cl.send(":%s CAP * ACK :%s", server, want)
				} else {
					cl.send(":%s CAP * NAK :%s", server, want)
				}
			}
		case "END":
			cl.capping = false
			cl.register()
		}
	case "AUTHENTICATE":
		switch {
		case !cl.saslMech && strings.ToUpper(p(0)) == "PLAIN":
			cl.saslMech = true
			cl.send("AUTHENTICATE +")
		case !cl.saslMech:
			cl.num("908", "PLAIN :are available SASL mechanisms")
			cl.num("904", ":SASL authentication failed")
		default:
			cl.saslMech = false
			if acc, ok := checkPlain(p(0)); ok {
				cl.num("900", fmt.Sprintf("%s!%s@fake %s :You are now logged in as %s", cl.nick, cl.user, acc, acc))
				cl.num("903", ":SASL authentication successful")
			} else {
				cl.num("904", ":SASL authentication failed")
			}
		}
	case "NICK":
		cl.nick = p(0)
		cl.register()
	case "USER":
		cl.user = p(0)
		cl.register()
	case "PING":
		cl.send(":%s PONG %s :%s", server, server, p(0))
	case "JOIN":
		if cl.registered {
			for _, ch := range strings.Split(p(0), ",") {
				cl.send(":%s!%s@fake JOIN %s", cl.nick, cl.user, ch)
				cl.send(":%s 353 %s = %s :%s", server, cl.nick, ch, cl.nick)
				cl.send(":%s 366 %s %s :End of NAMES list", server, cl.nick, ch)
			}
		}
	case "QUIT":
		cl.send("ERROR :Quit: %s", p(0))
		return false
	}
	return true
}

// register welcomes the client once NICK, USER and the CAP negotiation are done; a nick in use is refused (433) until another comes.
func (cl *client) register() {
	if cl.registered || cl.capping || cl.nick == "" || cl.user == "" {
		return
	}
	cl.s.mu.Lock()
	taken := cl.s.nicks[strings.ToLower(cl.nick)]
	if !taken {
		cl.s.nicks[strings.ToLower(cl.nick)] = true
	}
	cl.s.mu.Unlock()
	if taken {
		cl.num("433", cl.nick+" :Nickname is already in use")
		return
	}
	cl.registered = true
	cl.num("001", ":Welcome to the fake IRC network "+cl.nick)
	cl.num("005", "CASEMAPPING=ascii CHANTYPES=# NICKLEN=32 :are supported by this server")
	cl.num("422", ":MOTD File is missing")
}

// checkPlain reads a PLAIN response (authzid NUL account NUL password) and checks it.
func checkPlain(b64 string) (string, bool) {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(b), "\x00")
	if len(parts) != 3 {
		return "", false
	}
	pw, ok := Accounts[parts[1]]
	return parts[1], ok && pw == parts[2]
}
