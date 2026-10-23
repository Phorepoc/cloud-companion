// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/phorepoc/phecloud/internal/store"
)

type fakeCard struct {
	body string
	rev  int
}

type fakeDevice struct {
	mu    sync.Mutex
	cards map[string]*fakeCard
}

func (f *fakeDevice) etag(c *fakeCard) string { return fmt.Sprintf("\"r%d\"", c.rev) }

func (f *fakeDevice) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.cards[r.URL.Path]
	switch r.Method {
	case "GET":
		if c == nil {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", f.etag(c))
		io.WriteString(w, c.body)
	case "PUT":
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("If-None-Match") == "*" && c != nil {
			w.WriteHeader(412)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" && (c == nil || im != f.etag(c)) {
			w.WriteHeader(412)
			return
		}
		if c == nil {
			c = &fakeCard{}
			f.cards[r.URL.Path] = c
		}
		c.body, c.rev = string(b), c.rev+1
		w.Header().Set("ETag", f.etag(c))
		w.WriteHeader(204)
	case "DELETE":
		if c == nil {
			w.WriteHeader(404)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" && im != f.etag(c) {
			w.WriteHeader(412)
			return
		}
		delete(f.cards, r.URL.Path)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func card(uid, fn, tel, email string) string {
	s := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + fn + "\r\nN:;" + fn + ";;;\r\nTEL;TYPE=CELL:" + tel + "\r\n"
	if email != "" {
		s += "EMAIL;TYPE=HOME:" + email + "\r\n"
	}
	return s + "X-CUSTOM:keep-me\r\nEND:VCARD\r\n"
}

func TestDeviceMerge(t *testing.T) {
	dev := &fakeDevice{cards: map[string]*fakeCard{
		"/book/a.vcf": {body: card("a", "Anna Mueller", "+49 171 5550101", ""), rev: 1},
		"/book/b.vcf": {body: card("b", "A. Mueller", "0171 5550101", "anna@example.org"), rev: 1},
	}}
	srv := httptest.NewServer(dev)
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx, cfg := context.Background(), DAVCfg{URL: srv.URL + "/book/"}
	origA, origB := dev.cards["/book/a.vcf"].body, dev.cards["/book/b.vcf"].body

	// preview: shows what changes, writes nothing
	p, err := PreviewDeviceMerge(ctx, st, "acc", cfg, "", "/book/a.vcf", "/book/b.vcf")
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(p.Changes, " | ")
	if !strings.Contains(all, "anna@example.org") || !strings.Contains(all, "is deleted") || strings.Contains(all, "gains the phone number") {
		t.Fatalf("preview sentences: %s", all)
	}
	if dev.cards["/book/a.vcf"].body != origA || dev.cards["/book/b.vcf"].body != origB {
		t.Fatal("a preview changed the device")
	}

	// the device changes entry A meanwhile: apply must refuse and leave everything as it is
	dev.cards["/book/a.vcf"].rev++
	if err := ApplyDeviceMerge(ctx, st, p.ID, cfg, ""); err == nil || !strings.Contains(err.Error(), "changed on the device") {
		t.Fatalf("a concurrent change must stop the merge: %v", err)
	}
	if dev.cards["/book/b.vcf"] == nil {
		t.Fatal("entry B was deleted although the merge was refused")
	}

	// preview again, apply: A carries both, B is gone, unknown properties survive
	p, _ = PreviewDeviceMerge(ctx, st, "acc", cfg, "", "/book/a.vcf", "/book/b.vcf")
	if err := ApplyDeviceMerge(ctx, st, p.ID, cfg, ""); err != nil {
		t.Fatal(err)
	}
	a := dev.cards["/book/a.vcf"].body
	if !strings.Contains(a, "anna@example.org") || !strings.Contains(a, "X-CUSTOM:keep-me") || dev.cards["/book/b.vcf"] != nil {
		t.Fatalf("after apply: A=%q B=%v", a, dev.cards["/book/b.vcf"])
	}
	if err := ApplyDeviceMerge(ctx, st, p.ID, cfg, ""); err == nil {
		t.Fatal("a merge can only be applied once")
	}

	// undo: both original cards are back
	if err := UndoDeviceMerge(ctx, st, p.ID, cfg, ""); err != nil {
		t.Fatal(err)
	}
	if dev.cards["/book/b.vcf"] == nil || dev.cards["/book/b.vcf"].body != origB || !strings.Contains(dev.cards["/book/a.vcf"].body, "Anna Mueller") || strings.Contains(dev.cards["/book/a.vcf"].body, "anna@example.org") {
		t.Fatalf("after undo: A=%q B=%+v", dev.cards["/book/a.vcf"].body, dev.cards["/book/b.vcf"])
	}
	if err := UndoDeviceMerge(ctx, st, p.ID, cfg, ""); err == nil {
		t.Fatal("an undo can only be done once")
	}
}
