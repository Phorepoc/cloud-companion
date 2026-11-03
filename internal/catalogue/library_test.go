// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package catalogue

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/pkginfo"
	"github.com/phorepoc/cloud-companion/internal/store"
)

func newLib(t *testing.T) (*Manager, string) {
	st := store.OpenTest(t)
	m := New(st, t.TempDir())
	m.Library = t.TempDir()
	if _, err := st.CatalogueAdd(store.Catalogue{ID: "mine", Name: "Mine", Public: true, Priority: 100}); err != nil {
		t.Fatal(err)
	}
	return m, m.Library
}

func put(t *testing.T, dir, name string, b []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Duration(len(b)+int(time.Now().UnixNano()%1000)) * time.Second) // a distinct mtime for a changed file
	os.Chtimes(p, later, later)
}

func entry(t *testing.T, m *Manager, path string) store.CatFile {
	t.Helper()
	for _, f := range m.St.CatFiles() {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no entry for %s", path)
	return store.CatFile{}
}

func TestScanAddChangeRemove(t *testing.T) {
	m, dir := newLib(t)
	ctx := context.Background()
	put(t, dir, "demo.sisx", pkginfo.ExampleSIS(0xE1111111, "Demo", "Acme", [3]int32{1, 0, 0}, "v1"))
	put(t, dir, "sub/droid.apk", pkginfo.ExampleAPK())
	put(t, dir, "broken.sis", []byte("not a package at all, really"))
	put(t, dir, "notes.txt", []byte("ignored"))
	put(t, dir, "tool.zip", []byte("PK zip without metadata"))
	r, err := m.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 4 || r.Total != 4 || len(r.Unreadable) != 1 {
		t.Fatalf("%+v", r)
	}
	demo := entry(t, m, "demo.sisx")
	rel, err := m.Release(demo)
	if err != nil || rel.AppID != "uid-e1111111" || rel.Name != "Demo" || rel.Version != "1.0" || rel.Platform != "symbian" {
		t.Fatalf("%+v %v", rel, err)
	}
	if a := entry(t, m, "sub/droid.apk"); a.Status != "ok" {
		t.Fatalf("%+v", a)
	}
	if b := entry(t, m, "broken.sis"); b.Status != "unreadable" || b.Error == "" {
		t.Fatalf("%+v", b)
	}
	if _, err := m.Release(entry(t, m, "tool.zip")); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("a zip needs a version: %v", err)
	}
	// a second scan with nothing changed changes nothing
	if r, _ = m.Scan(ctx); len(r.Added)+len(r.Changed)+len(r.Removed) != 0 {
		t.Fatalf("%+v", r)
	}
	// publish, then edit: the edits survive a rescan
	if n, p := m.Publish("mine", []string{demo.ID}, true); n != 1 || len(p) != 0 {
		t.Fatalf("%d %v", n, p)
	}
	if err := m.SaveEdits(demo.ID, Edits{Description: "My notes", Name: "Demo Renamed", Version: "9.9"}); err != nil {
		t.Fatal(err)
	}
	m.Scan(ctx)
	rel, _ = m.Release(entry(t, m, "demo.sisx"))
	if rel.Name != "Demo Renamed" || rel.Description != "My notes" || rel.Version != "9.9" {
		t.Fatalf("%+v", rel)
	}
	// the file changes (same app): new version read, the edited version goes, the other edits stay, still published
	put(t, dir, "demo.sisx", pkginfo.ExampleSIS(0xE1111111, "Demo", "Acme", [3]int32{2, 0, 0}, "v2 is longer"))
	r, _ = m.Scan(ctx)
	if len(r.Changed) != 1 {
		t.Fatalf("%+v", r)
	}
	rel, _ = m.Release(entry(t, m, "demo.sisx"))
	if rel.Version != "2.0" || rel.Name != "Demo Renamed" || rel.Description != "My notes" {
		t.Fatalf("%+v", rel)
	}
	if _, pub := m.St.CatPublished("mine")[demo.ID]; !pub {
		t.Fatal("a changed file lost its publication")
	}
	// the file becomes another app: the edits are dropped
	put(t, dir, "demo.sisx", pkginfo.ExampleSIS(0xE2222222, "Other", "Acme", [3]int32{1, 0, 0}, "something else entirely"))
	m.Scan(ctx)
	rel, _ = m.Release(entry(t, m, "demo.sisx"))
	if rel.Name != "Other" || rel.Description != "" || rel.AppID != "uid-e2222222" {
		t.Fatalf("%+v", rel)
	}
}

