// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/catalogue"
	"github.com/phorepoc/cloud-companion/internal/store"
)

type catEnv struct {
	t     *testing.T
	st    *store.Store
	m     *catalogue.Manager
	srv   *httptest.Server
	admin *fakeDevice
}

func newCatEnv(t *testing.T) *catEnv {
	st := store.OpenTest(t)
	m := catalogue.New(st, t.TempDir())
	a := newAuth(st)
	mux := http.NewServeMux()
	(&catAPI{a: a, st: st, m: m}).routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	id, key, err := st.NewDevice("web", true)
	if err != nil {
		t.Fatal(err)
	}
	return &catEnv{t, st, m, srv, &fakeDevice{id, key, srv}}
}

func (e *catEnv) upload(cat string, fields map[string]string, name string, data []byte) (int, map[string]any) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("catalogue", cat)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/phe/catalogue/upload", &buf)
	ts := fmt.Sprint(time.Now().Unix())
	h := hmac.New(sha256.New, e.admin.key)
	h.Write([]byte(e.admin.id + "." + ts + ".POST /phe/catalogue/upload"))
	req.Header.Set("X-Phe-Auth", e.admin.id+"."+ts+"."+hex.EncodeToString(h.Sum(nil)))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *catEnv) get(path string, bearer string) (int, []byte, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

func (e *catEnv) index(path, bearer string) map[string]any {
	e.t.Helper()
	code, b, _ := e.get(path, bearer)
	if code != 200 {
		e.t.Fatalf("%s: %d %s", path, code, b)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		e.t.Fatal(err)
	}
	return doc
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (e *catEnv) create(id string, public bool) map[string]any {
	e.t.Helper()
	code, r := e.admin.call(e.t, "POST", "/phe/catalogues", map[string]any{"id": id, "name": id, "public": public})
	if code != 200 {
		e.t.Fatalf("create %s: %d %v", id, code, r)
	}
	return r["catalogue"].(map[string]any)
}

// Phase (a)+(b): upload, the served index.json in the official shape, public and private access, several platforms.
func TestCatalogueHostedPublicAndPrivate(t *testing.T) {
	e := newCatEnv(t)
	pub := e.create("open", true)
	priv := e.create("closed", false)
	tok := priv["token"].(string)
	if tok == "" || pub["token"] != nil {
		t.Fatalf("tokens: %v %v", pub, priv)
	}
	sis := []byte("SIS-v1")
	code, r := e.upload("open", map[string]string{"app_id": "hello", "version": "1.0", "name": "Hello", "description": "Says hello.\nMore."}, "Hello_1.0.sisx", sis)
	if code != 200 {
		t.Fatalf("upload: %d %v", code, r)
	}
	apk := []byte("APK-bytes")
	if code, r = e.upload("open", map[string]string{"app_id": "hello-droid", "version": "2.1", "name": "Hello Droid", "arch": "aarch64,arm"}, "hello.apk", apk); code != 200 {
		t.Fatalf("upload apk: %d %v", code, r)
	}
	// bad input is refused
	if code, _ = e.upload("open", map[string]string{"app_id": "Bad Id", "version": "1", "name": "x"}, "x.sis", sis); code != 400 {
		t.Fatalf("a bad id was accepted: %d", code)
	}
	if code, _ = e.upload("nope", map[string]string{"app_id": "okid", "version": "1", "name": "x"}, "x.sis", sis); code != 404 {
		t.Fatalf("unknown catalogue: %d", code)
	}
	if code, _ = (&fakeDevice{"x", []byte("k"), e.srv}).call(t, "GET", "/phe/catalogues", nil); code != 403 {
		t.Fatalf("an unsigned caller reached the owner API: %d", code)
	}

	doc := e.index("/catalogue/open/index.json", "")
	if doc["format"].(float64) != 1 || doc["generated"] == "" {
		t.Fatalf("index header: %v", doc)
	}
	items := doc["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items: %v", items)
	}
	hello, droid := items[0].(map[string]any), items[1].(map[string]any)
	// the Symbian entry has exactly the shape of an official one: the required fields, no platform / package extension
	for _, k := range []string{"id", "name", "summary", "category", "license", "source", "releases", "compatibility", "resolved", "icon_url", "screenshot_urls"} {
		if k != "icon_url" && hello[k] == nil {
			t.Fatalf("missing %s: %v", k, hello)
		}
	}
	if hello["platform"] != nil || hello["summary"] != "Says hello." || hello["category"] != "app" {
		t.Fatalf("symbian entry: %v", hello)
	}
	res := hello["resolved"].([]any)[0].(map[string]any)
	if res["tag"] != "1.0" || res["asset"] != "Hello_1.0.sisx" || res["sha256"] != sum(sis) || res["size"].(float64) != float64(len(sis)) || res["package"] != nil {
		t.Fatalf("resolved: %v", res)
	}
	if hello["compatibility"].(map[string]any)["arch"].([]any)[0] != "arm" {
		t.Fatalf("default arch: %v", hello["compatibility"])
	}
	// the other platform is marked, and no phone matches it by architecture
	dres := droid["resolved"].([]any)[0].(map[string]any)
	if droid["platform"] != "android" || dres["package"] != "apk" {
		t.Fatalf("apk entry: %v", droid)
	}
	// the file is served from the address in the index, byte for byte
	u := strings.TrimPrefix(res["url"].(string), e.srv.URL)
	code, body, hd := e.get(u, "")
	if code != 200 || !bytes.Equal(body, sis) || hd.Get("X-Content-SHA256") != sum(sis) {
		t.Fatalf("file: %d %q", code, body)
	}
	// a hash that is not in this catalogue is not served, even when the file exists on disk
	other := []byte("not offered")
	sha, _, _ := e.m.PutBlob(bytes.NewReader(other), 1<<20, "")
	if code, _, _ = e.get("/catalogue/open/files/"+sha+"/x.bin", ""); code != 404 {
		t.Fatalf("an unlisted file was served: %d", code)
	}
	// a public catalogue has no token path
	if code, _, _ = e.get("/catalogue/open/sometoken/index.json", ""); code != 404 {
		t.Fatalf("token path on a public catalogue: %d", code)
	}

	// private: nothing without the token; the token in the path, or as a bearer header
	if code, _, _ = e.get("/catalogue/closed/index.json", ""); code != 404 {
		t.Fatalf("private catalogue without token: %d", code)
	}
	if code, _, _ = e.get("/catalogue/closed/wrongtoken/index.json", ""); code != 404 {
		t.Fatalf("wrong token: %d", code)
	}
	if code, r = e.upload("closed", map[string]string{"app_id": "secret", "version": "0.1", "name": "Secret"}, "s.sis", []byte("s")); code != 200 {
		t.Fatalf("%d %v", code, r)
	}
	for _, d := range []map[string]any{e.index("/catalogue/closed/"+tok+"/index.json", ""), e.index("/catalogue/closed/index.json", tok)} {
		it := d["items"].([]any)
		if len(it) != 1 {
			t.Fatalf("private items: %v", d)
		}
		url := it[0].(map[string]any)["resolved"].([]any)[0].(map[string]any)["url"].(string)
		if !strings.Contains(url, "/catalogue/closed/"+tok+"/files/") { // a phone that cannot send headers can still download
			t.Fatalf("private file url lacks the token: %s", url)
		}
		if code, b, _ := e.get(strings.TrimPrefix(url, e.srv.URL), ""); code != 200 || string(b) != "s" {
			t.Fatalf("private file: %d", code)
		}
	}
	if code, _, _ = e.get("/catalogue/closed/files/"+sum([]byte("s"))+"/s.sis", ""); code != 404 {
		t.Fatalf("private file without token: %d", code)
	}
	// rotating the token kills the old address
	if code, r = e.admin.call(t, "POST", "/phe/catalogues/update", map[string]any{"id": "closed", "rotate_token": true}); code != 200 || r["catalogue"].(map[string]any)["token"] == tok {
		t.Fatalf("rotate: %d %v", code, r)
	}
	if code, _, _ = e.get("/catalogue/closed/"+tok+"/index.json", ""); code != 404 {
		t.Fatalf("old token still works: %d", code)
	}

	// two versions of one app are two releases of one item, newest metadata wins
	time.Sleep(1100 * time.Millisecond)
	if code, r := e.upload("open", map[string]string{"app_id": "hello", "version": "1.1", "name": "Hello Again"}, "Hello_1.1.sisx", []byte("SIS-v2")); code != 200 {
		t.Fatalf("v2: %d %v", code, r)
	}
	hi := e.index("/catalogue/open/index.json", "")["items"].([]any)[0].(map[string]any)
	if hi["name"] != "Hello Again" || len(hi["resolved"].([]any)) != 2 || len(hi["releases"].([]any)) != 2 {
		t.Fatalf("two releases: %v", hi)
	}
	// removing a release removes its file once nothing else needs it
	if code, _ = e.admin.call(t, "POST", "/phe/catalogue/release/remove", map[string]any{"catalogue": "open", "app_id": "hello", "version": "1.1"}); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.m.Dir, "blobs", sum([]byte("SIS-v2"))[:2], sum([]byte("SIS-v2")))); err == nil {
		t.Fatal("the removed release's file is still on disk")
	}
}

