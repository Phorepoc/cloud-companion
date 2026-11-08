// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package davhub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/proto"
)

func TestVCardRoundTrip(t *testing.T) {
	src := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:abc\r\nFN:Ada Lovelace\r\nN:Lovelace;Ada;;;\r\nORG:Analytical\\, Ltd\r\nTEL;TYPE=CELL,VOICE:+44 1\r\nEMAIL;TYPE=INTERNET,WORK:ada@example.org\r\n" +
		"X-CUSTOM:keep me\r\nNOTE:line one\\nline two\r\nPHOTO;ENCODING=b:AAAA\r\n BBBB\r\nEND:VCARD\r\n"
	c, uid, extra := parseVCard(src)
	if uid != "abc" || c.Name != "Ada Lovelace" || c.Family != "Lovelace" || c.Given != "Ada" || c.Org != "Analytical, Ltd" || c.Note != "line one\nline two" {
		t.Fatalf("parsed %+v uid %q", c, uid)
	}
	if len(c.Phones) != 1 || c.Phones[0].Type != "cell" || c.Emails[0].Type != "work" {
		t.Fatalf("labels %+v %+v", c.Phones, c.Emails)
	}
	c.Name = "Ada King"
	out := writeVCard(c, uid, extra)
	for _, want := range []string{"FN:Ada King", "UID:abc", "X-CUSTOM:keep me", "PHOTO;ENCODING=b:AAAABBBB", "TEL;TYPE=CELL:+44 1", "ORG:Analytical\\, Ltd", "NOTE:line one\\nline two"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestICalEditKeepsRuleAlarmAndZone(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTIMEZONE\r\nTZID:Europe/London\r\nEND:VTIMEZONE\r\nBEGIN:VEVENT\r\nUID:e1\r\nDTSTART;TZID=Europe/London:20261024T090000\r\n" +
		"DTEND;TZID=Europe/London:20261024T100000\r\nSUMMARY:Stand-up\r\nRRULE:FREQ=WEEKLY\r\nBEGIN:VALARM\r\nTRIGGER:-PT10M\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	o := parseICal(src)
	if !o.hasEvent || o.ev.Title != "Stand-up" || o.ev.Start != "2026-10-24T08:00:00Z" || o.ev.TZ != "Europe/London" || !o.ev.Recurs || o.startTZ != "Europe/London" {
		t.Fatalf("parsed %+v", o)
	}
	e := o.ev
	e.Title, e.Start, e.End = "Stand-up (moved)", "2026-10-24T09:30:00", "2026-10-24T10:30:00"
	out := writeICal(o, e)
	for _, want := range []string{"SUMMARY:Stand-up (moved)", "DTSTART;TZID=Europe/London:20261024T093000", "RRULE:FREQ=WEEKLY", "BEGIN:VALARM", "TRIGGER:-PT10M", "BEGIN:VTIMEZONE", "END:VCALENDAR"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestICalAllDayAndNew(t *testing.T) {
	out := writeICal(ical{uid: "n1"}, proto.Event{Title: "Holiday", Start: "2026-12-25", End: "2026-12-26", AllDay: true})
	if !strings.Contains(out, "DTSTART;VALUE=DATE:20261225") || !strings.Contains(out, "BEGIN:VCALENDAR") {
		t.Fatalf("new all-day event:\n%s", out)
	}
	o := parseICal(out)
	if !o.ev.AllDay || o.ev.Start != "2026-12-25" || o.ev.Title != "Holiday" {
		t.Fatalf("re-parsed %+v", o.ev)
	}
}

// an edit made from an older copy (another device saved in between): the hub refuses it, and the message says so in plain words
func TestEditOfAChangedContactSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u1\r\nFN:Ann\r\nEND:VCARD\r\n"))
			return
		}
		w.WriteHeader(http.StatusPreconditionFailed)
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Owner: "owner@hub.test", Pass: "x", HTTP: srv.Client()}
	_, err := c.SaveContact(proto.Contact{Href: c.base("card") + "u1.vcf", ETag: `"old"`, Name: "Ann B"})
	if err == nil || !strings.Contains(err.Error(), "changed on another device") {
		t.Fatalf("a stale edit: %v", err)
	}
}
