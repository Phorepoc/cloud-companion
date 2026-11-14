// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/files"
)

// Immich reads a person's own Immich server with an API key (Immich → Account settings → API keys). For Files its albums are the
// top folders; the Photos section uses its timeline, albums and thumbnails directly.
type Immich struct {
	Server string
	key    string
	HTTP   *http.Client

	mu     sync.Mutex
	albums map[string]string // album name as shown -> id
	names  map[string]map[string]string
}

// Asset is a photo or video on Immich.
type Asset struct {
	ID    string    `json:"id"`
	Video bool      `json:"video,omitempty"`
	Name  string    `json:"name"`
	Taken time.Time `json:"taken"`
	W     int       `json:"w,omitempty"`
	H     int       `json:"h,omitempty"`
	Type  string    `json:"type,omitempty"`
	Size  int64     `json:"size,omitempty"`
}

// Album is an Immich album.
type Album struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Cover string `json:"cover,omitempty"` // an asset ID
}

// NewImmich checks the key.
func NewImmich(ctx context.Context, server, key string, insecure bool) (*Immich, error) {
	s := strings.TrimRight(strings.TrimSpace(server), "/")
	s = strings.TrimSuffix(s, "/api")
	if u, err := url.Parse(s); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("the Immich address is needed, as https://photos.example.org")
	}
	im := &Immich{Server: s, key: key, HTTP: client(insecure)}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var me struct {
		ID string `json:"id"`
	}
	if err := im.call(ctx, "GET", "/api/users/me", nil, &me); err != nil {
		if err == ErrAuth || err == ErrNotFound {
			return nil, err
		}
		return nil, fmt.Errorf("this does not look like an Immich server (%v)", err)
	}
	return im, nil
}

func (im *Immich) call(ctx context.Context, method, p string, body, v any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(ctx, method, im.Server+p, rd)
	if err != nil {
		return err
	}
	r.Header.Set("x-api-key", im.key)
	r.Header.Set("Accept", "application/json")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	res, err := im.HTTP.Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		return ErrAuth
	case res.StatusCode == 404 || res.StatusCode == 400 && strings.Contains(p, "/assets/"):
		return ErrNotFound
	case res.StatusCode >= 300:
		return fmt.Errorf("the server answered %s", res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(v)
}

type immichAsset struct {
	ID               string    `json:"id"`
	Type             string    `json:"type"`
	OriginalFileName string    `json:"originalFileName"`
	OriginalMimeType string    `json:"originalMimeType"`
	FileCreatedAt    time.Time `json:"fileCreatedAt"`
	LocalDateTime    time.Time `json:"localDateTime"`
	IsTrashed        bool      `json:"isTrashed"`
	ExifInfo         *struct {
		W    int   `json:"exifImageWidth"`
		H    int   `json:"exifImageHeight"`
		Size int64 `json:"fileSizeInByte"`
	} `json:"exifInfo"`
}

func (a immichAsset) asset() Asset {
	o := Asset{ID: a.ID, Video: a.Type == "VIDEO", Name: a.OriginalFileName, Taken: a.FileCreatedAt, Type: a.OriginalMimeType}
	if o.Type == "" {
		o.Type = files.TypeOf(a.OriginalFileName)
	}
	if a.ExifInfo != nil {
		o.W, o.H, o.Size = a.ExifInfo.W, a.ExifInfo.H, a.ExifInfo.Size
	}
	return o
}

// Assets lists photos and videos newest first, taken before before (zero: from now), up to n; album limits it to one album.
func (im *Immich) Assets(ctx context.Context, album string, before time.Time, n int) ([]Asset, error) {
	q := map[string]any{"size": n, "page": 1, "order": "desc", "withExif": true}
	if !before.IsZero() {
		q["takenBefore"] = before.Add(-time.Millisecond).UTC().Format(time.RFC3339Nano)
	}
	if album != "" {
		q["albumIds"] = []string{album}
	}
	var res struct {
		Assets struct {
			Items []immichAsset `json:"items"`
		} `json:"assets"`
	}
	if err := im.call(ctx, "POST", "/api/search/metadata", q, &res); err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(res.Assets.Items))
	for _, a := range res.Assets.Items {
		if !a.IsTrashed {
			out = append(out, a.asset())
		}
	}
	return out, nil
}

// Albums lists the person's albums and those shared with them.
func (im *Immich) Albums(ctx context.Context) ([]Album, error) {
	var own, shared []struct {
		ID         string `json:"id"`
		AlbumName  string `json:"albumName"`
		AssetCount int    `json:"assetCount"`
		Thumb      string `json:"albumThumbnailAssetId"`
	}
	if err := im.call(ctx, "GET", "/api/albums", nil, &own); err != nil {
		return nil, err
	}
	im.call(ctx, "GET", "/api/albums?shared=true", nil, &shared) // older servers may not have it
	seen := map[string]bool{}
	var out []Album
	for _, a := range append(own, shared...) {
		if !seen[a.ID] {
			seen[a.ID] = true
			out = append(out, Album{ID: a.ID, Name: a.AlbumName, Count: a.AssetCount, Cover: a.Thumb})
		}
	}
	return out, nil
}

