// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// fakeImmich answers the parts of Immich's API Cloud Companion uses: two photos (2024 in the album "Summer", 2022 not).
func fakeImmich(t *testing.T) *httptest.Server {
	assets := []map[string]any{
		{"id": "a24", "type": "IMAGE", "originalFileName": "sea.jpg", "originalMimeType": "image/jpeg", "fileCreatedAt": "2024-07-01T10:00:00Z", "exifInfo": map[string]any{"exifImageWidth": 40, "exifImageHeight": 30}},
		{"id": "a22", "type": "VIDEO", "originalFileName": "dog.mp4", "originalMimeType": "video/mp4", "fileCreatedAt": "2022-03-01T10:00:00Z"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key-1" {
			http.Error(w, "{}", 401)
			return
		}
		switch {
		case r.URL.Path == "/api/users/me":
			w.Write([]byte(`{"id":"u1"}`))
		case r.URL.Path == "/api/albums" && r.URL.Query().Get("shared") == "":
			w.Write([]byte(`[{"id":"al1","albumName":"Summer","assetCount":1,"albumThumbnailAssetId":"a24"}]`))
		case r.URL.Path == "/api/albums":
			w.Write([]byte(`[]`))
		case r.URL.Path == "/api/search/metadata":
			var q struct {
				Size        int
				TakenBefore string
				AlbumIDs    []string `json:"albumIds"`
			}
			json.NewDecoder(r.Body).Decode(&q)
			before, _ := time.Parse(time.RFC3339Nano, q.TakenBefore)
			var out []map[string]any
			for _, a := range assets {
				at, _ := time.Parse(time.RFC3339, a["fileCreatedAt"].(string))
				if (q.TakenBefore == "" || at.Before(before)) && (len(q.AlbumIDs) == 0 || a["id"] == "a24") && len(out) < q.Size {
					out = append(out, a)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"assets": map[string]any{"items": out}})
		case r.URL.Path == "/api/assets/a24/thumbnail":
			w.Header().Set("Content-Type", "image/webp")
			w.Write([]byte("WEBP-" + r.URL.Query().Get("size")))
		case r.URL.Path == "/api/assets/a22/video/playback":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write([]byte("MP4"))
		default:
			http.NotFound(w, r)
		}
	}))
}

