// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

//go:build immich

// Against a real Immich (testdata/immich/test.sh runs it next to this test).

package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// immichSetup signs up the owner, makes an API key, uploads two photos (2024-07-01 and 2023-01-05) and puts the newer in "Summer".
func immichSetup(t *testing.T, base string) (key, newer, older string) {
	call := func(method, p, auth string, body any, out any) {
		t.Helper()
		b, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, base+"/api"+p, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 300 {
			t.Fatalf("%s %s: %s %s", method, p, res.Status, raw)
		}
		if out != nil {
			json.Unmarshal(raw, out)
		}
	}
	call("POST", "/auth/admin-sign-up", "", map[string]string{"email": "ann@example.org", "password": "pw-ann-12345", "name": "Ann"}, nil)
	var login struct{ AccessToken string }
	call("POST", "/auth/login", "", map[string]string{"email": "ann@example.org", "password": "pw-ann-12345"}, &login)
	var k struct{ Secret string }
	call("POST", "/api-keys", login.AccessToken, map[string]any{"name": "Cloud Companion", "permissions": []string{"all"}}, &k)
	up := func(name, when string, c color.RGBA) string {
		img := image.NewRGBA(image.Rect(0, 0, 64, 48))
		for i := range img.Pix {
			img.Pix[i] = []byte{c.R, c.G, c.B, 255}[i%4]
		}
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("assetData", name)
		png.Encode(fw, img)
		for k, v := range map[string]string{"fileCreatedAt": when, "fileModifiedAt": when, "deviceAssetId": name, "deviceId": "cc-test"} {
			mw.WriteField(k, v)
		}
		mw.Close()
		r, _ := http.NewRequest("POST", base+"/api/assets", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.Header.Set("x-api-key", k.Secret)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var j struct{ ID string }
		json.NewDecoder(res.Body).Decode(&j)
		if res.StatusCode >= 300 || j.ID == "" {
			t.Fatalf("upload %s: %s", name, res.Status)
		}
		return j.ID
	}
	newer = up("sea.png", "2024-07-01T10:00:00Z", color.RGBA{30, 90, 200, 255})
	older = up("snow.png", "2023-01-05T09:00:00Z", color.RGBA{240, 240, 250, 255})
	call("POST", "/albums", login.AccessToken, map[string]any{"albumName": "Summer", "assetIds": []string{newer}}, nil)
	return k.Secret, newer, older
}

func TestRealImmich(t *testing.T) {
	base := os.Getenv("IMMICH")
	ctx := context.Background()
	key, newer, older := immichSetup(t, base)
	if _, err := NewImmich(ctx, base, "wrong-key", false); !errors.Is(err, ErrAuth) {
		t.Fatalf("a wrong key: %v", err)
	}
	im, err := NewImmich(ctx, base+"/api/", key, false)
	if err != nil {
		t.Fatal(err)
	}
	var l []Asset
	for i := 0; i < 30; i++ { // Immich reads the photos' details in the background
		if l, err = im.Assets(ctx, "", time.Time{}, 10); err == nil && len(l) == 2 && l[0].W > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil || len(l) != 2 || l[0].ID != newer || l[1].ID != older || l[0].Name != "sea.png" || l[0].W != 64 || l[0].H != 48 || l[0].Type != "image/png" {
		t.Fatalf("the timeline: %+v %v", l, err)
	}
	if l, err := im.Assets(ctx, "", l[0].Taken, 10); err != nil || len(l) != 1 || l[0].ID != older {
		t.Fatalf("the next page: %+v %v", l, err)
	}
	albums, err := im.Albums(ctx)
	if err != nil || len(albums) != 1 || albums[0].Name != "Summer" || albums[0].Count != 1 || albums[0].Cover != newer {
		t.Fatalf("albums: %+v %v", albums, err)
	}
	if l, err := im.Assets(ctx, albums[0].ID, time.Time{}, 10); err != nil || len(l) != 1 || l[0].ID != newer {
		t.Fatalf("an album: %+v %v", l, err)
	}
	t.Log("the timeline newest first, a next page, an album and its photo")

	for _, c := range []struct{ size, typ string }{{"thumb", "image/"}, {"preview", "image/"}, {"", "image/png"}} {
		var rec *httptest.ResponseRecorder
		for i := 0; i < 30; i++ { // and makes the thumbnails in the background
			rec = httptest.NewRecorder()
			im.ServeAsset(rec, httptest.NewRequest("GET", "/x", nil), newer, c.size, false)
			if rec.Code == 200 {
				break
			}
			time.Sleep(time.Second)
		}
		if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), c.typ) || rec.Body.Len() == 0 {
			t.Fatalf("size %q: %d %v", c.size, rec.Code, rec.Header())
		}
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Range", "bytes=1-3")
	im.ServeAsset(rec, r, newer, "", false)
	if rec.Code != 206 || rec.Body.String() != "PNG" {
		t.Fatalf("a range of the original: %d %q", rec.Code, rec.Body.String())
	}
	t.Log("a thumbnail, a preview, and a range of the original")

	top, err := im.List(ctx, "")
	if err != nil || len(top) != 1 || top[0].Name != "Summer" || !top[0].Dir {
		t.Fatalf("Files, the top: %+v %v", top, err)
	}
	in, err := im.List(ctx, "Summer")
	if err != nil || len(in) != 1 || in[0].Name != "sea.png" || in[0].Path != "Summer/sea.png" {
		t.Fatalf("Files, an album: %+v %v", in, err)
	}
	rec = httptest.NewRecorder()
	im.Serve(rec, httptest.NewRequest("GET", "/x", nil), "Summer/sea.png")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Files, a photo: %d %v", rec.Code, rec.Header())
	}
	if d, err := Test(ctx, "immich", `{"url":"`+base+`"}`, key); err != nil || d != "signed in: 1 item at the top" {
		t.Fatalf("Test: %q %v", d, err)
	}
	t.Log("in Files: the album is a folder with the photo in it")
}
