// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The Files and Music API (internal/files). Every paired device reads and changes the owner's files; the browser's media players
// cannot sign their requests, so they read through a ticket (one file, 12 hours), and a public link (a share) lets anyone with it
// download one file. "Play on" hands a queue to another of the owner's devices, which picks it up on its next check.
//
//	GET  /phe/files?path=        {path, entries}
//	POST /phe/files/mkdir        {path}
//	POST /phe/files/move         {from, to}
//	POST /phe/files/delete       {path}
//	POST /phe/files/upload?path= multipart "file" parts, saved in that folder
//	POST /phe/files/ticket       {path} -> {url}: /phe/files/raw?t=... (Range requests work: seeking, streaming)
//	GET  /phe/files/shares       {shares}; POST /phe/files/share {path, days} -> {share, url}; POST /phe/files/unshare {token}
//	GET  /s/{token}              the shared file (public)
//	POST /phe/nextcloud/login    {url, insecure} -> {id, login}: open login, approve; GET /phe/nextcloud/login?id= -> {state, user, password}
//	GET  /phe/music              {tracks}: every song, by artist, album, number
//
// The folders of other services (Nextcloud, WebDAV and Seafile accounts, package remote) are top folders named after their
// account, at paths "~<account id>/...": listed, looked at and streamed like the owner's own, and read-only for now.
//	POST /phe/music/cast         {device, paths, index}: play these on that device
//	GET  /phe/music/remote       {cast} for this device (once), or {}

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/phorepoc/cloud-companion/internal/files"
	"github.com/phorepoc/cloud-companion/internal/remote"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// ---- the folders of other services

// remotePath splits "~<account id>/rest" into the account and the path in it.
func remotePath(p string) (string, string, bool) {
	c, err := files.Clean(p)
	if err != nil || !strings.HasPrefix(c, "~") {
		return "", "", false
	}
	id, rest, _ := strings.Cut(c[1:], "/")
	return id, rest, true
}

// mounts are the accounts whose files show as folders.
func (f *filesAPI) mounts() []store.Account {
	if f.secret == nil {
		return nil
	}
	var out []store.Account
	for _, a := range f.st.Accounts() {
		if remote.Kinds(a.Kind) {
			out = append(out, a)
		}
	}
	return out
}

func mountLabel(a store.Account) string {
	var c remote.Cfg
	json.Unmarshal([]byte(a.Config), &c)
	switch {
	case a.Kind == "seafile":
		return "Seafile"
	case a.Kind == "immich":
		return "Immich"
	case c.Nextcloud:
		return "Nextcloud"
	}
	return "WebDAV"
}

// source opens (or reuses) the files of account id.
func (f *filesAPI) source(ctx context.Context, id string) (remote.Source, error) {
	for _, a := range f.mounts() {
		if a.ID != id {
			continue
		}
		pass, err := f.secret(a)
		if err != nil {
			return nil, errors.New("the account's password cannot be opened (key changed?)")
		}
		sum := sha256.Sum256([]byte(a.Config + "\x00" + pass))
		key := hex.EncodeToString(sum[:])
		f.mu.Lock()
		o, ok := f.sources[id]
		f.mu.Unlock()
		if ok && o.key == key {
			return o.src, nil
		}
		src, err := remote.Connect(ctx, a.Kind, a.Config, pass)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", a.Name, mountLabel(a), err)
		}
		f.mu.Lock()
		if f.sources == nil {
			f.sources = map[string]openSource{}
		}
		f.sources[id] = openSource{key, src}
		f.mu.Unlock()
		return src, nil
	}
	return nil, remote.ErrNotFound
}

func remoteErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, files.ErrBadPath):
		bad(w, 400, err.Error())
	case errors.Is(err, remote.ErrNotFound):
		bad(w, 404, "not found")
	default:
		bad(w, 502, err.Error())
	}
}

var errReadOnly = errors.New("the folders of other services are read-only here for now: change them in that service")

