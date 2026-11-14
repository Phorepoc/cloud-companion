// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/files"
)

// Seafile reads a Seafile server through its web API (SeafDAV is optional and often off): the libraries are the top folders.
// Encrypted libraries are left out (their files can only be read with the library's password, on the person's devices).
type Seafile struct {
	Server string
	token  string
	HTTP   *http.Client

	mu    sync.Mutex
	repos map[string]string // library name as shown -> id
}

// NewSeafile signs in: secret is the password, or an API token (Seafile → Settings → Web API Auth Token; needed with two-step login).
func NewSeafile(ctx context.Context, server, user, secret string, insecure bool) (*Seafile, error) {
	s := strings.TrimRight(strings.TrimSpace(server), "/")
	if u, err := url.Parse(s); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("the Seafile address is needed, as https://seafile.example.org")
	}
	sf := &Seafile{Server: s, token: secret, HTTP: client(insecure)}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var pong string
	if err := sf.get(ctx, "/api2/auth/ping/", &pong); err == nil && pong == "pong" {
		return sf, nil // an API token
	}
	form := url.Values{"username": {user}, "password": {secret}}
	r, _ := http.NewRequestWithContext(ctx, "POST", s+"/api2/auth-token/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := sf.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.Header.Get("X-Seafile-OTP") == "required" {
		return nil, fmt.Errorf("%w: this account uses two-step login; give an API token instead of the password (Seafile → Settings → Web API Auth Token)", ErrAuth)
	}
	var j struct {
		Token string `json:"token"`
	}
	if res.StatusCode == 400 || res.StatusCode == 401 || res.StatusCode == 403 {
		return nil, ErrAuth
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&j) != nil || j.Token == "" {
		return nil, fmt.Errorf("this does not look like a Seafile server (%s)", res.Status)
	}
	sf.token = j.Token
	return sf, nil
}

func (sf *Seafile) get(ctx context.Context, p string, v any) error {
	r, err := http.NewRequestWithContext(ctx, "GET", sf.Server+p, nil)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Token "+sf.token)
	r.Header.Set("Accept", "application/json")
	res, err := sf.HTTP.Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		return ErrAuth
	case res.StatusCode == 404:
		return ErrNotFound
	case res.StatusCode != 200:
		return fmt.Errorf("the server answered %s", res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(v)
}

// libraries lists the libraries the person can read, as top folders: a name two libraries share gets the start of its ID added.
func (sf *Seafile) libraries(ctx context.Context) ([]files.Entry, error) {
	var l []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		Mtime     int64  `json:"mtime"`
		Encrypted bool   `json:"encrypted"`
	}
	if err := sf.get(ctx, "/api2/repos/", &l); err != nil {
		return nil, err
	}
	count, seen := map[string]int{}, map[string]bool{}
	for _, r := range l {
		if !seen[r.ID] {
			seen[r.ID] = true
			count[r.Name]++
		}
	}
	repos := map[string]string{}
	out := []files.Entry{}
	clear(seen)
	for _, r := range l {
		if r.Encrypted || seen[r.ID] {
			continue // encrypted, or the same library listed twice (shared with you and in a group)
		}
		seen[r.ID] = true
		name := strings.ReplaceAll(r.Name, "/", "-")
		if count[r.Name] > 1 && len(r.ID) >= 6 {
			name += " (" + r.ID[:6] + ")"
		}
		repos[name] = r.ID
		out = append(out, entry("", name, true, r.Size, time.Unix(r.Mtime, 0)))
	}
	sf.mu.Lock()
	sf.repos = repos
	sf.mu.Unlock()
	files.Sort(out)
	return out, nil
}

// split turns "Library/a/b" into the library's id and "/a/b".
func (sf *Seafile) split(ctx context.Context, c string) (string, string, error) {
	lib, rest, _ := strings.Cut(c, "/")
	sf.mu.Lock()
	id, ok := sf.repos[lib]
	sf.mu.Unlock()
	if !ok {
		if _, err := sf.libraries(ctx); err != nil {
			return "", "", err
		}
		sf.mu.Lock()
		id, ok = sf.repos[lib]
		sf.mu.Unlock()
		if !ok {
			return "", "", ErrNotFound
		}
	}
	return id, "/" + rest, nil
}

// List is what is in the folder p: the libraries at the top.
func (sf *Seafile) List(ctx context.Context, p string) ([]files.Entry, error) {
	c, err := clean(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if c == "" {
		return sf.libraries(ctx)
	}
	id, in, err := sf.split(ctx, c)
	if err != nil {
		return nil, err
	}
	var l []struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		Mtime int64  `json:"mtime"`
	}
	if err := sf.get(ctx, "/api2/repos/"+url.PathEscape(id)+"/dir/?p="+url.QueryEscape(in), &l); err != nil {
		return nil, err
	}
	out := []files.Entry{}
	for _, x := range l {
		out = append(out, entry(c, x.Name, x.Type == "dir", x.Size, time.Unix(x.Mtime, 0)))
	}
	files.Sort(out)
	return out, nil
}

// Stat is p itself, from its folder's listing.
func (sf *Seafile) Stat(ctx context.Context, p string) (files.Entry, error) {
	c, err := clean(p)
	if err != nil {
		return files.Entry{}, err
	}
	if c == "" {
		return files.Entry{Dir: true}, nil
	}
	parent := path.Dir(c)
	if parent == "." {
		parent = ""
	}
	l, err := sf.List(ctx, parent)
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

// Serve asks Seafile for a download link to the file and passes the file on from there, Range and all.
func (sf *Seafile) Serve(w http.ResponseWriter, r *http.Request, p string) {
	c, err := clean(p)
	if err != nil || !strings.Contains(c, "/") {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	id, in, err := sf.split(ctx, c)
	var link string
	if err == nil {
		err = sf.get(ctx, "/api2/repos/"+url.PathEscape(id)+"/file/?reuse=1&p="+url.QueryEscape(in), &link)
	}
	cancel()
	if err != nil {
		status(w, err)
		return
	}
	up, err := http.NewRequestWithContext(r.Context(), "GET", link, nil)
	if err != nil {
		status(w, err)
		return
	}
	forward(up, r)
	res, err := sf.HTTP.Do(up)
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
