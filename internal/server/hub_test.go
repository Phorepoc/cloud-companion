// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phorepoc/phecloud/internal/davhub"
	"github.com/phorepoc/phecloud/internal/proto"
)

type fakeIO struct{ sent []any }

func (f *fakeIO) Read() (string, []byte, error) { return "", nil, io.EOF }
func (f *fakeIO) Write(v any) error             { f.sent = append(f.sent, v); return nil }
func (f *fakeIO) Close() error                  { return nil }

const vcardA = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:a1\r\nFN:Ada Lovelace\r\nN:Lovelace;Ada;;;\r\nTEL;TYPE=CELL:+44 1\r\nEND:VCARD\r\n"
const icsA = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:e1\r\nDTSTART:20261024T090000Z\r\nDTEND:20261024T100000Z\r\nSUMMARY:Stand-up\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

// fakeHub is a tiny stand-in for Stalwart: just enough CardDAV, CalDAV and JMAP for the hub frames.
func fakeHub(puts *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", 401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "REPORT" && strings.Contains(r.URL.Path, "/dav/card/"):
			w.WriteHeader(207)
			io.WriteString(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:response><D:href>/dav/card/owner%40hub.lan/default/a1.vcf</D:href><D:propstat><D:prop><D:getetag>"e1"</D:getetag><C:address-data>`+vcardA+`</C:address-data></D:prop></D:propstat></D:response></D:multistatus>`)
		case r.Method == "REPORT" && strings.Contains(r.URL.Path, "/dav/cal/"):
			w.WriteHeader(207)
			io.WriteString(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:response><D:href>/dav/cal/owner%40hub.lan/default/e1.ics</D:href><D:propstat><D:prop><D:getetag>"c1"</D:getetag><C:calendar-data>`+icsA+`</C:calendar-data></D:prop></D:propstat></D:response></D:multistatus>`)
		case r.Method == "PUT":
			*puts = append(*puts, r.URL.Path+"\n"+string(b))
			w.Header().Set("ETag", `"new"`)
			w.WriteHeader(201)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, ".vcf"):
			io.WriteString(w, vcardA)
		case r.Method == "DELETE":
			w.WriteHeader(204)
		case r.URL.Path == "/jmap/session":
			io.WriteString(w, `{"primaryAccounts":{"urn:ietf:params:jmap:mail":"acc1"}}`)
		case r.URL.Path == "/jmap":
			var req struct {
				MethodCalls [][]json.RawMessage `json:"methodCalls"`
			}
			json.Unmarshal(b, &req)
			var name string
			json.Unmarshal(req.MethodCalls[0][0], &name)
			switch {
			case name == "x:AppPassword/get":
				io.WriteString(w, `{"methodResponses":[["x:AppPassword/get",{"list":[{"id":"p1","description":"old phone","createdAt":"2026-10-01T00:00:00Z","expiresAt":null}]},"c1"]]}`)
			case name == "x:AppPassword/set" && strings.Contains(string(b), "create"):
				io.WriteString(w, `{"methodResponses":[["x:AppPassword/set",{"created":{"a":{"id":"p2","secret":"app_SECRET"}}},"c1"]]}`)
			case name == "x:AppPassword/set":
				io.WriteString(w, `{"methodResponses":[["x:AppPassword/set",{"destroyed":["p1"]},"c1"]]}`)
			case name == "Mailbox/get":
				io.WriteString(w, `{"methodResponses":[["Mailbox/get",{"list":[{"id":"m1","name":"Inbox","role":"inbox","totalEmails":2,"unreadEmails":1}]},"m1"]]}`)
			case name == "Email/query":
				io.WriteString(w, `{"methodResponses":[["Email/query",{"total":2,"ids":["x1"]},"q1"],["Email/get",{"list":[{"id":"x1","subject":"Hello","from":[{"name":"Grace","email":"g@example.org"}],"receivedAt":"2026-10-22T10:00:00Z","preview":"Hi there","keywords":{},"hasAttachment":false}]},"g1"]]}`)
			case name == "Email/get":
				io.WriteString(w, `{"methodResponses":[["Email/get",{"list":[{"id":"x1","subject":"Hello","from":[{"name":"Grace","email":"g@example.org"}],"to":[{"email":"owner@hub.lan"}],"receivedAt":"2026-10-22T10:00:00Z","keywords":{"$seen":true},"textBody":[{"partId":"1"}],"htmlBody":[{"partId":"1"}],"bodyValues":{"1":{"value":"<html><body><p>Hi &amp; welcome</p><script>x()</script></body></html>"}}}]},"g1"]]}`)
			}
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
		}
	}))
}

func ask(t *testing.T, admin bool, hub *davhub.Client, h proto.Hub) proto.HubRes {
	t.Helper()
	fio := &fakeIO{}
	s := &session{srv: &Server{Hub: hub}, c: &conn{io: fio}, log: slog.Default(), admin: admin}
	h.T, h.ID = proto.THub, 7
	raw, _ := proto.Marshal(h)
	if err := s.hubRequest(raw); err != nil {
		t.Fatal(err)
	}
	res, ok := fio.sent[len(fio.sent)-1].(proto.HubRes)
	if !ok || res.ID != 7 {
		t.Fatalf("answer %#v", fio.sent)
	}
	return res
}

