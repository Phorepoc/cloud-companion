// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/store"
)

type fakeGraph struct {
	t      *testing.T
	base   string
	unauth bool
}

func (g *fakeGraph) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if g.unauth || r.Header.Get("Authorization") != "Bearer tokG" {
		w.WriteHeader(401)
		return
	}
	if !strings.Contains(r.Header.Get("Prefer"), "outlook.timezone=\"UTC\"") {
		g.t.Errorf("the Prefer header asks for UTC times: %q", r.Header.Get("Prefer"))
	}
	q := r.URL.Query()
	switch r.URL.Path {
	case "/v1.0/me/contacts":
		fmt.Fprint(w, `{"value":[{"id":"c0"}]}`)
	case "/v1.0/me/contacts/delta":
		switch {
		case q.Get("$deltatoken") == "gone":
			w.WriteHeader(410)
			fmt.Fprint(w, `{"error":{"code":"syncStateNotFound"}}`)
		case q.Get("$deltatoken") == "d1": // changes since the first full read
			fmt.Fprintf(w, `{"value":[{"id":"B=","displayName":"Bob Marsh","mobilePhone":"+358 40 111"},{"id":"A","@removed":{"reason":"deleted"}}],"@odata.deltaLink":"%s/v1.0/me/contacts/delta?$deltatoken=d2"}`, g.base)
		case q.Get("$skiptoken") == "p2":
			fmt.Fprintf(w, `{"value":[{"id":"C","displayName":"Cy","emailAddresses":[{"address":"cy@example.org"}]}],"@odata.deltaLink":"%s/v1.0/me/contacts/delta?$deltatoken=d1"}`, g.base)
		default:
			fmt.Fprintf(w, `{"value":[{"id":"A","displayName":"Ann Lee","givenName":"Ann","surname":"Lee","companyName":"Acme","personalNotes":"likes tea","mobilePhone":"+49 171 5550101","homePhones":["030 1234"],"businessPhones":["030 9999"],"emailAddresses":[{"address":"ann@example.org"}]},{"id":"B=","displayName":"Bob"}],"@odata.nextLink":"%s/v1.0/me/contacts/delta?$skiptoken=p2"}`, g.base)
		}
	case "/v1.0/me/calendars":
		fmt.Fprint(w, `{"value":[{"id":"k0"}]}`)
	case "/v1.0/me/calendarView/delta":
		if q.Get("startDateTime") == "" && q.Get("$deltatoken") == "" {
			g.t.Error("calendarView needs a window")
		}
		if q.Get("$deltatoken") == "dc1" {
			fmt.Fprintf(w, `{"value":[{"id":"E1","@removed":{"reason":"deleted"}}],"@odata.deltaLink":"%s/v1.0/me/calendarView/delta?$deltatoken=dc2"}`, g.base)
			return
		}
		fmt.Fprintf(w, `{"value":[
		 {"id":"E1","subject":"Planning, Q4; kickoff","isAllDay":false,"start":{"dateTime":"2026-11-01T09:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-11-01T10:30:00.0000000","timeZone":"UTC"},"location":{"displayName":"Room 4"},"bodyPreview":"Agenda attached","body":{"contentType":"text","content":"Line one\nLine two"}},
		 {"id":"E2","subject":"Holiday","isAllDay":true,"start":{"dateTime":"2026-11-02T00:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-11-03T00:00:00.0000000","timeZone":"UTC"}},
		 {"id":"E3","subject":"Cancelled thing","isCancelled":true,"start":{"dateTime":"2026-11-04T09:00:00.0000000"},"end":{"dateTime":"2026-11-04T10:00:00.0000000"}}],
		 "@odata.deltaLink":"%s/v1.0/me/calendarView/delta?$deltatoken=dc1"}`, g.base)
	default:
		w.WriteHeader(404)
	}
}