// A file whose content changed on disk is never sent.
func TestCatalogueTamperedFileNotServed(t *testing.T) {
	e := newCatEnv(t)
	e.create("open", true)
	data := []byte("good bytes")
	_, r := e.upload("open", map[string]string{"app_id": "app1", "version": "1", "name": "App"}, "a.sis", data)
	url := strings.TrimPrefix(r["url"].(string), e.srv.URL)
	if code, _, _ := e.get(url, ""); code != 200 {
		t.Fatalf("before: %d", code)
	}
	p := filepath.Join(e.m.Dir, "blobs", sum(data)[:2], sum(data))
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(p, []byte("evil bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, b, _ := e.get(url, ""); code == 200 {
		t.Fatalf("a modified file was served: %q", b)
	}
	// and the index no longer offers a release it cannot back
	if it := e.index("/catalogue/open/index.json", "")["items"].([]any); len(it) != 0 {
		t.Fatalf("a broken release is still listed: %v", it)
	}
}

// ---- phase (c): federation

type fakeSource struct {
	srv   *httptest.Server
	files map[string][]byte
	items func(base string) []map[string]any
	token string
	hits  int
}

func newFakeSource(t *testing.T, token string) *fakeSource {
	f := &fakeSource{files: map[string][]byte{}, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
			http.Error(w, "no", 401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"format": 1, "items": f.items("http://" + r.Host)})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
			http.Error(w, "no", 401)
			return
		}
		if b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/dl/")]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func listing(id, name, tag, published, asset, url, sha string, size int) map[string]any {
	return map[string]any{
		"id": id, "name": name, "summary": name, "category": "app", "license": "MIT", "source": map[string]any{"type": "github", "repo": "o/" + id},
		"releases": []any{tag}, "compatibility": map[string]any{"arch": []any{"arm"}}, "icon_url": "/icons/" + id + ".png",
		"resolved": []any{map[string]any{"tag": tag, "published": published, "asset": asset, "url": url, "size": size, "sha256": sha}},
	}
}

