// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

func TestFilesAPI(t *testing.T) {
	st := store.OpenTest(t)
	mux := http.NewServeMux()
	newFilesAPI(newAuth(st), st, t.TempDir()).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func() *fakeDevice {
		id, key, err := st.NewDevice("d", false)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	pc, phone := mk(), mk()
	if code, _ := (&fakeDevice{"nobody", []byte("x"), srv}).call(t, "GET", "/phe/files", nil); code != 403 {
		t.Fatal("unpaired")
	}
	// upload two files into Music/A/B
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, n := range []string{"01 One.mp3", "notes.txt"} {
		w, _ := mw.CreateFormFile("file", n)
		w.Write([]byte("0123456789"))
	}
	mw.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/phe/files/upload?path=Music/A/B", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	ts := fmt.Sprint(time.Now().Unix())
	m := hmac.New(sha256.New, pc.key)
	m.Write([]byte(pc.id + "." + ts + ".POST /phe/files/upload"))
	req.Header.Set("X-Phe-Auth", pc.id+"."+ts+"."+hex.EncodeToString(m.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("upload: %v %v", err, resp.Status)
	}
	if code, j := phone.call(t, "GET", "/phe/files?path=Music/A/B", nil); code != 200 || len(j["entries"].([]any)) != 2 {
		t.Fatalf("list from another device: %d %v", code, j)
	}
	if code, _ := phone.call(t, "GET", "/phe/files?path=../../etc", nil); code != 400 {
		t.Fatal("out of the root")
	}
	// stream with a ticket, with a Range
	_, j := pc.call(t, "POST", "/phe/files/ticket", map[string]any{"path": "Music/A/B/01 One.mp3"})
	rq, _ := http.NewRequest("GET", srv.URL+j["url"].(string), nil)
	rq.Header.Set("Range", "bytes=2-4")
	r2, _ := http.DefaultClient.Do(rq)
	b, _ := io.ReadAll(r2.Body)
	if r2.StatusCode != 206 || string(b) != "234" || r2.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("range: %d %q %s", r2.StatusCode, b, r2.Header.Get("Content-Type"))
	}
	if r3, _ := http.Get(srv.URL + "/phe/files/raw?t=bad"); r3.StatusCode != 403 {
		t.Error("bad ticket")
	}
	// share publicly, then stop sharing
	_, j = pc.call(t, "POST", "/phe/files/share", map[string]any{"path": "Music/A/B/notes.txt", "days": 7})
	u := j["url"].(string)
	r4, _ := http.Get(srv.URL + u)
	b, _ = io.ReadAll(r4.Body)
	if r4.StatusCode != 200 || string(b) != "0123456789" || r4.Header.Get("Content-Disposition") == "" {
		t.Fatalf("public link: %d %q", r4.StatusCode, b)
	}
	if code, j := phone.call(t, "GET", "/phe/files/shares", nil); code != 200 || len(j["shares"].([]any)) != 1 {
		t.Fatal("shares list")
	}
	pc.call(t, "POST", "/phe/files/unshare", map[string]any{"token": u[3:]})
	if r5, _ := http.Get(srv.URL + u); r5.StatusCode != 404 {
		t.Error("an unshared link stops working")
	}
	// music and play-on
	if code, j := phone.call(t, "GET", "/phe/music", nil); code != 200 || len(j["tracks"].([]any)) != 1 || j["tracks"].([]any)[0].(map[string]any)["artist"] != "A" {
		t.Fatalf("music: %v", j)
	}
	if code, _ := pc.call(t, "POST", "/phe/music/cast", map[string]any{"device": phone.id, "paths": []string{"nope.mp3"}, "index": 0}); code != 422 {
		t.Error("a missing song")
	}
	if code, _ := pc.call(t, "POST", "/phe/music/cast", map[string]any{"device": phone.id, "paths": []string{"Music/A/B/01 One.mp3"}, "index": 0}); code != 200 {
		t.Fatal("cast")
	}
	if _, j := phone.call(t, "GET", "/phe/music/remote", nil); j["cast"] == nil {
		t.Fatal("the phone picks it up")
	}
	if _, j := phone.call(t, "GET", "/phe/music/remote", nil); j["cast"] != nil {
		t.Error("only once")
	}
	if code, _ := pc.call(t, "POST", "/phe/files/delete", map[string]any{"path": "Music"}); code != 200 {
		t.Fatal("delete")
	}
}