type filesAPI struct {
	a      *auth
	st     *store.Store
	fs     *files.Store
	shares *files.Shares
	mu     sync.Mutex
	casts  map[string]cast // device id -> what to play
	// secret opens an account's sealed password (connectors.Manager.Secret); nil: no folders of other services
	secret   func(store.Account) (string, error)
	sources  map[string]openSource // by account id
	ncLogins map[string]*ncLogin
	ph       photosState
}

type openSource struct {
	key string // the configuration and secret it was opened with: a change opens it again
	src remote.Source
}

type ncLogin struct {
	l       *remote.NCLogin
	started time.Time
	done    *remote.NCLoginResult
	err     string
}

type cast struct {
	From  string    `json:"from"`
	Paths []string  `json:"paths"`
	Index int       `json:"index"`
	At    time.Time `json:"at"`
}

const sharesSetting = "file-shares"

func newFilesAPI(a *auth, st *store.Store, dir string) *filesAPI {
	var saved []files.Share
	if s := st.Setting(sharesSetting); s != "" {
		json.Unmarshal([]byte(s), &saved)
	}
	return &filesAPI{a: a, st: st, fs: files.New(dir), casts: map[string]cast{},
		shares: files.NewShares(saved, func(l []files.Share) { b, _ := json.Marshal(l); st.SetSetting(sharesSetting, string(b)) })}
}

func (f *filesAPI) dev(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, _, ok := f.a.signedDevice(r)
	if !ok {
		bad(w, 403, "pair this device first")
	}
	return id, ok
}

func fileErr(w http.ResponseWriter, err error) {
	if errors.Is(err, files.ErrBadPath) {
		bad(w, 400, err.Error())
		return
	}
	bad(w, 422, err.Error())
}

