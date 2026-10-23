// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/store"
)

func TestVault(t *testing.T) {
	dir := t.TempDir()
	v, err := OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := v.Seal([]byte("hunter2"))
	if strings.Contains(string(s), "hunter2") {
		t.Fatal("the sealed secret contains the plaintext")
	}
	if p, err := v.Open(s); err != nil || string(p) != "hunter2" {
		t.Fatal("round trip", err)
	}
	v2, _ := OpenVault(dir) // the key file is reused
	if p, err := v2.Open(s); err != nil || string(p) != "hunter2" {
		t.Fatal("a second open with the same key file", err)
	}
	s[len(s)-1] ^= 1
	if _, err := v.Open(s); err == nil {
		t.Fatal("a tampered secret was accepted")
	}
}

func TestDuplicates(t *testing.T) {
	cs := []proto.Contact{
		{Href: "/x/a.vcf", Name: "Anna Müller", Phones: []proto.Labeled{{Value: "+49 171 2345678"}}},
		{Href: "/x/b.vcf", Name: "Müller, Anna", Phones: []proto.Labeled{{Value: "0171 2345678"}}, Emails: []proto.Labeled{{Value: "Anna@Example.org"}}},
		{Href: "/x/c.vcf", Name: "A. Mueller", Emails: []proto.Labeled{{Value: "anna@example.org"}}},
		{Href: "/x/d.vcf", Name: "Bob", Phones: []proto.Labeled{{Value: "12345"}}},
		{Href: "/x/e.vcf", Name: "Robert", Phones: []proto.Labeled{{Value: "12345"}}},
	}
	got := Duplicates(cs)
	has := func(a, b, why string) bool {
		for _, s := range got {
			if (s.A == a && s.B == b || s.A == b && s.B == a) && strings.Contains(s.Reason, why) {
				return true
			}
		}
		return false
	}
	if !has("a", "b", "phone") || !has("b", "c", "email") {
		t.Fatalf("missing suggestions: %+v", got)
	}
	for _, s := range got {
		if s.A == "d" || s.B == "d" {
			t.Fatalf("a short number must not match: %+v", s)
		}
	}
	m := Merge(cs[0], cs[1])
	if len(m.Phones) != 1 || len(m.Emails) != 1 || m.Name != "Anna Müller" {
		t.Fatalf("merge: %+v", m)
	}
}

type fakeHubDAV struct {
	puts map[string]string
	dels []string
}

func (f *fakeHubDAV) PutRaw(kind, name, body string) error { f.puts[kind+"/"+name] = body; return nil }
func (f *fakeHubDAV) DeleteRaw(kind, name string) error {
	f.dels = append(f.dels, kind+"/"+name)
	return nil
}

func TestSyncDAV(t *testing.T) {
	cards := map[string]string{
		"/book/1.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u-1\r\nFN:One\r\nEND:VCARD\r\n",
		"/book/2.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u-2\r\nFN:Two\r\nEND:VCARD\r\n",
	}
	etag := map[string]string{"/book/1.vcf": "\"a1\"", "/book/2.vcf": "\"b1\""}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "pw" {
			w.WriteHeader(401)
			return
		}
		if r.Method == "PROPFIND" {
			w.WriteHeader(207)
			fmt.Fprint(w, "<multistatus xmlns=\"DAV:\"><response><href>/book/</href><propstat><prop><resourcetype><collection/></resourcetype></prop></propstat></response>")
			for h := range cards {
				fmt.Fprintf(w, "<response><href>%s</href><propstat><prop><getetag>%s</getetag><resourcetype/></prop></propstat></response>", h, etag[h])
			}
			fmt.Fprint(w, "</multistatus>")
			return
		}
		if b, ok := cards[r.URL.Path]; ok {
			fmt.Fprint(w, b)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hub := &fakeHubDAV{puts: map[string]string{}}
	cfg := DAVCfg{URL: srv.URL + "/book/", User: "u"}
	if _, err := TestDAV(context.Background(), cfg, "wrong"); err == nil {
		t.Fatal("a wrong password was accepted")
	}
	ch, rm, err := SyncDAV(context.Background(), "acc9", "card", cfg, "pw", hub, st)
	if err != nil || ch != 2 || rm != 0 || len(hub.puts) != 2 {
		t.Fatalf("first: %d %d %v %v", ch, rm, err, hub.puts)
	}
	if _, ok := hub.puts["card/acc9-u-1.vcf"]; !ok {
		t.Fatalf("hub names: %v", hub.puts)
	}
	if l := st.Links("acc9-u-1"); len(l) != 1 || l[0].Source != "acc9" || l[0].EntryID != "/book/1.vcf" {
		t.Fatalf("links: %+v", l)
	}
	if ch, _, _ := SyncDAV(context.Background(), "acc9", "card", cfg, "pw", hub, st); ch != 0 {
		t.Fatalf("nothing changed, yet %d were written", ch)
	}
	etag["/book/2.vcf"] = "\"b2\""
	cards["/book/2.vcf"] = strings.Replace(cards["/book/2.vcf"], "Two", "Two Changed", 1)
	delete(cards, "/book/1.vcf")
	ch, rm, err = SyncDAV(context.Background(), "acc9", "card", cfg, "pw", hub, st)
	if err != nil || ch != 1 || rm != 1 || len(hub.dels) != 1 || hub.dels[0] != "card/acc9-u-1.vcf" {
		t.Fatalf("third: %d %d %v %v", ch, rm, err, hub.dels)
	}
	if !strings.Contains(hub.puts["card/acc9-u-2.vcf"], "Two Changed") {
		t.Fatal("the changed card was not pushed")
	}
}
