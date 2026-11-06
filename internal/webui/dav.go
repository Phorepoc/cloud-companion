// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/files"
	"github.com/phorepoc/cloud-companion/internal/remote"
	"github.com/phorepoc/cloud-companion/internal/store"
	"golang.org/x/net/webdav"
)

// The Files section as a WebDAV server at /dav/, for file managers and phones that speak it (Nautilus, Dolphin, Windows,
// iOS Files, the N9's and Symbian's apps): the owner's own files, which can be changed, and next to them a read-only folder per
// account of another service (Nextcloud, WebDAV, Seafile, Immich albums). Sign in with a phone login (Connections → Messaging →
// Phone logins; the same login as SIP and XMPP).

const davPrefix = "/dav"

// davFS joins the owner's files and the other services' folders into one tree.
type davFS struct {
	f     *filesAPI
	local webdav.FileSystem
}

var errDavReadOnly = errors.New("read-only: change these files in their own service")

// split tells whether name is in another service's folder: its account ID and the path in it.
func (d *davFS) split(name string) (id, rest string, ok bool) {
	c := strings.Trim(path.Clean("/"+name), "/")
	top, rest, _ := strings.Cut(c, "/")
	if top == "" {
		return "", "", false
	}
	for _, a := range d.f.mounts() {
		if davName(a, d.f.mounts()) == top {
			return a.ID, rest, true
		}
	}
	return "", "", false
}

// davName is an account's folder name: its name, with the start of its ID added when two accounts share it.
func davName(a store.Account, all []store.Account) string {
	n := strings.ReplaceAll(strings.TrimSpace(a.Name), "/", "∕")
	for _, b := range all {
		if b.ID != a.ID && strings.TrimSpace(b.Name) == strings.TrimSpace(a.Name) {
			return n + " (" + a.ID[:min(6, len(a.ID))] + ")"
		}
	}
	return n
}

// local checks a path of the owner's files: no way out of the root, no Cloud Companion bookkeeping files.
func (d *davFS) checkLocal(name string) error {
	c := strings.Trim(path.Clean("/"+name), "/")
	for _, seg := range strings.Split(c, "/") {
		if strings.HasPrefix(seg, ".phe-") {
			return os.ErrNotExist
		}
	}
	if _, err := d.f.fs.Abs(c); err != nil {
		return os.ErrPermission
	}
	return nil
}

func (d *davFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if _, _, ok := d.split(name); ok {
		return errDavReadOnly
	}
	if err := d.checkLocal(name); err != nil {
		return err
	}
	return d.local.Mkdir(ctx, name, perm)
}

func (d *davFS) RemoveAll(ctx context.Context, name string) error {
	if _, rest, ok := d.split(name); ok || rest != "" {
		return errDavReadOnly
	}
	if strings.Trim(name, "/") == "" {
		return os.ErrPermission
	}
	if err := d.checkLocal(name); err != nil {
		return err
	}
	return d.local.RemoveAll(ctx, name)
}

func (d *davFS) Rename(ctx context.Context, from, to string) error {
	_, _, a := d.split(from)
	_, _, b := d.split(to)
	if a || b {
		return errDavReadOnly
	}
	if err := d.checkLocal(from); err != nil {
		return err
	}
	if err := d.checkLocal(to); err != nil {
		return err
	}
	return d.local.Rename(ctx, from, to)
}

func (d *davFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if id, rest, ok := d.split(name); ok {
		src, err := d.f.source(ctx, id)
		if err != nil {
			return nil, os.ErrNotExist
		}
		e, err := src.Stat(ctx, rest)
		if err != nil {
			return nil, davErr(err)
		}
		e.Name = path.Base("/" + strings.Trim(name, "/"))
		return entryInfo{e}, nil
	}
	if err := d.checkLocal(name); err != nil {
		return nil, err
	}
	return d.local.Stat(ctx, name)
}

func (d *davFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if id, rest, ok := d.split(name); ok {
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
			return nil, errDavReadOnly
		}
		src, err := d.f.source(ctx, id)
		if err != nil {
			return nil, os.ErrNotExist
		}
		e, err := src.Stat(ctx, rest)
		if err != nil {
			return nil, davErr(err)
		}
		e.Name = path.Base("/" + strings.Trim(name, "/"))
		return &remoteFile{src: src, p: rest, e: e}, nil
	}
	if err := d.checkLocal(name); err != nil {
		return nil, err
	}
	fh, err := d.local.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}
	if strings.Trim(name, "/") == "" {
		return &rootDir{File: fh, d: d}, nil
	}
	return &hideBookkeeping{fh}, nil
}

func davErr(err error) error {
	if errors.Is(err, remote.ErrNotFound) || errors.Is(err, files.ErrBadPath) {
		return os.ErrNotExist
	}
	return err
}

// entryInfo is a files.Entry as an os.FileInfo.
type entryInfo struct{ e files.Entry }