func serveFile(w http.ResponseWriter, r *http.Request, fs *files.Store, p string, download bool) {
	fh, e, err := fs.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer fh.Close()
	if e.Type != "" {
		w.Header().Set("Content-Type", e.Type)
	}
	disp := "inline"
	if download {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": e.Name}))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// a shared or streamed file is shown as data, never run as a page of this site
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'")
	http.ServeContent(w, r, e.Name, e.Mod, fh)
}

func (f *filesAPI) routes(mux *http.ServeMux) {
	f.photoRoutes(mux)
	f.davRoutes(mux)
	mux.HandleFunc("GET /phe/files", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		p := r.URL.Query().Get("path")
		if id, rest, ok := remotePath(p); ok {
			src, err := f.source(r.Context(), id)
			var l []files.Entry
			if err == nil {
				l, err = src.List(r.Context(), rest)
			}
			if err != nil {
				remoteErr(w, err)
				return
			}
			for i := range l {
				l[i].Path, l[i].ReadOnly = "~"+id+"/"+l[i].Path, true
			}
			c, _ := files.Clean(p)
			reply(w, 200, map[string]any{"path": c, "entries": l, "readonly": true})
			return
		}
		l, err := f.fs.List(p)
		if err != nil {
			fileErr(w, err)
			return
		}
		c, _ := files.Clean(p)
		if c == "" { // the other services' folders, after the owner's own
			for _, a := range f.mounts() {
				l = append(l, files.Entry{Name: a.Name, Path: "~" + a.ID, Dir: true, Source: mountLabel(a), ReadOnly: true})
			}
		}
		reply(w, 200, map[string]any{"path": c, "entries": l})
	})
	one := func(name string, do func(in struct{ Path, From, To string }) error) {
		mux.HandleFunc("POST /phe/files/"+name, func(w http.ResponseWriter, r *http.Request) {
			if _, ok := f.dev(w, r); !ok {
				return
			}
			var in struct{ Path, From, To string }
			if !readJSON(w, r, &in) {
				return
			}
			for _, p := range []string{in.Path, in.From, in.To} {
				if _, _, ok := remotePath(p); ok {
					bad(w, 422, errReadOnly.Error())
					return
				}
			}
			if err := do(in); err != nil {
				fileErr(w, err)
				return
			}
			reply(w, 200, map[string]any{"ok": true})
		})
	}
	one("mkdir", func(in struct{ Path, From, To string }) error { return f.fs.Mkdir(in.Path) })
	one("move", func(in struct{ Path, From, To string }) error { return f.fs.Move(in.From, in.To) })
	one("delete", func(in struct{ Path, From, To string }) error { return f.fs.Delete(in.Path) })
	mux.HandleFunc("POST /phe/files/upload", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		dir := r.URL.Query().Get("path")
		if _, _, ok := remotePath(dir); ok {
			bad(w, 422, errReadOnly.Error())
			return
		}
		mr, err := r.MultipartReader()
		if err != nil {
			bad(w, 400, "send the files as a form")
			return
		}
		saved := []files.Entry{}
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if part.FormName() != "file" || part.FileName() == "" {
				part.Close()
				continue
			}
			e, err := f.fs.Save(dir, path.Base(strings.ReplaceAll(part.FileName(), "\\", "/")), part)
			part.Close()
			if err != nil {
				fileErr(w, err)
				return
			}
			saved = append(saved, e)
		}
		reply(w, 200, map[string]any{"saved": saved})
	})
	mux.HandleFunc("POST /phe/files/ticket", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		var in struct{ Path string }
		if !readJSON(w, r, &in) {
			return
		}
		if id, rest, ok := remotePath(in.Path); ok {
			src, err := f.source(r.Context(), id)
			var e files.Entry
			if err == nil {
				e, err = src.Stat(r.Context(), rest)
			}
			if err == nil && e.Dir {
				err = errors.New("that is a folder")
			}
			if err != nil {
				remoteErr(w, err)
				return
			}
			c, _ := files.Clean(in.Path)
			reply(w, 200, map[string]any{"url": "/phe/files/raw?t=" + f.fs.TicketFor(c)})
			return
		}
		t, err := f.fs.Ticket(in.Path)
		if err != nil {
			fileErr(w, err)
			return
		}
		reply(w, 200, map[string]any{"url": "/phe/files/raw?t=" + t})
	})
	mux.HandleFunc("GET /phe/files/raw", func(w http.ResponseWriter, r *http.Request) {
		p, ok := f.fs.Redeem(r.URL.Query().Get("t"))
		if !ok {
			http.Error(w, "this link has expired", http.StatusForbidden)
			return
		}
		if id, rest, ok := remotePath(p); ok {
			src, err := f.source(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			src.Serve(w, r, rest)
			return
		}
		serveFile(w, r, f.fs, p, r.URL.Query().Get("dl") == "1")
	})
	mux.HandleFunc("GET /phe/files/shares", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		reply(w, 200, map[string]any{"shares": f.shares.List()})
	})
	mux.HandleFunc("POST /phe/files/share", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		var in struct {
			Path string
			Days int
		}
		if !readJSON(w, r, &in) {
			return
		}
		if _, _, ok := remotePath(in.Path); ok {
			bad(w, 422, "share links are for your own files; share this one from its service")
			return
		}
		e, err := f.fs.Stat(in.Path)
		if err == nil && e.Dir {
			err = errors.New("share a file, not a folder")
		}
		if err != nil {
			fileErr(w, err)
			return
		}
		s, err := f.shares.Add(e.Path, in.Days)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		reply(w, 200, map[string]any{"share": s, "url": "/s/" + s.Token})
	})
	mux.HandleFunc("POST /phe/files/unshare", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		var in struct{ Token string }
		if !readJSON(w, r, &in) {
			return
		}
		f.shares.Remove(in.Token)
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /s/{token}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := f.shares.Get(r.PathValue("token"))
		if !ok {
			http.Error(w, "this link does not work any more", http.StatusNotFound)
			return
		}
		serveFile(w, r, f.fs, p, r.URL.Query().Get("view") != "1")
	})
	// Nextcloud's sign-in from another app: the dialog opens login in a new tab and asks how it is going every 2 seconds
	mux.HandleFunc("POST /phe/nextcloud/login", func(w http.ResponseWriter, r *http.Request) {
		if !f.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		var in struct {
			URL      string
			Insecure bool
		}
		if !readJSON(w, r, &in) {
			return
		}
		l, err := remote.StartNCLogin(r.Context(), in.URL, in.Insecure)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		id := rand.Text()
		f.mu.Lock()
		if f.ncLogins == nil {
			f.ncLogins = map[string]*ncLogin{}
		}
		for k, o := range f.ncLogins { // Nextcloud forgets a sign-in after 20 minutes
			if time.Since(o.started) > 20*time.Minute {
				delete(f.ncLogins, k)
			}
		}
		f.ncLogins[id] = &ncLogin{l: l, started: time.Now()}
		f.mu.Unlock()
		reply(w, 200, map[string]any{"id": id, "login": l.Login})
	})
	mux.HandleFunc("GET /phe/nextcloud/login", func(w http.ResponseWriter, r *http.Request) {
		if !f.a.adminDevice(r) {
			bad(w, 403, "sign in as the owner")
			return
		}
		f.mu.Lock()
		o := f.ncLogins[r.URL.Query().Get("id")]
		f.mu.Unlock()
		if o == nil {
			bad(w, 404, "no such sign-in; start again")
			return
		}
		if o.done == nil && o.err == "" {
			res, err := o.l.Poll(r.Context())
			f.mu.Lock()
			if err != nil {
				o.err = err.Error()
			} else if res != nil {
				o.done = res
			}
			f.mu.Unlock()
		}
		switch {
		case o.err != "":
			reply(w, 200, map[string]any{"state": "failed", "error": o.err})
		case o.done != nil: // the app password goes to the owner's own dialog, which stores it as the account's secret
			reply(w, 200, map[string]any{"state": "done", "user": o.done.LoginName, "password": o.done.AppPassword, "server": o.done.Server})
		default:
			reply(w, 200, map[string]any{"state": "waiting"})
		}
	})
	mux.HandleFunc("GET /phe/music", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.dev(w, r); !ok {
			return
		}
		l, err := f.fs.Music()
		if err != nil {
			bad(w, 500, "could not read the music")
			return
		}
		reply(w, 200, map[string]any{"tracks": l})
	})
	mux.HandleFunc("POST /phe/music/cast", func(w http.ResponseWriter, r *http.Request) {
		from, ok := f.dev(w, r)
		if !ok {
			return
		}
		var in struct {
			Device string
			Paths  []string
			Index  int
		}
		if !readJSON(w, r, &in) {
			return
		}
		if _, _, err := f.st.Key(in.Device); err != nil || in.Device == from {
			bad(w, 400, "pick another of your devices")
			return
		}
		if len(in.Paths) == 0 || len(in.Paths) > 500 || in.Index < 0 || in.Index >= len(in.Paths) {
			bad(w, 400, "nothing to play")
			return
		}
		for _, p := range in.Paths {
			if e, err := f.fs.Stat(p); err != nil || e.Dir {
				bad(w, 422, "a song is missing: "+p)
				return
			}
		}
		f.mu.Lock()
		f.casts[in.Device] = cast{From: from, Paths: in.Paths, Index: in.Index, At: time.Now()}
		f.mu.Unlock()
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /phe/music/remote", func(w http.ResponseWriter, r *http.Request) {
		id, ok := f.dev(w, r)
		if !ok {
			return
		}
		f.mu.Lock()
		c, has := f.casts[id]
		delete(f.casts, id)
		f.mu.Unlock()
		if !has || time.Since(c.At) > 10*time.Minute {
			reply(w, 200, map[string]any{})
			return
		}
		reply(w, 200, map[string]any{"cast": c})
	})
}
