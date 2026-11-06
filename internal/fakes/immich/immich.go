// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package immich is a stand-in Immich for the Photos browser suite (web/test/photos.sh): the owner Ann with one API key, two photos
// (sea.png, July 2024, in the album "Summer"; snow.png, January 2023), and just the API calls Cloud Companion makes for them
// (internal/remote/immich.go). The real server is in testdata/immich/test.sh.
package immich

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/fakes"
)

// Key is the seeded API key (the suite types it into Connections).
const Key = "fake-immich-api-key-ann"

// Addr is where the real server listens.
const Addr = ":2283"

func init() {
	fakes.Register(fakes.Fake{Name: "immich", Run: func(ctx context.Context) error { return run(ctx, Addr) }})
}

func run(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s := &http.Server{Handler: New()}
	go func() { <-ctx.Done(); s.Close() }()
	if err := s.Serve(l); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type asset struct {
	ID, Name string
	Taken    time.Time
	W, H     int
	Data     []byte
}

type album struct {
	ID, Name string
	Assets   []string
}

// Server is the fake; its state is fixed after seeding (the suite only reads).
type Server struct {
	assets map[string]*asset
	albums []album
}

// New makes a seeded fake.
func New() http.Handler {
	s := &Server{assets: map[string]*asset{}}
	add := func(id, name, when string, rgb [3]byte) {
		t, _ := time.Parse(time.RFC3339, when)
		s.assets[id] = &asset{ID: id, Name: name, Taken: t, W: 320, H: 240, Data: PNG(320, 240, rgb)}
	}
	add("8f1c0e4a-0000-4000-8000-000000000001", "sea.png", "2024-07-01T10:00:00Z", [3]byte{30, 110, 220})
	add("8f1c0e4a-0000-4000-8000-000000000002", "snow.png", "2023-01-05T09:00:00Z", [3]byte{235, 238, 245})
	s.albums = []album{{ID: "a1b2c3d4-0000-4000-8000-0000000000a1", Name: "Summer", Assets: []string{"8f1c0e4a-0000-4000-8000-000000000001"}}}
	return s
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p == "/api/server/ping" {
		reply(w, 200, map[string]string{"res": "pong"})
		return
	}
	if r.Header.Get("x-api-key") != Key { // Immich answers 401 to a missing or wrong key
		reply(w, 401, map[string]any{"message": "Invalid API key", "error": "Unauthorized", "statusCode": 401})
		return
	}
	switch {
	case p == "/api/users/me" && r.Method == "GET":
		reply(w, 200, map[string]any{"id": "u-ann", "email": "ann@example.org", "name": "Ann", "isAdmin": true})
	case p == "/api/albums" && r.Method == "GET":
		out := []map[string]any{}
		if r.URL.Query().Get("shared") != "true" { // nothing is shared with Ann
			for _, a := range s.albums {
				out = append(out, map[string]any{"id": a.ID, "albumName": a.Name, "assetCount": len(a.Assets), "albumThumbnailAssetId": a.Assets[0]})
			}
		}
		reply(w, 200, out)
	case p == "/api/search/metadata" && r.Method == "POST":
		s.search(w, r)
	case strings.HasPrefix(p, "/api/assets/") && r.Method == "GET":
		id, what, _ := strings.Cut(strings.TrimPrefix(p, "/api/assets/"), "/")
		a, ok := s.assets[id]
		if !ok || (what != "thumbnail" && what != "original") {
			reply(w, 400, map[string]any{"message": "Not found or no asset.read access", "statusCode": 400})
			return
		}
		w.Header().Set("Content-Type", "image/png") // the real one makes WebP/JPEG thumbnails; a PNG shows the same in the browser
		w.Write(a.Data)
	default:
		reply(w, 404, map[string]any{"message": "Cannot " + r.Method + " " + p, "statusCode": 404})
	}
}

// search answers POST /api/search/metadata: newest first, takenBefore and albumIds as filters, one page of size.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Size        int       `json:"size"`
		TakenBefore time.Time `json:"takenBefore"`
		AlbumIDs    []string  `json:"albumIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		reply(w, 400, map[string]any{"message": err.Error(), "statusCode": 400})
		return
	}
	in := map[string]bool{}
	for _, id := range q.AlbumIDs {
		for _, a := range s.albums {
			if a.ID == id {
				for _, x := range a.Assets {
					in[x] = true
				}
			}
		}
	}
	var l []*asset
	for _, a := range s.assets {
		if (len(q.AlbumIDs) == 0 || in[a.ID]) && (q.TakenBefore.IsZero() || a.Taken.Before(q.TakenBefore)) {
			l = append(l, a)
		}
	}
	sort.Slice(l, func(i, j int) bool { return l[i].Taken.After(l[j].Taken) })
	if q.Size > 0 && len(l) > q.Size {
		l = l[:q.Size]
	}
	items := []map[string]any{}
	for _, a := range l {
		items = append(items, map[string]any{
			"id": a.ID, "type": "IMAGE", "originalFileName": a.Name, "originalMimeType": "image/png",
			"fileCreatedAt": a.Taken, "localDateTime": a.Taken, "isTrashed": false,
			"exifInfo": map[string]any{"exifImageWidth": a.W, "exifImageHeight": a.H, "fileSizeInByte": len(a.Data)},
		})
	}
	reply(w, 200, map[string]any{"assets": map[string]any{"total": len(items), "count": len(items), "items": items, "nextPage": nil}})
}

// PNG makes a w×h PNG of one colour.
func PNG(w, h int, rgb [3]byte) []byte {
	chunk := func(b *bytes.Buffer, t string, d []byte) {
		binary.Write(b, binary.BigEndian, uint32(len(d)))
		td := append([]byte(t), d...)
		b.Write(td)
		binary.Write(b, binary.BigEndian, crc32.ChecksumIEEE(td))
	}
	var raw bytes.Buffer
	for y := 0; y < h; y++ {
		raw.WriteByte(0)
		for x := 0; x < w; x++ {
			raw.Write(rgb[:])
		}
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(raw.Bytes())
	zw.Close()
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	copy(ihdr[8:], []byte{8, 2, 0, 0, 0})
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	chunk(&b, "IHDR", ihdr)
	chunk(&b, "IDAT", z.Bytes())
	chunk(&b, "IEND", nil)
	return b.Bytes()
}