func (i entryInfo) Name() string       { return i.e.Name }
func (i entryInfo) Size() int64        { return i.e.Size }
func (i entryInfo) ModTime() time.Time { return i.e.Mod }
func (i entryInfo) IsDir() bool        { return i.e.Dir }
func (i entryInfo) Sys() any           { return nil }
func (i entryInfo) Mode() fs.FileMode {
	if i.e.Dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

// ContentType saves WebDAV from reading the start of a file of another service to guess its type.
func (i entryInfo) ContentType(context.Context) (string, error) {
	if i.e.Type == "" {
		return "application/octet-stream", nil
	}
	return i.e.Type, nil
}

// rootDir is the top: the owner's files and a folder per account of another service.
type rootDir struct {
	webdav.File
	d *davFS
}

func (r *rootDir) Readdir(n int) ([]os.FileInfo, error) {
	l, err := (&hideBookkeeping{r.File}).Readdir(-1)
	if err != nil {
		return nil, err
	}
	all := r.d.f.mounts()
	for _, a := range all {
		l = append(l, entryInfo{files.Entry{Name: davName(a, all), Dir: true, Mod: time.Now()}})
	}
	return l, nil
}

type hideBookkeeping struct{ webdav.File }

func (h *hideBookkeeping) Readdir(n int) ([]os.FileInfo, error) {
	l, err := h.File.Readdir(n)
	out := l[:0]
	for _, fi := range l {
		if !strings.HasPrefix(fi.Name(), ".phe-") {
			out = append(out, fi)
		}
	}
	return out, err
}

// remoteFile is a file or folder of another service. Reading streams it from where it lives: a seek starts a new request
// for the bytes from there on (Range), so players and copies that jump around work without holding the file here.
type remoteFile struct {
	src remote.Source
	p   string
	e   files.Entry

	mu   sync.Mutex
	off  int64
	body io.ReadCloser
	stop context.CancelFunc
}

func (f *remoteFile) Stat() (os.FileInfo, error) { return entryInfo{f.e}, nil }
func (f *remoteFile) Write([]byte) (int, error)  { return 0, errDavReadOnly }

func (f *remoteFile) Readdir(n int) ([]os.FileInfo, error) {
	if !f.e.Dir {
		return nil, errors.New("not a folder")
	}
	l, err := f.src.List(context.Background(), f.p)
	if err != nil {
		return nil, davErr(err)
	}
	out := make([]os.FileInfo, 0, len(l))
	for _, e := range l {
		out = append(out, entryInfo{e})
	}
	return out, nil
}

func (f *remoteFile) Seek(off int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch whence {
	case io.SeekCurrent:
		off += f.off
	case io.SeekEnd:
		off += f.e.Size
	}
	if off < 0 {
		return 0, errors.New("seek before the start")
	}
	if off != f.off {
		f.closeBody()
		f.off = off
	}
	return off, nil
}

func (f *remoteFile) Read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.e.Dir {
		return 0, errors.New("a folder")
	}
	if f.e.Size > 0 && f.off >= f.e.Size {
		return 0, io.EOF
	}
	if f.body == nil {
		ctx, cancel := context.WithCancel(context.Background())
		pr, pw := io.Pipe()
		r, _ := http.NewRequestWithContext(ctx, "GET", "/", nil)
		if f.off > 0 {
			r.Header.Set("Range", "bytes="+itoa(f.off)+"-")
		}
		go func() {
			w := &pipeWriter{pw: pw, h: http.Header{}}
			f.src.Serve(w, r, f.p)
			if w.code >= 400 {
				pw.CloseWithError(errors.New("the other service answered " + itoa(int64(w.code))))
				return
			}
			pw.Close()
		}()
		f.body, f.stop = pr, cancel
	}
	n, err := f.body.Read(b)
	f.off += int64(n)
	return n, err
}

func (f *remoteFile) closeBody() {
	if f.body != nil {
		f.stop()
		f.body.Close()
		f.body, f.stop = nil, nil
	}
}

func (f *remoteFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeBody()
	return nil
}

// pipeWriter is the ResponseWriter a Source serves a file into: its body goes into the pipe, an error answer is kept back.
type pipeWriter struct {
	pw   *io.PipeWriter
	h    http.Header
	code int
}

func (w *pipeWriter) Header() http.Header { return w.h }
func (w *pipeWriter) WriteHeader(c int) {
	if w.code == 0 {
		w.code = c
	}
}
func (w *pipeWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	if w.code >= 400 {
		return len(b), nil
	}
	return w.pw.Write(b)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// davRoutes serves /dav/ to phone logins. Wrong passwords slow down: one second more for each in a row from an address, up to 30.
func (f *filesAPI) davRoutes(mux *http.ServeMux) {
	h := &webdav.Handler{
		Prefix:     davPrefix,
		FileSystem: &davFS{f: f, local: webdav.Dir(f.fs.Dir)},
		LockSystem: webdav.NewMemLS(),
	}
	var mu sync.Mutex
	fails := map[string]int{}
	serve := func(w http.ResponseWriter, r *http.Request) {
		ip := infoOf(r).ClientIP
		u, p, ok := r.BasicAuth()
		ha1 := ""
		if ok {
			u = strings.ToLower(u)
			ha1 = f.st.SIPUserHA1(u)
		}
		if !ok || ha1 == "" || subtle.ConstantTimeCompare([]byte(ha1), []byte(store.SIPHA1(u, p))) != 1 {
			if ok {
				mu.Lock()
				fails[ip]++
				n := min(fails[ip], 30)
				mu.Unlock()
				time.Sleep(time.Duration(n) * time.Second)
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="Cloud Companion", charset="UTF-8"`)
			http.Error(w, "sign in with a phone login", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		delete(fails, ip)
		mu.Unlock()
		if f.fs.MaxFile > 0 && r.Method == "PUT" && r.ContentLength > f.fs.MaxFile {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Del("Content-Security-Policy")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		h.ServeHTTP(w, r)
	}
	mux.HandleFunc(davPrefix+"/", serve)
	mux.HandleFunc(davPrefix, serve)
}
