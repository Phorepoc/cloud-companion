// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/files"
)

// WebDAV reads a WebDAV server (RFC 4918): Nextcloud, ownCloud, most NAS boxes, Apache or nginx with WebDAV. Base is the folder
// that is the top (for Nextcloud https://host/remote.php/dav/files/<user>/).
type WebDAV struct {
	Base       *url.URL
	User, Pass string
	HTTP       *http.Client
}

// NewWebDAV checks the address and makes the client.
func NewWebDAV(base, user, pass string, insecure bool) (*WebDAV, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("the address is needed, as https://example.org/dav/")
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return &WebDAV{Base: u, User: user, Pass: pass, HTTP: client(insecure)}, nil
}

// at is the address of a path under the top, each part escaped.
func (d *WebDAV) at(p string, dir bool) *url.URL {
	u := *d.Base
	u.RawPath = ""
	if p != "" {
		u.Path += p
		if dir {
			u.Path += "/"
		}
	}
	return &u
}

func (d *WebDAV) req(ctx context.Context, method string, u *url.URL, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if d.User != "" || d.Pass != "" {
		r.SetBasicAuth(d.User, d.Pass)
	}
	return r, nil
}

const propfind = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/><d:displayname/></d:prop></d:propfind>`

type multistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Status string `xml:"status"`
			Prop   struct {
				Collection *struct{} `xml:"resourcetype>collection"`
				Length     int64     `xml:"getcontentlength"`
				Modified   string    `xml:"getlastmodified"`
			} `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

// find asks for p and, with depth 1, what is in it.
func (d *WebDAV) find(ctx context.Context, p string, depth string) ([]files.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := d.req(ctx, "PROPFIND", d.at(p, depth == "1"), strings.NewReader(propfind))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Depth", depth)
	r.Header.Set("Content-Type", "application/xml; charset=utf-8")
	res, err := d.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		return nil, ErrAuth
	case res.StatusCode == 404:
		return nil, ErrNotFound
	case res.StatusCode != 207:
		return nil, fmt.Errorf("the server answered %s", res.Status)
	}
	var ms multistatus
	if err := xml.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("not a WebDAV answer: %v", err)
	}
	var out []files.Entry
	for _, x := range ms.Responses {
		h, err := url.Parse(x.Href)
		if err != nil {
			continue
		}
		rel, ok := strings.CutPrefix(h.Path, d.Base.Path)
		if !ok {
			if strings.TrimSuffix(h.Path, "/")+"/" == d.Base.Path {
				rel = "" // the top itself
			} else {
				continue
			}
		}
		rel = strings.Trim(rel, "/")
		for _, ps := range x.Propstat {
			if !strings.Contains(ps.Status, " 200") {
				continue
			}
			mod, _ := http.ParseTime(ps.Prop.Modified)
			name := path.Base(rel)
			if rel == "" {
				name = ""
			}
			e := entry(path.Dir(rel), name, ps.Prop.Collection != nil, ps.Prop.Length, mod)
			e.Path = rel
			out = append(out, e)
		}
	}
	return out, nil
}

// List is what is in the folder p (the folder itself left out), folders first, by name.
func (d *WebDAV) List(ctx context.Context, p string) ([]files.Entry, error) {
	c, err := clean(p)
	if err != nil {
		return nil, err
	}
	all, err := d.find(ctx, c, "1")
	if err != nil {
		return nil, err
	}
	out := []files.Entry{}
	for _, e := range all {
		if e.Path != c {
			out = append(out, e)
		}
	}
	files.Sort(out)
	return out, nil
}

// Stat is p itself.
func (d *WebDAV) Stat(ctx context.Context, p string) (files.Entry, error) {
	c, err := clean(p)
	if err != nil {
		return files.Entry{}, err
	}
	l, err := d.find(ctx, c, "0")
	if err != nil {
		return files.Entry{}, err
	}
	for _, e := range l {
		if e.Path == c {
			return e, nil
		}
	}
	return files.Entry{}, ErrNotFound
}

// Serve passes the file on, Range and all.
func (d *WebDAV) Serve(w http.ResponseWriter, r *http.Request, p string) {
	c, err := clean(p)
	if err != nil || c == "" {
		http.NotFound(w, r)
		return
	}
	up, err := d.req(r.Context(), "GET", d.at(c, false), nil)
	if err != nil {
		status(w, err)
		return
	}
	forward(up, r)
	res, err := d.HTTP.Do(up)
	if err != nil {
		status(w, err)
		return
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		status(w, ErrAuth)
		return
	case res.StatusCode == 404:
		status(w, ErrNotFound)
		return
	case res.StatusCode >= 400:
		status(w, fmt.Errorf("%s", res.Status))
		return
	}
	passOn(w, res, path.Base(c))
}