// ServeAsset answers r with an asset: size "thumb" (a small JPEG/WebP), "preview" (screen-sized) or "" (the original, or for a
// video the version Immich made for browsers).
func (im *Immich) ServeAsset(w http.ResponseWriter, r *http.Request, id, size string, video bool) {
	p := "/api/assets/" + url.PathEscape(id)
	switch {
	case size == "thumb":
		p += "/thumbnail?size=thumbnail"
	case size == "preview":
		p += "/thumbnail?size=preview"
	case video:
		p += "/video/playback"
	default:
		p += "/original"
	}
	up, err := http.NewRequestWithContext(r.Context(), "GET", im.Server+p, nil)
	if err != nil {
		status(w, err)
		return
	}
	up.Header.Set("x-api-key", im.key)
	forward(up, r)
	res, err := im.HTTP.Do(up)
	if err != nil {
		status(w, err)
		return
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		status(w, ErrAuth)
		return
	case res.StatusCode == 400 || res.StatusCode == 404:
		status(w, ErrNotFound)
		return
	case res.StatusCode >= 400:
		status(w, fmt.Errorf("%s", res.Status))
		return
	}
	name := id
	switch t := res.Header.Get("Content-Type"); {
	case strings.HasPrefix(t, "image/webp"):
		name += ".webp"
	case strings.HasPrefix(t, "image/jpeg"):
		name += ".jpg"
	case strings.HasPrefix(t, "video/mp4"):
		name += ".mp4"
	default:
		if exts, _ := mime.ExtensionsByType(t); len(exts) > 0 {
			name += exts[0]
		}
	}
	passOn(w, res, name)
}

// Files: the albums are folders, their photos and videos the files in them.

func (im *Immich) albumIndex(ctx context.Context) (map[string]string, []Album, error) {
	l, err := im.Albums(ctx)
	if err != nil {
		return nil, nil, err
	}
	idx := map[string]string{}
	for i, a := range l {
		n := strings.ReplaceAll(strings.TrimSpace(a.Name), "/", "∕")
		if n == "" {
			n = "Album"
		}
		if _, dup := idx[n]; dup {
			n += " (" + a.ID[:min(6, len(a.ID))] + ")"
		}
		idx[n] = a.ID
		l[i].Name = n
	}
	im.mu.Lock()
	im.albums = idx
	im.mu.Unlock()
	return idx, l, nil
}

// albumFiles lists an album as files: a name two assets share gets the start of the asset's ID added.
func (im *Immich) albumFiles(ctx context.Context, dir, id string) ([]files.Entry, map[string]Asset, error) {
	var all []Asset
	var before time.Time
	for len(all) < 20000 {
		l, err := im.Assets(ctx, id, before, 1000)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, l...)
		if len(l) < 1000 {
			break
		}
		before = l[len(l)-1].Taken
	}
	byName := map[string]Asset{}
	var out []files.Entry
	for _, a := range all {
		n := strings.ReplaceAll(a.Name, "/", "∕")
		if _, dup := byName[n]; dup || n == "" {
			ext := path.Ext(n)
			n = strings.TrimSuffix(n, ext) + " (" + a.ID[:min(6, len(a.ID))] + ")" + ext
		}
		byName[n] = a
		e := entry(dir, n, false, a.Size, a.Taken)
		if a.Type != "" {
			e.Type = a.Type
		}
		out = append(out, e)
	}
	return out, byName, nil
}

func (im *Immich) find(ctx context.Context, c string) (Asset, error) {
	album, name, ok := strings.Cut(c, "/")
	if !ok || strings.Contains(name, "/") {
		return Asset{}, ErrNotFound
	}
	idx, _, err := im.albumIndex(ctx)
	if err != nil {
		return Asset{}, err
	}
	id, ok := idx[album]
	if !ok {
		return Asset{}, ErrNotFound
	}
	_, byName, err := im.albumFiles(ctx, album, id)
	if err != nil {
		return Asset{}, err
	}
	a, ok := byName[name]
	if !ok {
		return Asset{}, ErrNotFound
	}
	return a, nil
}

// List lists the albums at the top, and an album's photos and videos.
func (im *Immich) List(ctx context.Context, p string) ([]files.Entry, error) {
	c, err := clean(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	idx, albums, err := im.albumIndex(ctx)
	if err != nil {
		return nil, err
	}
	if c == "" {
		out := make([]files.Entry, 0, len(albums))
		for _, a := range albums {
			out = append(out, entry("", a.Name, true, 0, time.Time{}))
		}
		files.Sort(out)
		return out, nil
	}
	id, ok := idx[c]
	if !ok {
		return nil, ErrNotFound
	}
	out, _, err := im.albumFiles(ctx, c, id)
	files.Sort(out)
	return out, err
}

// Stat looks at one album or asset.
func (im *Immich) Stat(ctx context.Context, p string) (files.Entry, error) {
	c, err := clean(p)
	if err != nil {
		return files.Entry{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if c == "" {
		return files.Entry{Dir: true}, nil
	}
	if !strings.Contains(c, "/") {
		idx, _, err := im.albumIndex(ctx)
		if err != nil {
			return files.Entry{}, err
		}
		if _, ok := idx[c]; !ok {
			return files.Entry{}, ErrNotFound
		}
		return entry("", c, true, 0, time.Time{}), nil
	}
	a, err := im.find(ctx, c)
	if err != nil {
		return files.Entry{}, err
	}
	dir, name := path.Split(c)
	e := entry(strings.TrimSuffix(dir, "/"), name, false, a.Size, a.Taken)
	return e, nil
}

// Serve passes on the original of an album's photo or video.
func (im *Immich) Serve(w http.ResponseWriter, r *http.Request, p string) {
	c, err := clean(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := im.find(r.Context(), c)
	if err != nil {
		status(w, err)
		return
	}
	up, _ := http.NewRequestWithContext(r.Context(), "GET", im.Server+"/api/assets/"+url.PathEscape(a.ID)+"/original", nil)
	up.Header.Set("x-api-key", im.key)
	forward(up, r)
	res, err := im.HTTP.Do(up)
	if err != nil {
		status(w, err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		status(w, fmt.Errorf("%s", res.Status))
		return
	}
	passOn(w, res, path.Base(c))
}