func TestMissingFileRule(t *testing.T) {
	m, dir := newLib(t)
	ctx := context.Background()
	put(t, dir, "a.sisx", pkginfo.ExampleSIS(0xE1111111, "A", "V", [3]int32{1, 0, 0}, "aaa"))
	put(t, dir, "b.apk", pkginfo.ExampleAPK())
	m.Scan(ctx)
	a, b := entry(t, m, "a.sisx"), entry(t, m, "b.apk")
	m.Publish("mine", []string{a.ID, b.ID}, true)
	idx, _ := m.Index(mustCat(t, m), "http://x/catalogue/mine")
	if len(idx["items"].([]any)) != 2 {
		t.Fatalf("%v", idx["items"])
	}
	os.Remove(filepath.Join(dir, "a.sisx"))
	r, _ := m.Scan(ctx)
	if len(r.Removed) != 1 || r.Removed[0] != "a.sisx" {
		t.Fatalf("%+v", r)
	}
	a = entry(t, m, "a.sisx")
	if a.Status != "missing" || !strings.Contains(a.Error, "gone") {
		t.Fatalf("%+v", a)
	}
	if _, pub := m.St.CatPublished("mine")[a.ID]; pub {
		t.Fatal("a missing file is still published")
	}
	idx, _ = m.Index(mustCat(t, m), "http://x/catalogue/mine")
	if len(idx["items"].([]any)) != 1 {
		t.Fatalf("the index still offers the missing app: %v", idx["items"])
	}
	if n, p := m.Publish("mine", []string{a.ID}, true); n != 0 || p[a.ID] == "" {
		t.Fatalf("published a missing file: %d %v", n, p)
	}
	// the file comes back: restored, but not published again
	put(t, dir, "a.sisx", pkginfo.ExampleSIS(0xE1111111, "A", "V", [3]int32{1, 0, 0}, "aaa"))
	r, _ = m.Scan(ctx)
	if len(r.Restored) != 1 {
		t.Fatalf("%+v", r)
	}
	if a = entry(t, m, "a.sisx"); a.Status != "ok" || a.Error != "" {
		t.Fatalf("%+v", a)
	}
	if _, pub := m.St.CatPublished("mine")[a.ID]; pub {
		t.Fatal("a restored file was republished on its own")
	}
	// a file moved to another folder is the same entry
	os.MkdirAll(filepath.Join(dir, "moved"), 0o755)
	os.Rename(filepath.Join(dir, "b.apk"), filepath.Join(dir, "moved", "b2.apk"))
	r, _ = m.Scan(ctx)
	if len(r.Renamed) != 1 || len(r.Removed) != 0 {
		t.Fatalf("%+v", r)
	}
	if got := entry(t, m, "moved/b2.apk"); got.ID != b.ID {
		t.Fatal("the entry changed identity")
	}
	if _, pub := m.St.CatPublished("mine")[b.ID]; !pub {
		t.Fatal("a moved file lost its publication")
	}
	// deleting the entry of a missing file leaves nothing behind
	os.Remove(filepath.Join(dir, "a.sisx"))
	m.Scan(ctx)
	if err := m.DeleteEntry(a.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.St.CatFile(a.ID); ok {
		t.Fatal("still there")
	}
}

func mustCat(t *testing.T, m *Manager) store.Catalogue {
	c, ok := m.St.Catalogue("mine")
	if !ok {
		t.Fatal("no catalogue")
	}
	return c
}

func TestPublishClashAndUpload(t *testing.T) {
	m, dir := newLib(t)
	put(t, dir, "one.sisx", pkginfo.ExampleSIS(0xE1111111, "A", "V", [3]int32{1, 0, 0}, "one"))
	put(t, dir, "two.sisx", pkginfo.ExampleSIS(0xE1111111, "A", "V", [3]int32{1, 0, 0}, "two"))
	m.Scan(context.Background())
	one, two := entry(t, m, "one.sisx"), entry(t, m, "two.sisx")
	m.Publish("mine", []string{one.ID}, true)
	if n, p := m.Publish("mine", []string{two.ID}, true); n != 0 || !strings.Contains(p[two.ID], "already published") {
		t.Fatalf("%d %v", n, p)
	}
	if n, _ := m.Publish("mine", []string{one.ID}, false); n != 1 {
		t.Fatal("unpublish")
	}
	if n, _ := m.Publish("mine", []string{two.ID}, true); n != 1 {
		t.Fatal("publish the other")
	}
	// an upload lands in the library, is read, and a name clash with other content gets a new name
	f, err := m.AddUpload("up.apk", strings.NewReader(string(pkginfo.ExampleAPK())), 1<<20)
	if err != nil || f.Path != "uploads/up.apk" || f.Status != "ok" {
		t.Fatalf("%+v %v", f, err)
	}
	g, err := m.AddUpload("up.apk", strings.NewReader("different"), 1<<20)
	if err != nil || g.Path == "uploads/up.apk" || g.Status != "unreadable" {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := m.AddUpload("evil.txt", strings.NewReader("x"), 10); err == nil {
		t.Fatal("a text file was accepted")
	}
	if _, err := m.AddUpload("big.apk", strings.NewReader(strings.Repeat("x", 100)), 10); err == nil {
		t.Fatal("limit")
	}
}

// The old model's releases become published "stored" entries and keep serving.
func TestMigrationOfReleases(t *testing.T) {
	m, _ := newLib(t)
	sha, _, err := m.PutBlob(strings.NewReader("old file"), 1<<20, "")
	if err != nil {
		t.Fatal(err)
	}
	data := `{"app_id":"old-app","version":"1.0","name":"Old App","platform":"symbian","package":"sis","arch":["arm"],"file":{"name":"old.sis","sha256":"` + sha + `","size":8},"published":5}`
	if err := m.St.TestRunMigration4(map[string]string{"mine|old-app|1.0": data}); err != nil {
		t.Fatal(err)
	}
	idx, files := m.Index(mustCat(t, m), "http://x/catalogue/mine")
	items := idx["items"].([]any)
	if len(items) != 1 || files[sha] != "old.sis" {
		t.Fatalf("%v %v", items, files)
	}
	if got := items[0].(map[string]any); got["id"] != "old-app" || got["name"] != "Old App" {
		t.Fatalf("%v", got)
	}
	// and it can be unpublished and deleted like the others
	var id string
	for _, f := range m.St.CatFiles() {
		id = f.ID
	}
	m.Publish("mine", []string{id}, false)
	if idx, _ = m.Index(mustCat(t, m), "x"); len(idx["items"].([]any)) != 0 {
		t.Fatal("still offered")
	}
	m.DeleteEntry(id, true)
	if _, err := os.Stat(m.blobPath(sha)); err == nil {
		t.Fatal("the stored file is still on disk")
	}
}

func TestScanVectorIcon(t *testing.T) {
	m, dir := newLib(t)
	put(t, dir, "vec.sisx", pkginfo.ExampleVectorSIS(0xE4444444, "Vector App"))
	m.Scan(context.Background())
	f := entry(t, m, "vec.sisx")
	if f.IconSHA == "" || !strings.Contains(f.Meta, `"icon_svg"`) {
		t.Fatalf("no icon kept: %q %s", f.IconSHA, f.Meta)
	}
	blob, _, err := m.OpenBlob(f.IconSHA)
	if err != nil {
		t.Fatal(err)
	}
	blob.Close()
}