// The Photos API: an Immich account and a photo folder in one timeline, newest first, in pages; albums from both; pictures
// through the ticket; the owner alone sets the photo folders, and only files in them are shown.
func TestPhotosAPI(t *testing.T) {
	im := fakeImmich(t)
	defer im.Close()
	st := store.OpenTest(t)
	acct, _ := st.AddAccount(store.Account{Kind: "immich", Provider: "generic", Name: "Home photos", Config: `{"url":"` + im.URL + `"}`})
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Camera", "2023"), 0o755)
	os.WriteFile(filepath.Join(dir, "Camera", "2023", "hill.jpg"), []byte("JPEG-hill"), 0o644)
	os.WriteFile(filepath.Join(dir, "Camera", "notes.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.jpg"), []byte("JPEG-secret"), 0o644)
	os.Chtimes(filepath.Join(dir, "Camera", "2023", "hill.jpg"), time.Now(), time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC))
	mux := http.NewServeMux()
	fa := newFilesAPI(newAuth(st), st, dir)
	fa.secret = func(store.Account) (string, error) { return "key-1", nil }
	fa.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	oid, okey, _ := st.NewDevice("owner", true)
	id, key, _ := st.NewDevice("phone", false)
	owner, phone := &fakeDevice{oid, okey, srv}, &fakeDevice{id, key, srv}

	if code, _ := phone.call(t, "POST", "/phe/photos/folders", map[string]any{"folders": []string{"Camera"}}); code != 403 {
		t.Fatalf("a phone set the photo folders: %d", code)
	}
	if code, _ := owner.call(t, "POST", "/phe/photos/folders", map[string]any{"folders": []string{"Nope"}}); code != 422 {
		t.Fatalf("a missing folder: %d", code)
	}
	if code, j := owner.call(t, "POST", "/phe/photos/folders", map[string]any{"folders": []string{"Camera", "Camera/"}}); code != 200 || len(j["folders"].([]any)) != 1 {
		t.Fatalf("set the photo folders: %d %v", code, j)
	}

	names := func(j map[string]any) (out []string) {
		for _, x := range j["items"].([]any) {
			out = append(out, x.(map[string]any)["name"].(string))
		}
		return
	}
	code, j := phone.call(t, "GET", "/phe/photos?n=2", nil)
	if code != 200 || strings.Join(names(j), ",") != "sea.jpg,hill.jpg" || j["more"] != true {
		t.Fatalf("the timeline: %d %v", code, j)
	}
	first := j["items"].([]any)[0].(map[string]any)
	if first["src"] != "im:"+acct.ID || first["w"] != 40.0 {
		t.Fatalf("an Immich photo: %v", first)
	}
	last := j["items"].([]any)[1].(map[string]any)
	if last["src"] != "f" || last["id"] != "Camera/2023/hill.jpg" {
		t.Fatalf("a folder photo: %v", last)
	}
	tk := j["ticket"].(string)
	code, j = phone.call(t, "GET", "/phe/photos?n=2&before="+url.QueryEscape(last["taken"].(string)), nil)
	if code != 200 || strings.Join(names(j), ",") != "dog.mp4" || j["more"] != false || j["items"].([]any)[0].(map[string]any)["video"] != true {
		t.Fatalf("the next page: %d %v", code, j)
	}

	code, j = phone.call(t, "GET", "/phe/photos/albums", nil)
	al := j["albums"].([]any)
	if code != 200 || len(al) != 2 {
		t.Fatalf("albums: %d %v", code, j)
	}
	if a := al[0].(map[string]any); a["name"] != "Camera" || a["count"] != 1.0 || a["cover"] != "Camera/2023/hill.jpg" {
		t.Fatalf("the photo folder as an album: %v", a)
	}
	if a := al[1].(map[string]any); a["name"] != "Summer" || a["src"] != "im:"+acct.ID || a["cover"] != "a24" {
		t.Fatalf("the Immich album: %v", a)
	}
	if code, j := phone.call(t, "GET", "/phe/photos/album?src=im:"+acct.ID+"&id=al1", nil); code != 200 || strings.Join(names(j), ",") != "sea.jpg" {
		t.Fatalf("in the Immich album: %d %v", code, j)
	}
	if code, j := phone.call(t, "GET", "/phe/photos/album?src=f&id=Camera", nil); code != 200 || strings.Join(names(j), ",") != "hill.jpg" {
		t.Fatalf("in the photo folder: %d %v", code, j)
	}
	if code, _ := phone.call(t, "GET", "/phe/photos/album?src=f&id=Music", nil); code != 404 {
		t.Fatalf("a folder that is not a photo folder: %d", code)
	}

	get := func(q string) (int, string, string) {
		res, err := http.Get(srv.URL + "/phe/photos/img?" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Content-Type"), string(b)
	}
	if c, ty, b := get("t=" + tk + "&src=im:" + acct.ID + "&id=a24&size=thumb"); c != 200 || ty != "image/webp" || b != "WEBP-thumbnail" {
		t.Fatalf("an Immich thumbnail: %d %s %q", c, ty, b)
	}
	if c, ty, b := get("t=" + tk + "&src=im:" + acct.ID + "&id=a22&video=1"); c != 200 || ty != "video/mp4" || b != "MP4" {
		t.Fatalf("an Immich video: %d %s %q", c, ty, b)
	}
	if c, _, b := get("t=" + tk + "&src=f&id=Camera/2023/hill.jpg"); c != 200 || b != "JPEG-hill" {
		t.Fatalf("a folder photo: %d %q", c, b)
	}
	for _, q := range []string{"t=" + tk + "&src=f&id=secret.jpg", "t=" + tk + "&src=f&id=Camera/../secret.jpg", "t=nope&src=f&id=Camera/2023/hill.jpg"} {
		if c, _, _ := get(q); c != 404 && c != 403 {
			t.Fatalf("%s: %d", q, c)
		}
	}
}
