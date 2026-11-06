// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/files"
	"github.com/phorepoc/cloud-companion/internal/remote"
)

// Photos: the timeline and albums of the owner's Immich accounts, and of the folders they marked as photo folders (their own, or
// in another service's files), newest first. Every paired device can read them:
//
//	GET  /phe/photos?before=<time>&n=        -> {items, ticket, more}   the timeline
//	GET  /phe/photos/albums                  -> {albums, ticket}        Immich albums and the photo folders
//	GET  /phe/photos/album?src=&id=&before=  -> {items, ticket, more}
//	GET  /phe/photos/folders                 -> {folders}               the photo folders (paths as in Files)
//	POST /phe/photos/folders {folders}                                  the owner sets them
//	GET  /phe/photos/img?t=&src=&id=&size=thumb|preview|  [&video=1]    a photo or video, for <img> and <video>
//
// An item's src is "im:<account id>" (Immich) or "f" (a photo folder; its id is the file's path). The ticket in each answer lets
// that device's browser show the pictures (it cannot sign <img> requests) for 12 hours.

const photoFoldersSetting = "photo_folders"

type photo struct {
	Src string `json:"src"`
	remote.Asset
}

type photoTicket struct{ exp time.Time }

type photoAlbum struct {
	Src string `json:"src"`
	remote.Album
	CoverSrc string `json:"cover_src,omitempty"`
}

type photoCache struct {
	at    time.Time
	items []photo
}

type photosState struct {
	mu      sync.Mutex
	tickets map[string]photoTicket
	scans   map[string]photoCache // a photo folder's pictures, for 2 minutes
}

func (f *filesAPI) photoFolders() []string {
	var l []string
	json.Unmarshal([]byte(f.st.Setting(photoFoldersSetting)), &l)
	return l
}

func (f *filesAPI) photoTicket() string {
	t := rand.Text()
	f.ph.mu.Lock()
	defer f.ph.mu.Unlock()
	if f.ph.tickets == nil {
		f.ph.tickets = map[string]photoTicket{}
	}
	now := time.Now()
	for k, v := range f.ph.tickets {
		if now.After(v.exp) {
			delete(f.ph.tickets, k)
		}
	}
	f.ph.tickets[t] = photoTicket{now.Add(12 * time.Hour)}
	return t
}

func (f *filesAPI) photoTicketOK(t string) bool {
	f.ph.mu.Lock()
	defer f.ph.mu.Unlock()
	v, ok := f.ph.tickets[t]
	return ok && time.Now().Before(v.exp)
}

func (f *filesAPI) immich(ctx context.Context, id string) (*remote.Immich, error) {
	src, err := f.source(ctx, id)
	if err != nil {
		return nil, err
	}
	im, ok := src.(*remote.Immich)
	if !ok {
		return nil, remote.ErrNotFound
	}
	return im, nil
}

func (f *filesAPI) immichAccounts() []string {
	var out []string
	for _, a := range f.mounts() {
		if a.Kind == "immich" {
			out = append(out, a.ID)
		}
	}
	return out
}

// list reads a folder of the owner's or of another service.
func (f *filesAPI) list(ctx context.Context, p string) ([]files.Entry, error) {
	if id, rest, ok := remotePath(p); ok {
		src, err := f.source(ctx, id)
		if err != nil {
			return nil, err
		}
		l, err := src.List(ctx, rest)
		for i := range l {
			l[i].Path = "~" + id + "/" + l[i].Path
		}
		return l, err
	}
	return f.fs.List(p)
}

// scan finds the photos and videos in a photo folder and the folders in it, four deep, up to 5000.
func (f *filesAPI) scan(ctx context.Context, root string) []photo {
	f.ph.mu.Lock()
	c, ok := f.ph.scans[root]
	f.ph.mu.Unlock()
	if ok && time.Since(c.at) < 2*time.Minute {
		return c.items
	}
	var out []photo
	var walk func(p string, depth int)
	walk = func(p string, depth int) {
		l, err := f.list(ctx, p)
		if err != nil {
			return
		}
		for _, e := range l {
			if len(out) >= 5000 {
				return
			}
			switch {
			case e.Dir && depth < 4:
				walk(e.Path, depth+1)
			case strings.HasPrefix(e.Type, "image/") || strings.HasPrefix(e.Type, "video/"):
				out = append(out, photo{"f", remote.Asset{ID: e.Path, Name: e.Name, Taken: e.Mod, Video: strings.HasPrefix(e.Type, "video/"), Type: e.Type, Size: e.Size}})
			}
		}
	}
	walk(root, 0)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Taken.After(out[j].Taken) })
	f.ph.mu.Lock()
	if f.ph.scans == nil {
		f.ph.scans = map[string]photoCache{}
	}
	f.ph.scans[root] = photoCache{time.Now(), out}
	f.ph.mu.Unlock()
	return out
}

// inPhotoFolder: a folder file the image endpoint may show.
func (f *filesAPI) inPhotoFolder(p string) bool {
	c, err := files.Clean(p)
	if err != nil || c != p {
		return false
	}
	for _, root := range f.photoFolders() {
		if root == "" || strings.HasPrefix(c, strings.TrimSuffix(root, "/")+"/") {
			return true
		}
	}
	return false
}