func TestHubFrames(t *testing.T) {
	var puts []string
	srv := fakeHub(&puts)
	defer srv.Close()
	hub := &davhub.Client{URL: srv.URL, Owner: "owner@hub.lan", Token: "tok", HTTP: srv.Client()}

	if r := ask(t, false, nil, proto.Hub{Op: "contacts.list"}); r.OK || !strings.Contains(r.Err, "not set up") {
		t.Errorf("no hub configured: %+v", r)
	}
	r := ask(t, false, hub, proto.Hub{Op: "contacts.list"})
	if !r.OK || len(r.Contacts) != 1 || r.Contacts[0].Name != "Ada Lovelace" || r.Contacts[0].Phones[0].Value != "+44 1" || r.Contacts[0].ETag != `"e1"` {
		t.Fatalf("contacts.list: %+v", r)
	}
	href := r.Contacts[0].Href
	c := r.Contacts[0]
	c.Org = "Analytical"
	r = ask(t, false, hub, proto.Hub{Op: "contacts.save", Contact: &c})
	if !r.OK || r.Contact == nil || r.Contact.ETag != `"new"` || len(puts) != 1 || !strings.Contains(puts[0], "ORG:Analytical") || !strings.Contains(puts[0], "UID:a1") {
		t.Fatalf("contacts.save (edit keeps the UID): %+v %v", r, puts)
	}
	r = ask(t, false, hub, proto.Hub{Op: "contacts.save", Contact: &proto.Contact{Name: "New Person"}})
	if !r.OK || !strings.HasPrefix(r.Contact.Href, "/dav/card/") || !strings.Contains(puts[1], "FN:New Person") {
		t.Fatalf("contacts.save (create): %+v", r)
	}
	if r = ask(t, false, hub, proto.Hub{Op: "contacts.delete", Href: "/etc/passwd"}); r.OK {
		t.Errorf("a foreign address must be refused: %+v", r)
	}
	if r = ask(t, false, hub, proto.Hub{Op: "contacts.delete", Href: href}); !r.OK {
		t.Errorf("contacts.delete: %+v", r)
	}

	r = ask(t, false, hub, proto.Hub{Op: "events.list", From: "2026-10-01", To: "2026-11-01"})
	if !r.OK || len(r.Events) != 1 || r.Events[0].Title != "Stand-up" || r.Events[0].Start != "2026-10-24T09:00:00Z" {
		t.Fatalf("events.list: %+v", r)
	}
	r = ask(t, false, hub, proto.Hub{Op: "events.save", Event: &proto.Event{Title: "Lunch", Start: "2026-10-25T12:00:00", End: "2026-10-25T13:00:00", TZ: "America/New_York"}})
	last := puts[len(puts)-1]
	if !r.OK || !strings.Contains(last, "DTSTART;TZID=America/New_York:20261025T120000") || !strings.Contains(last, "TZOFFSETTO:-0400") {
		t.Fatalf("events.save with a time zone: %+v\n%s", r, last)
	}

	// app passwords: admin only
	if r = ask(t, false, hub, proto.Hub{Op: "apppw.list"}); r.OK {
		t.Errorf("a non-admin device must not manage app passwords")
	}
	r = ask(t, true, hub, proto.Hub{Op: "apppw.list"})
	if !r.OK || len(r.AppPws) != 1 || r.AppPws[0].Name != "old phone" || r.Owner != "owner@hub.lan" {
		t.Fatalf("apppw.list: %+v", r)
	}
	r = ask(t, true, hub, proto.Hub{Op: "apppw.add", Name: "tablet", Days: 30})
	if !r.OK || r.Secret != "app_SECRET" || r.AppPws[0].ID != "p2" {
		t.Fatalf("apppw.add: %+v", r)
	}
	if r = ask(t, true, hub, proto.Hub{Op: "apppw.add", Name: " "}); r.OK {
		t.Errorf("an unnamed app password must be refused")
	}
	if r = ask(t, true, hub, proto.Hub{Op: "apppw.revoke", ID2: "p1"}); !r.OK {
		t.Errorf("apppw.revoke: %+v", r)
	}

	// mail, read-only
	r = ask(t, false, hub, proto.Hub{Op: "mail.folders"})
	if !r.OK || len(r.Folders) != 1 || r.Folders[0].Role != "inbox" || r.Folders[0].Unread != 1 {
		t.Fatalf("mail.folders: %+v", r)
	}
	r = ask(t, false, hub, proto.Hub{Op: "mail.list", Folder: "m1"})
	if !r.OK || r.Total != 2 || len(r.Mails) != 1 || r.Mails[0].From != "Grace" || !r.Mails[0].Unread {
		t.Fatalf("mail.list: %+v", r)
	}
	r = ask(t, false, hub, proto.Hub{Op: "mail.read", ID2: "x1"})
	if !r.OK || r.Mail == nil || r.Mail.Text != "Hi & welcome" || !r.Mail.HTML || r.Mail.Unread {
		t.Fatalf("mail.read (HTML turned into text, scripts dropped): %+v", r.Mail)
	}
}
