// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package nextcloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/nctalk"
	"github.com/phorepoc/cloud-companion/internal/remote"
)

// The app's own clients against the fake: Talk's test, the Login Flow through its pages, then WebDAV with the app password.
func TestWithTheAppsClients(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	ctx := context.Background()

	if _, err := nctalk.Test(ctx, nctalk.Cfg{URL: srv.URL, User: "cc"}, "definitely-wrong"); err == nil || !strings.Contains(err.Error(), "login refused") {
		t.Fatalf("a wrong password: %v", err)
	}
	if got, err := nctalk.Test(ctx, nctalk.Cfg{URL: srv.URL, User: "cc"}, "pw-cc-12345"); err != nil || got != "login works as cc, with Talk" {
		t.Fatalf("the right one: %q %v", got, err)
	}
	if r, _ := http.Get(srv.URL + "/ocs/v2.php/cloud/user"); r.StatusCode != 401 {
		t.Fatalf("no auth: %d", r.StatusCode)
	}

	l, err := remote.StartNCLogin(ctx, srv.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := l.Poll(ctx); res != nil || err != nil {
		t.Fatalf("poll before granting: %v %v", res, err)
	}
	form := func(v url.Values) string {
		r, err := http.PostForm(l.Login, v)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b := new(strings.Builder)
		buf := make([]byte, 4096)
		for n, _ := r.Body.Read(buf); n > 0; n, _ = r.Body.Read(buf) {
			b.Write(buf[:n])
		}
		return b.String()
	}
	if p := form(url.Values{"user": {"ann"}, "password": {"nope"}}); !strings.Contains(p, "Wrong login") {
		t.Fatal("a wrong password on the page")
	}
	if p := form(url.Values{"user": {"ann"}, "password": {"pw-ann-12345"}}); !strings.Contains(p, "Grant access") {
		t.Fatal("no grant step")
	}
	if p := form(url.Values{"step": {"grant"}, "user": {"ann"}, "password": {"pw-ann-12345"}}); !strings.Contains(p, "Account connected") {
		t.Fatal("not connected")
	}
	res, err := l.Poll(ctx)
	if err != nil || res == nil || res.LoginName != "ann" || res.AppPassword == "" {
		t.Fatalf("poll: %+v %v", res, err)
	}

	d, err := remote.Nextcloud(ctx, srv.URL, "ann", res.AppPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	top, err := d.List(ctx, "")
	if err != nil || len(top) != 4 || top[0].Path != "CC Photos" {
		t.Fatalf("top: %+v %v", top, err)
	}
	in, err := d.List(ctx, "CC Photos")
	if err != nil || len(in) != 1 || in[0].Path != "CC Photos/sea.png" {
		t.Fatalf("folder: %+v %v", in, err)
	}
	w := httptest.NewRecorder()
	d.Serve(w, httptest.NewRequest("GET", "/x", nil), "CC Photos/sea.png")
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "\x89PNG") {
		t.Fatalf("photo: %d", w.Code)
	}
	if _, err := remote.Nextcloud(ctx, srv.URL, "ann", "wrong", false); err == nil {
		t.Fatal("a wrong app password was taken")
	}
}

// Talk: a one-to-one chat made by inviting, a message in it, the 304 when nothing is new.
func TestTalkChat(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()
	do := func(user, method, p string, v url.Values) *http.Response {
		var body *strings.Reader = strings.NewReader("")
		if v != nil {
			body = strings.NewReader(v.Encode())
		}
		r, _ := http.NewRequest(method, srv.URL+"/ocs/v2.php"+p, body)
		r.SetBasicAuth(user, "pw-"+user+"-12345")
		r.Header.Set("OCS-APIRequest", "true")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if r := do("cc", "POST", "/apps/spreed/api/v4/room", url.Values{"roomType": {"1"}, "invite": {"nobody"}}); r.StatusCode != 404 {
		t.Fatalf("unknown user: %d", r.StatusCode)
	}
	r := do("cc", "POST", "/apps/spreed/api/v4/room", url.Values{"roomType": {"1"}, "invite": {"bob"}})
	var rm struct {
		OCS struct {
			Data struct{ Token, Name string } `json:"data"`
		} `json:"ocs"`
	}
	if r.StatusCode != 201 || json.NewDecoder(r.Body).Decode(&rm) != nil || rm.OCS.Data.Name != "bob" {
		t.Fatalf("one-to-one: %d %+v", r.StatusCode, rm)
	}
	chat := "/apps/spreed/api/v1/chat/" + rm.OCS.Data.Token
	if r := do("bob", "POST", chat, url.Values{"message": {"hi cc"}}); r.StatusCode != 201 {
		t.Fatalf("post: %d", r.StatusCode)
	}
	if r := do("cc", "GET", chat+"?lookIntoFuture=1&limit=100", nil); r.StatusCode != 200 {
		t.Fatalf("read: %d", r.StatusCode)
	}
	if r := do("cc", "GET", chat+"?lookIntoFuture=1&lastKnownMessageId=1", nil); r.StatusCode != 304 {
		t.Fatalf("nothing new: %d", r.StatusCode)
	}
	if r := do("carol", "GET", chat, nil); r.StatusCode != 404 {
		t.Fatalf("not a member: %d", r.StatusCode)
	}
}