// timeline merges the sources' pictures taken before before, newest first: n of them, and whether there are more.
func (f *filesAPI) timeline(ctx context.Context, srcs []string, album string, folders []string, before time.Time, n int) ([]photo, bool, error) {
	var all []photo
	var firstErr error
	more := false
	for _, id := range srcs {
		im, err := f.immich(ctx, id)
		var l []remote.Asset
		if err == nil {
			l, err = im.Assets(ctx, album, before, n+1)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(l) > n {
			more = true
		}
		for _, a := range l {
			all = append(all, photo{"im:" + id, a})
		}
	}
	for _, root := range folders {
		for _, p := range f.scan(ctx, root) {
			if before.IsZero() || p.Taken.Before(before) {
				all = append(all, p)
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Taken.After(all[j].Taken) })
	if len(all) > n {
		all, more = all[:n], true
	}
	if len(all) == 0 && firstErr != nil {
		return nil, false, firstErr
	}
	return all, more, nil
}

func (f *filesAPI) photoRoutes(mux *http.ServeMux) {
	page := func(r *http.Request) (time.Time, int) {
		before, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("before"))
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if n <= 0 || n > 500 {
			n = 150
		}
		return before, n
	}
	mux.HandleFunc("GET /phe/photos", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		before, n := page(r)
		items, more, err := f.timeline(r.Context(), f.immichAccounts(), "", f.photoFolders(), before, n)
		if err != nil {
			remoteErr(w, err)
			return
		}
		reply(w, 200, map[string]any{"items": nonNil(items), "more": more, "ticket": f.photoTicket()})
	})
	mux.HandleFunc("GET /phe/photos/album", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		before, n := page(r)
		src, id := r.URL.Query().Get("src"), r.URL.Query().Get("id")
		var items []photo
		var more bool
		var err error
		switch {
		case src == "f" && contains(f.photoFolders(), id):
			items, more, err = f.timeline(r.Context(), nil, "", []string{id}, before, n)
		case strings.HasPrefix(src, "im:") && id != "":
			items, more, err = f.timeline(r.Context(), []string{strings.TrimPrefix(src, "im:")}, id, nil, before, n)
		default:
			err = remote.ErrNotFound
		}
		if err != nil {
			remoteErr(w, err)
			return
		}
		reply(w, 200, map[string]any{"items": nonNil(items), "more": more, "ticket": f.photoTicket()})
	})
	mux.HandleFunc("GET /phe/photos/albums", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		var out []photoAlbum
		for _, root := range f.photoFolders() {
			l := f.scan(r.Context(), root)
			a := photoAlbum{Src: "f", Album: remote.Album{ID: root, Name: f.folderName(r.Context(), root), Count: len(l)}}
			for _, p := range l {
				if !p.Video {
					a.Cover, a.CoverSrc = p.ID, "f"
					break
				}
			}
			out = append(out, a)
		}
		for _, id := range f.immichAccounts() {
			im, err := f.immich(r.Context(), id)
			if err != nil {
				continue
			}
			l, err := im.Albums(r.Context())
			if err != nil {
				continue
			}
			for _, a := range l {
				out = append(out, photoAlbum{Src: "im:" + id, Album: a, CoverSrc: "im:" + id})
			}
		}
		reply(w, 200, map[string]any{"albums": nonNil(out), "ticket": f.photoTicket()})
	})
	mux.HandleFunc("GET /phe/photos/folders", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		reply(w, 200, map[string]any{"folders": nonNil(f.photoFolders())})
	})
	mux.HandleFunc("POST /phe/photos/folders", func(w http.ResponseWriter, r *http.Request) {
		if !f.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		var in struct{ Folders []string }
		if !readJSON(w, r, &in) {
			return
		}
		seen := map[string]bool{}
		out := []string{}
		for _, p := range in.Folders {
			c, err := files.Clean(p)
			if err != nil || c == "" {
				bad(w, 400, "pick a folder, not the top of Files: "+p)
				return
			}
			if seen[c] {
				continue
			}
			if l, err := f.list(r.Context(), c); err != nil && l == nil {
				bad(w, 422, "no such folder: "+p)
				return
			}
			seen[c] = true
			out = append(out, c)
		}
		b, _ := json.Marshal(out)
		f.st.SetSetting(photoFoldersSetting, string(b))
		f.ph.mu.Lock()
		f.ph.scans = nil
		f.ph.mu.Unlock()
		reply(w, 200, map[string]any{"folders": out})
	})
	mux.HandleFunc("GET /phe/photos/img", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !f.photoTicketOK(q.Get("t")) {
			http.Error(w, "this link has expired", http.StatusForbidden)
			return
		}
		src, id := q.Get("src"), q.Get("id")
		switch {
		case src == "f" && f.inPhotoFolder(id):
			if rid, rest, ok := remotePath(id); ok {
				s, err := f.source(r.Context(), rid)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
				s.Serve(w, r, rest)
				return
			}
			serveFile(w, r, f.fs, id, false)
		case strings.HasPrefix(src, "im:") && id != "":
			im, err := f.immich(r.Context(), strings.TrimPrefix(src, "im:"))
			if err != nil {
				http.Error(w, "no such Immich account", http.StatusNotFound)
				return
			}
			im.ServeAsset(w, r, id, q.Get("size"), q.Get("video") == "1")
		default:
			http.NotFound(w, r)
		}
	})
}

// folderName is what a photo folder is called: its own name, under the account's for another service's top folder.
func (f *filesAPI) folderName(ctx context.Context, p string) string {
	if id, rest, ok := remotePath(p); ok && rest == "" {
		for _, a := range f.mounts() {
			if a.ID == id {
				return a.Name
			}
		}
	}
	return path.Base(p)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func nonNil[T any](l []T) []T {
	if l == nil {
		return []T{}
	}
	return l
}