func (e *catEnv) addSource(cat, name, url, token string, prio int, mirror bool) string {
	e.t.Helper()
	code, r := e.admin.call(e.t, "POST", "/phe/catalogue/sources", map[string]any{"catalogue": cat, "name": name, "url": url, "token": token, "priority": prio, "mirror": mirror})
	if code != 200 {
		e.t.Fatalf("add source: %d %v", code, r)
	}
	return r["source"].(map[string]any)["id"].(string)
}

func (e *catEnv) sync(id string) map[string]any {
	e.t.Helper()
	code, r := e.admin.call(e.t, "POST", "/phe/catalogue/sources/sync", map[string]any{"id": id})
	if code != 200 {
		e.t.Fatalf("sync: %d %v", code, r)
	}
	return r["results"].([]any)[0].(map[string]any)
}

func TestCatalogueFederation(t *testing.T) {
	e := newCatEnv(t)
	cat := e.create("hub", true)
	_ = cat

	good, other := []byte("official-file"), []byte("second-source-file")
	officialSrc := newFakeSource(t, "")
	officialSrc.files["calc.sisx"] = good
	officialSrc.items = func(base string) []map[string]any {
		return []map[string]any{
			listing("calc", "Calc (official)", "1.0", "2026-01-01T00:00:00Z", "calc.sisx", base+"/dl/calc.sisx", sum(good), len(good)),
			listing("only-official", "Only Official", "3", "2026-01-01T00:00:00Z", "o.sis", base+"/dl/o.sis", sum([]byte("o")), 1),
			{"id": "BAD ID", "name": "x"},
		}
	}
	privSrc := newFakeSource(t, "s3cret")
	privSrc.files["calc2.sisx"] = other
	privSrc.items = func(base string) []map[string]any {
		return []map[string]any{listing("calc", "Calc (friend)", "2.0", "2026-03-01T00:00:00Z", "calc2.sisx", base+"/dl/calc2.sisx", sum(other), len(other))}
	}

	s1 := e.addSource("hub", "official", officialSrc.srv.URL+"/index.json", "", 10, false)
	s2 := e.addSource("hub", "friend", privSrc.srv.URL+"/index.json", "s3cret", 20, true)
	if code, _ := e.admin.call(t, "POST", "/phe/catalogue/sources", map[string]any{"catalogue": "hub", "url": "ftp://x/y"}); code != 400 {
		t.Fatalf("a non-http source: %d", code)
	}

	r1 := e.sync(s1)
	if r1["ok"] != true || r1["summary"].(map[string]any)["items"].(float64) != 2 || r1["summary"].(map[string]any)["skipped"].(float64) != 1 {
		t.Fatalf("sync official: %v", r1)
	}
	r2 := e.sync(s2)
	if r2["ok"] != true || r2["summary"].(map[string]any)["mirrored"].(float64) != 1 {
		t.Fatalf("sync friend (token + mirror): %v", r2)
	}
	// a wrong token is an error that is recorded, and the old listing stays
	e.admin.call(t, "POST", "/phe/catalogue/sources/update", map[string]any{"id": s2, "token": "wrong"})
	if r := e.sync(s2); r["ok"] != false {
		t.Fatalf("wrong token synced: %v", r)
	}
	e.admin.call(t, "POST", "/phe/catalogue/sources/update", map[string]any{"id": s2, "token": "s3cret"})

	items := e.index("/catalogue/hub/index.json", "")["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("merged items: %v", items)
	}
	by := map[string]map[string]any{}
	for _, x := range items {
		by[x.(map[string]any)["id"].(string)] = x.(map[string]any)
	}
	calc := by["calc"]
	// the higher priority source (friend, 20) wins although the official one is listed too
	if calc["name"] != "Calc (friend)" {
		t.Fatalf("winner: %v", calc)
	}
	// its file is mirrored: the address is ours, and the bytes are the verified ones
	res := calc["resolved"].([]any)[0].(map[string]any)
	if !strings.Contains(res["url"].(string), "/catalogue/hub/files/"+sum(other)+"/") {
		t.Fatalf("mirrored url: %v", res)
	}
	if code, b, _ := e.get(strings.TrimPrefix(res["url"].(string), e.srv.URL), ""); code != 200 || !bytes.Equal(b, other) {
		t.Fatalf("mirrored file: %d", code)
	}
	// relative addresses of the source are made absolute; an un-mirrored release keeps the source's own file address
	if by["only-official"]["icon_url"] != officialSrc.srv.URL+"/icons/only-official.png" || !strings.HasPrefix(by["only-official"]["resolved"].([]any)[0].(map[string]any)["url"].(string), officialSrc.srv.URL+"/dl/") {
		t.Fatalf("passthrough: %v", by["only-official"])
	}

	// the owner sees who won and who lost
	_, ar := e.admin.call(t, "GET", "/phe/catalogue/apps?catalogue=hub", nil)
	for _, a := range ar["apps"].([]any) {
		a := a.(map[string]any)
		if a["id"] == "calc" {
			w, o := a["winner"].(map[string]any), a["others"].([]any)
			if w["source"] != "friend" || w["version"] != "2.0" || len(o) != 1 || o[0].(map[string]any)["source"] != "official" || o[0].(map[string]any)["reason"] != "lower priority" {
				t.Fatalf("report: %v", a)
			}
		}
	}
	// the owner turns the priorities around: the official one wins now, the friend's stays visible
	e.admin.call(t, "POST", "/phe/catalogue/sources/update", map[string]any{"id": s1, "priority": 99})
	if n := e.index("/catalogue/hub/index.json", "")["items"].([]any)[0].(map[string]any)["name"]; n != "Calc (official)" {
		t.Fatalf("after re-prioritising: %v", n)
	}
	// a hosted entry wins by the catalogue's own priority (default 100)
	e.upload("hub", map[string]string{"app_id": "calc", "version": "9", "name": "Calc (mine)"}, "mine.sis", []byte("mine"))
	if n := e.index("/catalogue/hub/index.json", "")["items"].([]any)[0].(map[string]any)["name"]; n != "Calc (mine)" {
		t.Fatalf("hosted vs sources: %v", n)
	}
	_, ar = e.admin.call(t, "GET", "/phe/catalogue/apps?catalogue=hub", nil)
	for _, a := range ar["apps"].([]any) {
		if a := a.(map[string]any); a["id"] == "calc" && len(a["others"].([]any)) != 2 {
			t.Fatalf("both sources stay visible: %v", a)
		}
	}
}

