// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package xmpp is a fake XMPP server for web/test/xmpp.sh, in place of Prosody: port 5222, the domain "localhost" with the one
// account the suite signs in as (cc@localhost, password pw-cc). It does what internal/xmpp needs: STARTTLS (required, a certificate
// made at start; the suite accepts any), SASL PLAIN (a wrong password is refused with not-authorized), resource binding, an empty
// roster, and a self-presence for each group chat joined. Anything else is ignored, or answered as an unknown IQ.
package xmpp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"github.com/phorepoc/cloud-companion/internal/fakes"
	"github.com/phorepoc/cloud-companion/internal/tlsutil"
)

// Users are the accounts, by local part of the domain "localhost" (what xmpp.sh registered with prosodyctl).
var Users = map[string]string{"cc": "pw-cc"}

const domain = "localhost"

func init() {
	fakes.Register(fakes.Fake{Name: "xmpp", Run: func(ctx context.Context) error {
		ln, err := net.Listen("tcp", ":5222")
		if err != nil {
			return err
		}
		tc, err := TLSConfig()
		if err != nil {
			return err
		}
		return Serve(ctx, ln, tc)
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

// Serve answers the clients on ln until ctx ends.
func Serve(ctx context.Context, ln net.Listener, tc *tls.Config) error {
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
			if err := session(c, tc); err != nil && !errors.Is(err, io.EOF) {
				log.Printf("xmpp: %v", err)
			}
		}()
	}
}

// elem is one stanza or nonza, its attributes and what it holds.
type elem struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Inner   string     `xml:",innerxml"`
}

func (e elem) attr(name string) string {
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// stream reads one stream: the client's header, then its top-level elements.
type stream struct {
	conn net.Conn
	d    *xml.Decoder
}

// open waits for the client's stream header and sends ours with the features.
func (s *stream) open(features string) error {
	s.d = xml.NewDecoder(s.conn)
	for {
		t, err := s.d.Token()
		if err != nil {
			return err
		}
		if st, ok := t.(xml.StartElement); ok && st.Name.Local == "stream" {
			break
		}
	}
	_, err := fmt.Fprintf(s.conn, "<?xml version='1.0'?><stream:stream xmlns='jabber:client' xmlns:stream='http://etherx.jabber.org/streams' "+
		"id='%s' from='%s' version='1.0' xml:lang='en'><stream:features>%s</stream:features>", rand.Text(), domain, features)
	return err
}

// next is the client's next element; io.EOF when it closed the stream.
func (s *stream) next() (elem, error) {
	for {
		t, err := s.d.Token()
		if err != nil {
			return elem{}, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			var e elem
			err := s.d.DecodeElement(&e, &t)
			return e, err
		case xml.EndElement: // </stream:stream>
			io.WriteString(s.conn, "</stream:stream>")
			return elem{}, io.EOF
		}
	}
}

func (s *stream) send(format string, a ...any) error {
	_, err := fmt.Fprintf(s.conn, format, a...)
	return err
}

func esc(v string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(v))
	return b.String()
}

// session runs one client: TLS, the password, a resource, then stanzas.
func session(c net.Conn, tc *tls.Config) error {
	s := &stream{conn: c}
	if err := s.open("<starttls xmlns='urn:ietf:params:xml:ns:xmpp-tls'><required/></starttls>"); err != nil {
		return err
	}
	e, err := s.next()
	if err != nil {
		return err
	}
	if e.XMLName.Local != "starttls" {
		return s.send("<stream:error><policy-violation xmlns='urn:ietf:params:xml:ns:xmpp-streams'/></stream:error></stream:stream>")
	}
	s.send("<proceed xmlns='urn:ietf:params:xml:ns:xmpp-tls'/>")
	tlsConn := tls.Server(c, tc)
	if err := tlsConn.Handshake(); err != nil {
		return err
	}
	s.conn = tlsConn

	// SASL PLAIN, tried as often as the client likes
	if err := s.open("<mechanisms xmlns='urn:ietf:params:xml:ns:xmpp-sasl'><mechanism>PLAIN</mechanism></mechanisms>"); err != nil {
		return err
	}
	user := ""
	for user == "" {
		e, err := s.next()
		if err != nil {
			return err
		}
		if e.XMLName.Local != "auth" || e.attr("mechanism") != "PLAIN" {
			s.send("<failure xmlns='urn:ietf:params:xml:ns:xmpp-sasl'><invalid-mechanism/></failure>")
			continue
		}
		if u, ok := checkPlain(e.Inner); ok {
			user = u
			s.send("<success xmlns='urn:ietf:params:xml:ns:xmpp-sasl'/>")
		} else {
			s.send("<failure xmlns='urn:ietf:params:xml:ns:xmpp-sasl'><not-authorized/></failure>")
		}
	}

	if err := s.open("<bind xmlns='urn:ietf:params:xml:ns:xmpp-bind'/>"); err != nil {
		return err
	}
	me := user + "@" + domain
	for {
		e, err := s.next()
		if err != nil {
			return err
		}
		switch e.XMLName.Local {
		case "iq":
			err = s.iq(e, me)
		case "presence":
			// joining a group chat: the room answers with our own presence there (status 110), as a MUC service does
			if to := e.attr("to"); strings.Contains(e.Inner, "http://jabber.org/protocol/muc") && strings.Contains(to, "/") {
				err = s.send("<presence from='%s' to='%s/fake'><x xmlns='http://jabber.org/protocol/muc#user'><item affiliation='none' role='participant'/>"+
					"<status code='110'/></x></presence>", esc(to), esc(me))
			}
		}
		if err != nil {
			return err
		}
	}
}

// checkPlain reads a PLAIN response ([authzid] NUL user NUL password) and checks it.
func checkPlain(b64 string) (string, bool) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(b), "\x00")
	if len(parts) != 3 {
		return "", false
	}
	user, _, _ := strings.Cut(parts[1], "@")
	pw, ok := Users[user]
	return user, ok && pw == parts[2]
}

// iq answers resource binding, the session request (old clients), the roster (empty); others get service-unavailable.
func (s *stream) iq(e elem, me string) error {
	id, typ := esc(e.attr("id")), e.attr("type")
	if typ == "result" || typ == "error" {
		return nil
	}
	switch {
	case strings.Contains(e.Inner, "urn:ietf:params:xml:ns:xmpp-bind"):
		return s.send("<iq type='result' id='%s'><bind xmlns='urn:ietf:params:xml:ns:xmpp-bind'><jid>%s/%s</jid></bind></iq>", id, esc(me), rand.Text()[:8])
	case strings.Contains(e.Inner, "urn:ietf:params:xml:ns:xmpp-session"):
		return s.send("<iq type='result' id='%s'/>", id)
	case strings.Contains(e.Inner, "jabber:iq:roster") && typ == "get":
		return s.send("<iq type='result' id='%s' to='%s'><query xmlns='jabber:iq:roster'/></iq>", id, esc(me))
	}
	return s.send("<iq type='error' id='%s'><error type='cancel'><service-unavailable xmlns='urn:ietf:params:xml:ns:xmpp-stanzas'/></error></iq>", id)
}
