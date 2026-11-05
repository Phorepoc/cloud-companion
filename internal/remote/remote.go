// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package remote reads the files of other services, so the Files section (and every paired device through its API) shows them
// next to the owner's own: a WebDAV server (Nextcloud and most others) or a Seafile server. Read-only for now: list, look at,
// download and stream (Range requests are passed on, so players can seek).
package remote

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/files"
)

// Source is one account's files. Paths are slash paths relative to its top ("" is the top), as in package files.
type Source interface {
	List(ctx context.Context, p string) ([]files.Entry, error)
	Stat(ctx context.Context, p string) (files.Entry, error)
	// Serve answers r with the file at p: its bytes, honouring Range.
	Serve(w http.ResponseWriter, r *http.Request, p string)
}

var (
	// ErrNotFound: no such file or folder.
	ErrNotFound = errors.New("not found")
	// ErrAuth: the server refused the login.
	ErrAuth = errors.New("the server refused the login")
)

// clean makes p a relative slash path without "." or "..": what package files does for the owner's own files.
func clean(p string) (string, error) {
	c, err := files.Clean(p)
	if err != nil {
		return "", err
	}
	return c, nil
}

func entry(dir, name string, isDir bool, size int64, mod time.Time) files.Entry {
	e := files.Entry{Name: name, Path: path.Join(dir, name), Dir: isDir, Size: size, Mod: mod}
	if !isDir {
		e.Type = files.TypeOf(name)
	}
	return e
}

// client is the HTTP client for other services: a time limit for listings; downloads are limited by their request's context.
func client(insecure bool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig.InsecureSkipVerify = true // the owner asked for it: a server of their own with a self-made certificate
	}
	return &http.Client{Transport: tr}
}

// passOn copies an upstream answer for a file to the browser: the bytes and the headers a player needs (length, ranges,
// type), shown as data and never run as a page of this site.
func passOn(w http.ResponseWriter, up *http.Response, name string) {
	for _, h := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := up.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	t := files.TypeOf(name)
	if t == "" {
		t = "application/octet-stream"
	}
	w.Header().Set("Content-Type", t)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'")
	w.WriteHeader(up.StatusCode)
	io.Copy(w, up.Body) // a browser that goes away ends it
}

// forward is what a GET for a file passes on from the browser's request.
func forward(dst, src *http.Request) {
	for _, h := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if v := src.Header.Get(h); v != "" {
			dst.Header.Set(h, v)
		}
	}
}

func status(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrAuth):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		http.Error(w, "the other server did not answer: "+strings.TrimSpace(err.Error()), http.StatusBadGateway)
	}
}
