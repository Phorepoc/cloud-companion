// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// WebDAV under remote.php/dav/files/<user>/: PROPFIND (depth 0 and 1) and GET, read-only (Cloud Companion only reads).
package nextcloud

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
)

func (s *Server) dav(w http.ResponseWriter, r *http.Request) {
	u := r.PathValue("user")
	if me := s.user(r); me == "" || me != u {
		w.Header().Set("WWW-Authenticate", `Basic realm="Nextcloud", charset="UTF-8"`)
		http.Error(w, "not signed in", http.StatusUnauthorized)
		return
	}
	top := "/remote.php/dav/files/" + u + "/"
	p := strings.Trim(strings.TrimPrefix(r.URL.Path, top), "/")
	s.mu.Lock()
	tree := s.files[u]
	n, ok := tree[p]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	switch r.Method {
	case "GET", "HEAD":
		if n.dir {
			http.Error(w, "a folder", http.StatusNotImplemented)
			return
		}
		w.Header().Set("Content-Type", mimeOf(p))
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, n.mod.Unix()))
		http.ServeContent(w, r, path.Base(p), n.mod, bytes.NewReader(n.data))
	case "PROPFIND":
		names := []string{p}
		if n.dir && r.Header.Get("Depth") != "0" {
			s.mu.Lock()
			for q := range tree {
				if q != "" && path.Dir(q) == dirOf(p) {
					names = append(names, q)
				}
			}
			s.mu.Unlock()
			sort.Strings(names[1:])
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?>` + "\n" + `<d:multistatus xmlns:d="DAV:" xmlns:s="http://sabredav.org/ns" xmlns:oc="http://owncloud.org/ns">`)
		for _, q := range names {
			s.mu.Lock()
			m := tree[q]
			s.mu.Unlock()
			href := (&url.URL{Path: top + q}).EscapedPath()
			prop := "<d:resourcetype/><d:getcontentlength>" + fmt.Sprint(len(m.data)) + "</d:getcontentlength>"
			if m.dir {
				if q != "" {
					href += "/"
				}
				prop = "<d:resourcetype><d:collection/></d:resourcetype>"
			}
			b.WriteString("<d:response><d:href>" + xmlText(href) + "</d:href><d:propstat><d:prop>" + prop +
				"<d:getlastmodified>" + m.mod.UTC().Format(http.TimeFormat) + "</d:getlastmodified></d:prop>" +
				"<d:status>HTTP/1.1 200 OK</d:status></d:propstat>" +
				"<d:propstat><d:prop><d:displayname/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response>")
		}
		b.WriteString("</d:multistatus>")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(207)
		w.Write([]byte(b.String()))
	default: // read-only here: Nextcloud would take these, Cloud Companion never sends them
		http.Error(w, "read-only", http.StatusForbidden)
	}
}

// dirOf is how path.Dir names the parent of what is in folder p ("" for the top).
func dirOf(p string) string {
	if p == "" {
		return "."
	}
	return p
}

func mimeOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	}
	return "application/octet-stream"
}

func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