func TestGraphSync(t *testing.T) {
	g := &fakeGraph{t: t}
	srv := httptest.NewServer(g)
	defer srv.Close()
	g.base = srv.URL
	t.Setenv("PHECLOUD_GRAPH_URL", srv.URL+"/v1.0")
	st := store.OpenTest(t)
	hub := &fakeHubDAV{puts: map[string]string{}}
	ctx, cfg := context.Background(), DAVCfg{URL: srv.URL + "/v1.0/me/contacts", User: "me@outlook.test", OAuth: "microsoft"}
	tok := Cred("tokG")

	if _, err := TestGraph(ctx, cfg, tok); err != nil {
		t.Fatal("test:", err)
	}
	if _, err := TestGraph(ctx, cfg, Cred("stale")); err == nil || !strings.Contains(err.Error(), "sign in again") {
		t.Fatalf("a bad token must say what to do: %v", err)
	}

	// 1. the first run reads everything (two pages) and keeps the delta link
	ch, rm, err := SyncGraph(ctx, "acc", "card", cfg, tok, hub, st)
	if err != nil || ch != 3 || rm != 0 || len(hub.puts) != 3 {
		t.Fatalf("contacts, first run: %d %d %v %v", ch, rm, err, hub.puts)
	}
	ann := hub.puts["card/acc-A.vcf"]
	for _, want := range []string{"FN:Ann Lee", "ann@example.org", "+49 171 5550101", "030 1234", "030 9999", "Acme", "likes tea", "UID:msgraph-A"} {
		if !strings.Contains(ann, want) {
			t.Errorf("the vCard lacks %q:\n%s", want, ann)
		}
	}
	if _, ok := hub.puts["card/acc-B_.vcf"]; !ok {
		t.Fatalf("an id with odd characters gets a safe hub name: %v", hub.puts)
	}
	if l := st.Links("acc-A"); len(l) != 1 || l[0].Source != "acc" || l[0].EntryID != "graph:A" {
		t.Fatalf("link: %+v", l)
	}
	if d, _ := st.SyncedGet("acc", "graph:delta:card"); !strings.HasSuffix(d, "$deltatoken=d1") {
		t.Fatalf("delta link kept: %q", d)
	}
	// 2. the next run asks only for changes: Bob renamed, Ann removed
	ch, rm, err = SyncGraph(ctx, "acc", "card", cfg, tok, hub, st)
	if err != nil || ch != 1 || rm != 1 {
		t.Fatalf("contacts, delta: %d %d %v", ch, rm, err)
	}
	if !strings.Contains(hub.puts["card/acc-B_.vcf"], "FN:Bob Marsh") {
		t.Fatal("the changed contact was not rewritten")
	}
	if len(hub.dels) != 1 || hub.dels[0] != "card/acc-A.vcf" || len(st.Links("acc-A")) != 0 {
		t.Fatalf("the removed contact: dels=%v links=%v", hub.dels, st.Links("acc-A"))
	}
	// 3. Graph forgot the delta state (410): start over
	st.SyncedSet("acc", "graph:delta:card", srv.URL+"/v1.0/me/contacts/delta?$deltatoken=gone")
	if ch, _, err := SyncGraph(ctx, "acc", "card", cfg, tok, hub, st); err != nil || ch != 3 {
		t.Fatalf("after 410: %d %v", ch, err)
	}

	// calendar
	ccfg := DAVCfg{URL: srv.URL + "/v1.0/me/calendarView", User: "me@outlook.test", OAuth: "microsoft"}
	ch, rm, err = SyncGraph(ctx, "cal1", "cal", ccfg, tok, hub, st)
	if err != nil || ch != 2 || rm != 0 {
		t.Fatalf("calendar, first run (the cancelled event is not copied): %d %d %v", ch, rm, err)
	}
	e1 := hub.puts["cal/cal1-E1.ics"]
	for _, want := range []string{"DTSTART:20261101T090000Z", "DTEND:20261101T103000Z", "SUMMARY:Planning\\, Q4\\; kickoff", "LOCATION:Room 4", "DESCRIPTION:Line one\\nLine two", "UID:msgraph-E1"} {
		if !strings.Contains(e1, want) {
			t.Errorf("the event lacks %q:\n%s", want, e1)
		}
	}
	if e2 := hub.puts["cal/cal1-E2.ics"]; !strings.Contains(e2, "DTSTART;VALUE=DATE:20261102") || !strings.Contains(e2, "DTEND;VALUE=DATE:20261103") {
		t.Errorf("an all-day event is a DATE: %s", e2)
	}
	ch, rm, err = SyncGraph(ctx, "cal1", "cal", ccfg, tok, hub, st)
	if err != nil || ch != 0 || rm != 1 || hub.dels[len(hub.dels)-1] != "cal/cal1-E1.ics" {
		t.Fatalf("calendar, delta: %d %d %v %v", ch, rm, err, hub.dels)
	}
	// a failing page leaves the delta link where it was (nothing is skipped next time)
	g.unauth = true
	before, _ := st.SyncedGet("cal1", "graph:delta:cal")
	if _, _, err := SyncGraph(ctx, "cal1", "cal", ccfg, tok, hub, st); err == nil {
		t.Fatal("a refused login must be an error")
	}
	if after, _ := st.SyncedGet("cal1", "graph:delta:cal"); after != before {
		t.Fatal("the delta link moved although the run failed")
	}
}
