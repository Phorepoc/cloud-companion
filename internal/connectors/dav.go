// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// DAVCfg is the config of a "carddav" or "caldav" account: one collection URL (an address book or a calendar) and the user.
type DAVCfg struct {
	URL   string `json:"url"`
	User  string `json:"user"`
	OAuth string `json:"oauth,omitempty"` // "google": the credential is an OAuth access token (Bearer)
}

// HubDAV is the hub side (davhub.Client).
type HubDAV interface {
	PutRaw(kind, name, body string) error
	DeleteRaw(kind, name string) error
}

// SyncStore is what a DAV sync remembers (store.Store).
type SyncStore interface {
	SyncedGet(account, key string) (string, bool)
	SyncedSet(account, key, value string)
	SyncedDelete(account, key string)
	SyncedKeys(account, prefix string) map[string]string
	LinkContact(hubID, source, entryID string) error
	LinkTarget(source, entryID string) (string, bool)
}

type propfind struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Prop struct {
				ETag         string `xml:"getetag"`
				ResourceType struct {
					Collection *struct{} `xml:"collection"`
				} `xml:"resourcetype"`
			} `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

func davClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}
}

func davDo(ctx context.Context, c DAVCfg, pass, method, u, body string, hdr map[string]string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if tok, ok := isCred(pass); ok { // OAuth (Google): a Bearer token
		req.Header.Set("Authorization", "Bearer "+tok)
	} else if c.User != "" {
		req.SetBasicAuth(c.User, pass)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := davClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, b, nil
}

type davItem struct{ href, etag string }

func listDAV(ctx context.Context, c DAVCfg, pass string) ([]davItem, error) {
	code, b, err := davDo(ctx, c, pass, "PROPFIND", c.URL, `<?xml version="1.0"?><propfind xmlns="DAV:"><prop><getetag/><resourcetype/></prop></propfind>`,
		map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if err != nil {
		return nil, err
	}
	if code == 401 || code == 403 {
		return nil, fmt.Errorf("login refused (HTTP %d)", code)
	}
	if code != 207 {
		return nil, fmt.Errorf("the server answered HTTP %d to PROPFIND", code)
	}
	var m propfind
	if err := xml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	base, _ := url.Parse(c.URL)
	var out []davItem
	for _, r := range m.Responses {
		h, err := url.PathUnescape(r.Href)
		if err != nil || strings.TrimRight(h, "/") == strings.TrimRight(base.Path, "/") {
			continue
		}
		coll, etag := false, ""
		for _, ps := range r.Propstat {
			if ps.Prop.ResourceType.Collection != nil {
				coll = true
			}
			if ps.Prop.ETag != "" {
				etag = strings.TrimSpace(ps.Prop.ETag)
			}
		}
		if !coll {
			out = append(out, davItem{h, etag})
		}
	}
	return out, nil
}

// TestDAV checks the login and counts the resources.
func TestDAV(ctx context.Context, c DAVCfg, pass string) (int, error) {
	l, err := listDAV(ctx, c, pass)
	return len(l), err
}

var uidRe = regexp.MustCompile(`(?im)^UID[^:\r\n]*:(.+?)\r?$`)

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SyncDAV pulls a collection into the hub (one way: outside account -> hub cache). Changed and new resources are written to the hub
// under "<account>-<uid>", resources deleted at the source are deleted in the hub. kind is "card" or "cal". For contacts every
// pulled entry is also recorded in the link table (hub contact id <-> source account + entry href).
func SyncDAV(ctx context.Context, account, kind string, c DAVCfg, pass string, hub HubDAV, st SyncStore) (changed, removed int, err error) {
	items, err := listDAV(ctx, c, pass)
	if err != nil {
		return 0, 0, err
	}
	ext := ".vcf"
	if kind == "cal" {
		ext = ".ics"
	}
	prefix := "dav:" + kind + ":"
	known := st.SyncedKeys(account, prefix)
	present := map[string]bool{}
	for _, it := range items {
		if !strings.HasSuffix(strings.ToLower(it.href), ext) {
			continue
		}
		key := prefix + it.href
		present[key] = true
		if old, ok := known[key]; ok && it.etag != "" && strings.HasPrefix(old, it.etag+"|") {
			continue
		}
		u := it.href
		if !strings.HasPrefix(u, "http") {
			base, _ := url.Parse(c.URL)
			u = base.Scheme + "://" + base.Host + u
		}
		code, body, err := davDo(ctx, c, pass, "GET", u, "", nil)
		if err != nil {
			return changed, removed, err
		}
		if code != 200 {
			continue
		}
		uid := strings.TrimSpace(path.Base(it.href))
		uid = strings.TrimSuffix(strings.TrimSuffix(uid, ".vcf"), ".ics")
		if m := uidRe.FindSubmatch(body); m != nil {
			uid = strings.TrimSpace(string(m[1]))
		}
		name := account + "-" + unsafeName.ReplaceAllString(uid, "_")
		if kind == "card" {
			if t, ok := st.LinkTarget(account, it.href); ok && t != name {
				// merged into another hub contact by the owner: do not bring the old one back; just remember the new version
				st.SyncedSet(account, key, it.etag+"|"+t+ext)
				continue
			}
		}
		if err := hub.PutRaw(kind, name+ext, string(body)); err != nil {
			return changed, removed, err
		}
		st.SyncedSet(account, key, it.etag+"|"+name+ext)
		if kind == "card" {
			st.LinkContact(name, account, it.href)
		}
		changed++
	}
	for key, v := range known {
		if present[key] {
			continue
		}
		if i := strings.Index(v, "|"); i >= 0 {
			if err := hub.DeleteRaw(kind, v[i+1:]); err != nil {
				return changed, removed, err
			}
		}
		st.SyncedDelete(account, key)
		removed++
	}
	return changed, removed, nil
}

// davDoH is davDo that also returns the response headers (ETag after a PUT, for the guarded merges).
func davDoH(ctx context.Context, c DAVCfg, pass, method, u, body string, hdr map[string]string) (int, []byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, strings.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	if tok, ok := isCred(pass); ok { // OAuth (Google): a Bearer token
		req.Header.Set("Authorization", "Bearer "+tok)
	} else if c.User != "" {
		req.SetBasicAuth(c.User, pass)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := davClient().Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, b, resp.Header, nil
}