// A mirrored file whose hash differs from the source's listing is dropped and never served; the release keeps the source's own address.
func TestCatalogueMirrorRejectsHashMismatch(t *testing.T) {
	e := newCatEnv(t)
	e.create("hub", true)
	real := []byte("the real bytes")
	src := newFakeSource(t, "")
	src.files["app.sis"] = []byte("TAMPERED bytes")
	src.items = func(base string) []map[string]any {
		return []map[string]any{listing("app1", "App", "1", "2026-01-01T00:00:00Z", "app.sis", base+"/dl/app.sis", sum(real), len(real))}
	}
	id := e.addSource("hub", "s", src.srv.URL+"/index.json", "", 50, true)
	r := e.sync(id)
	if s := r["summary"].(map[string]any); s["mirror_failed"].(float64) != 1 || s["mirrored"].(float64) != 0 {
		t.Fatalf("mismatch: %v", r)
	}
	res := e.index("/catalogue/hub/index.json", "")["items"].([]any)[0].(map[string]any)["resolved"].([]any)[0].(map[string]any)
	if !strings.HasPrefix(res["url"].(string), src.srv.URL) {
		t.Fatalf("a failed mirror was advertised: %v", res)
	}
	if code, _, _ := e.get("/catalogue/hub/files/"+sum(real)+"/app.sis", ""); code != 404 {
		t.Fatalf("served: %d", code)
	}
	if code, _, _ := e.get("/catalogue/hub/files/"+sum([]byte("TAMPERED bytes"))+"/app.sis", ""); code != 404 {
		t.Fatalf("served the tampered file: %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.m.Dir, "blobs", sum(src.files["app.sis"])[:2], sum(src.files["app.sis"]))); err == nil {
		t.Fatal("the mismatching download was kept")
	}
	// per-app override: the source mirrors nothing, but one app is set to mirror -> still the hash check; fixing the source makes it work
	src.files["app.sis"] = real
	e.admin.call(t, "POST", "/phe/catalogue/sources/update", map[string]any{"id": id, "mirror": false})
	if r := e.sync(id); r["summary"].(map[string]any)["mirrored"].(float64) != 0 {
		t.Fatalf("mirror off: %v", r)
	}
	if code, _ := e.admin.call(t, "POST", "/phe/catalogue/sources/mirror", map[string]any{"source": id, "app_id": "app1", "mirror": 1}); code != 200 {
		t.Fatalf("override: %d", code)
	}
	if r := e.sync(id); r["summary"].(map[string]any)["mirrored"].(float64) != 1 {
		t.Fatalf("override mirror: %v", r)
	}
	res = e.index("/catalogue/hub/index.json", "")["items"].([]any)[0].(map[string]any)["resolved"].([]any)[0].(map[string]any)
	if !strings.Contains(res["url"].(string), "/catalogue/hub/files/"+sum(real)) {
		t.Fatalf("after fixing: %v", res)
	}
}

// The automatic sync honours each catalogue's interval; 0 means manual only.
func TestCatalogueSyncDue(t *testing.T) {
	e := newCatEnv(t)
	e.create("auto", true)
	e.create("manual", true)
	src := newFakeSource(t, "")
	src.items = func(base string) []map[string]any { return []map[string]any{} }
	e.addSource("auto", "a", src.srv.URL+"/index.json", "", 50, false)
	e.addSource("manual", "m", src.srv.URL+"/index.json", "", 50, false)
	e.admin.call(t, "POST", "/phe/catalogues/update", map[string]any{"id": "auto", "sync_interval": 3600})
	e.admin.call(t, "POST", "/phe/catalogues/update", map[string]any{"id": "manual", "sync_interval": 0})
	now := time.Now()
	if n := e.m.SyncDue(t.Context(), now); n != 1 {
		t.Fatalf("first pass synced %d, want 1", n)
	}
	if n := e.m.SyncDue(t.Context(), now.Add(30*time.Minute)); n != 0 {
		t.Fatalf("too early: %d", n)
	}
	if n := e.m.SyncDue(t.Context(), now.Add(2*time.Hour)); n != 1 {
		t.Fatalf("due again: %d", n)
	}
}

// DUMP_INDEX=<file> writes a served index for the compatibility check against the official schema (see docs/catalogue.md).
func TestCatalogueDumpIndex(t *testing.T) {
	out := os.Getenv("DUMP_INDEX")
	if out == "" {
		t.Skip("set DUMP_INDEX to write a sample index")
	}
	e := newCatEnv(t)
	e.create("open", true)
	e.upload("open", map[string]string{"app_id": "hello", "version": "1.0", "name": "Hello", "arch": "arm,x32", "boards": "pinephone"}, "Hello.sisx", []byte("x"))
	e.upload("open", map[string]string{"app_id": "droid", "version": "2", "name": "Droid"}, "d.apk", []byte("y"))
	_, b, _ := e.get("/catalogue/open/index.json", "")
	os.WriteFile(out, b, 0o644)
}
