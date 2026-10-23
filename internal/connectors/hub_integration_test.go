// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package connectors

// End to end against a REAL hub (Stalwart) and a REAL CardDAV/CalDAV server (Radicale). Needs the hub's environment
// (PHECLOUD_HUB_URL, _OWNER, _TOKEN) and RADICALE_URL (default http://192.168.2.247:5232), RADICALE_USER/RADICALE_PASS (phone/phonepass).
// Run: web/test/hubtest-run.sh  (golang container on the throwaway hub's network).

import (
	"bytes"
	"context"
	"github.com/emersion/go-imap/v2"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/phecloud/internal/davhub"
	"github.com/phorepoc/phecloud/internal/store"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func rad(t *testing.T, method, p, body string, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, env("RADICALE_URL", "http://192.168.2.247:5232")+p, strings.NewReader(body))
	req.SetBasicAuth(env("RADICALE_USER", "phone"), env("RADICALE_PASS", "phonepass"))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := make([]byte, 1<<16)
	n, _ := resp.Body.Read(b)
	return resp.StatusCode, string(b[:n]), resp.Header
}

func TestHubEndToEnd(t *testing.T) {
	hub := davhub.FromEnv()
	if hub == nil {
		t.Skip("no hub in the environment")
	}
	ctx := context.Background()
	st := store.OpenTest(t)

	// ---- 1. IMAP into the hub cache ----
	icfg, u := startIMAP(t)
	for i := 1; i <= 3; i++ {
		raw := msg("e2e"+string(rune('0'+i)), "End to end "+string(rune('0'+i)))
		if _, err := u.Append("INBOX", lit{bytes.NewReader(raw)}, &imap.AppendOptions{Time: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	mc := MailCfg{IMAP: icfg, Months: 3}
	n, err := SyncIMAP(ctx, "accM", "E2E", mc, "secret", hub, st, 100)
	if err != nil || n != 3 {
		t.Fatalf("imap -> hub: %d %v", n, err)
	}
	fl, err := hub.MailFolders()
	if err != nil {
		t.Fatal(err)
	}
	fid := ""
	for _, f := range fl {
		if f.Name == "E2E / INBOX" {
			fid = f.ID
		}
	}
	if fid == "" {
		t.Fatalf("the hub has no folder 'E2E / INBOX': %+v", fl)
	}
	items, total, err := hub.MailList(fid, 0)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("hub folder holds %d (%v)", total, err)
	}
	body, err := hub.MailRead(items[0].ID)
	if err != nil || !strings.Contains(body.Text, "body of End to end") {
		t.Fatalf("hub mail body: %+v %v", body, err)
	}
	if n, _ := SyncIMAP(ctx, "accM", "E2E", mc, "secret", hub, st, 100); n != 0 {
		t.Fatalf("a second sync imported %d again", n)
	}
	t.Log("IMAP import into Stalwart: 3 messages, readable, no repeats")

	// ---- 2. CardDAV + CalDAV pull from Radicale ----
	card, cal := "/phone/phe-r22-card/", "/phone/phe-r22-cal/"
	for _, p := range []string{card, cal} {
		rad(t, "DELETE", p, "", nil)
	}
	defer func() {
		for _, p := range []string{card, cal} {
			rad(t, "DELETE", p, "", nil)
		}
	}()
	mk := func(p, typ string) {
		b := `<?xml version="1.0"?><mkcol xmlns="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav" xmlns:C="urn:ietf:params:xml:ns:caldav"><set><prop><resourcetype><collection/>` + typ + `</resourcetype></prop></set></mkcol>`
		if c, _, _ := rad(t, "MKCOL", p, b, map[string]string{"Content-Type": "application/xml"}); c != 201 {
			t.Fatalf("MKCOL %s -> %d", p, c)
		}
	}
	mk(card, "<CR:addressbook/>")
	mk(cal, "<C:calendar/>")
	put := func(p, body, ct string) {
		if c, _, _ := rad(t, "PUT", p, body, map[string]string{"Content-Type": ct}); c/100 != 2 {
			t.Fatalf("PUT %s -> %d", p, c)
		}
	}
	vc := func(uid, fn, tel string) string {
		return "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + fn + "\r\nN:;" + fn + ";;;\r\nTEL;TYPE=CELL:" + tel + "\r\nEND:VCARD\r\n"
	}
	put(card+"a.vcf", vc("e2e-a", "Anna Mueller", "+49 171 5550101"), "text/vcard")
	put(card+"b.vcf", vc("e2e-b", "A. Mueller", "0171 5550101"), "text/vcard")
	put(cal+"ev.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:e2e-ev@test\r\nDTSTAMP:20261023T100000Z\r\nDTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\nSUMMARY:E2E event\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", "text/calendar")
	dc := DAVCfg{URL: env("RADICALE_URL", "http://192.168.2.247:5232") + card, User: env("RADICALE_USER", "phone")}
	pw := env("RADICALE_PASS", "phonepass")
	ch, rm, err := SyncDAV(ctx, "accC", "card", dc, pw, hub, st)
	if err != nil || ch != 2 || rm != 0 {
		t.Fatalf("carddav -> hub: %d %d %v", ch, rm, err)
	}
	cc := DAVCfg{URL: env("RADICALE_URL", "http://192.168.2.247:5232") + cal, User: dc.User}
	if ch, _, err := SyncDAV(ctx, "accK", "cal", cc, pw, hub, st); err != nil || ch != 1 {
		t.Fatalf("caldav -> hub: %d %v", ch, err)
	}
	defer hub.DeleteRaw("cal", "accK-e2e-ev_test.ics")
	evs, err := hub.Events("2026-10-30", "2026-11-03")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Title == "E2E event" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the event is not in the hub: %+v", evs)
	}
	t.Log("CardDAV and CalDAV pulled from Radicale into Stalwart")

	// ---- 3. duplicates and the merge on real hub contacts ----
	var ids []string
	cs, _ := hub.Contacts()
	for _, c := range cs {
		if strings.HasPrefix(HubID(c.Href), "accC-") {
			ids = append(ids, HubID(c.Href))
		}
	}
	defer func() {
		for _, id := range ids {
			hub.DeleteRaw("card", id+".vcf")
		}
	}()
	var pair *Suggestion
	for _, s := range Duplicates(cs) {
		if strings.HasPrefix(s.A, "accC-") && strings.HasPrefix(s.B, "accC-") {
			s := s
			pair = &s
		}
	}
	if pair == nil || !strings.Contains(pair.Reason, "phone") {
		t.Fatalf("the duplicate finder did not see the two cards: %+v", pair)
	}
	before := map[string]string{}
	_, bx, _ := rad(t, "GET", card+"a.vcf", "", nil)
	_, by, _ := rad(t, "GET", card+"b.vcf", "", nil)
	before["a"], before["b"] = bx, by
	if _, err := MergeHub(hub, st, pair.A, pair.B); err != nil {
		t.Fatal(err)
	}
	cs2, _ := hub.Contacts()
	n1 := 0
	for _, c := range cs2 {
		if strings.HasPrefix(HubID(c.Href), "accC-") {
			n1++
		}
	}
	if n1 != 1 {
		t.Fatalf("after the merge the hub has %d of the two contacts, want 1", n1)
	}
	if l := st.Links(pair.A); len(l) != 2 {
		t.Fatalf("both source entries must link to the kept contact: %+v", l)
	}
	// the sources are untouched and a later sync does not bring the dropped contact back or rewrite anything at the source
	_, ax, _ := rad(t, "GET", card+"a.vcf", "", nil)
	_, ay, _ := rad(t, "GET", card+"b.vcf", "", nil)
	if ax != before["a"] || ay != before["b"] {
		t.Fatal("a source entry was changed by the merge")
	}
	if c, _, _ := rad(t, "HEAD", card+"b.vcf", "", nil); c != 200 {
		t.Fatalf("a source entry is gone (HTTP %d)", c)
	}
	// make the source entry look changed (new etag) : the sync must respect the merge
	put(card+"b.vcf", vc("e2e-b", "A. Mueller", "0171 5550101")+"", "text/vcard")
	if _, _, err := SyncDAV(ctx, "accC", "card", dc, pw, hub, st); err != nil {
		t.Fatal(err)
	}
	cs3, _ := hub.Contacts()
	n2 := 0
	for _, c := range cs3 {
		if strings.HasPrefix(HubID(c.Href), "accC-") {
			n2++
		}
	}
	if n2 != 1 {
		t.Fatalf("a later sync brought the merged contact back (%d)", n2)
	}
	t.Log("merge on real hub contacts: links moved, sources untouched, no resurrection")
	_ = time.Second
}
