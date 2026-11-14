// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The fake Immich through the app's own client (internal/remote): key check, timeline, albums, thumbnails.
package immich

import (
	"context"
	"image/png"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/remote"
)

func TestFake(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()
	ctx := context.Background()
	if _, err := remote.NewImmich(ctx, srv.URL, "wrong", false); err != remote.ErrAuth {
		t.Fatalf("a wrong key: %v", err)
	}
	im, err := remote.NewImmich(ctx, srv.URL, Key, false)
	if err != nil {
		t.Fatal(err)
	}
	l, err := im.Assets(ctx, "", time.Time{}, 10)
	if err != nil || len(l) != 2 || l[0].Name != "sea.png" || l[1].Name != "snow.png" || l[0].W != 320 {
		t.Fatalf("timeline %+v %v", l, err)
	}
	if o, _ := im.Assets(ctx, "", l[0].Taken, 10); len(o) != 1 || o[0].Name != "snow.png" {
		t.Fatalf("older than sea.png: %+v", o)
	}
	al, err := im.Albums(ctx)
	if err != nil || len(al) != 1 || al[0].Name != "Summer" || al[0].Count != 1 || al[0].Cover != l[0].ID {
		t.Fatalf("albums %+v %v", al, err)
	}
	if in, _ := im.Assets(ctx, al[0].ID, time.Time{}, 10); len(in) != 1 || in[0].Name != "sea.png" {
		t.Fatalf("Summer %+v", in)
	}
	if top, err := im.List(ctx, ""); err != nil || len(top) != 1 {
		t.Fatalf("Files top %+v %v", top, err)
	}
	rec := httptest.NewRecorder()
	im.ServeAsset(rec, httptest.NewRequest("GET", "/", nil), l[0].ID, "thumb", false)
	if rec.Code != 200 {
		t.Fatalf("thumbnail %d", rec.Code)
	}
	if img, err := png.Decode(rec.Body); err != nil || img.Bounds().Dx() != 320 {
		t.Fatalf("thumbnail image: %v", err)
	}
	rec = httptest.NewRecorder()
	im.ServeAsset(rec, httptest.NewRequest("GET", "/", nil), "nope", "thumb", false)
	if rec.Code != 404 {
		t.Fatalf("no such asset %d", rec.Code)
	}
}
