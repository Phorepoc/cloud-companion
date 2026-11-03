// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The catalogue library (docs/catalogue.md). Owner API, signed like the other catalogue calls:
//
//	GET  /phe/catalogue/library?catalogue=     the library: every package file with what was read from it, the owner's edits, its status and whether
//	                                           this catalogue publishes it; plus the folder, the last scan and the scan interval
//	POST /phe/catalogue/library/scan           rescan the folder now -> what was added, changed, removed
//	POST /phe/catalogue/library/publish        {catalogue, ids, publish}   publish or unpublish entries
//	POST /phe/catalogue/library/edit           {id, edits: {name, version, ...}}   replace the owner's edits of an entry
//	POST /phe/catalogue/library/remove         {id, delete_file}   delete the entry (and the file from the folder)
//	POST /phe/catalogue/upload                 multipart: file (+ catalogue, publish, app_id, version, name, summary, description, ... icon): the file lands
//	                                           in the library (uploads/); with a catalogue and publish (default 1) it is published there at once
//	POST /phe/catalogue/release/remove         {catalogue, app_id, version}   older name for removing the entry published under that id and version

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/phorepoc/cloud-companion/internal/catalogue"
	"github.com/phorepoc/cloud-companion/internal/store"
)

type libEntry struct {
	store.CatFile
	Meta        catalogue.Meta    `json:"meta"`
	Edits       catalogue.Edits   `json:"edits"`
	Name        string            `json:"name"`
	AppID       string            `json:"app_id"`
	Version     string            `json:"version"`
	Platform    string            `json:"platform"`
	Package     string            `json:"package"`
	Arch        []string          `json:"arch"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Problem     string            `json:"problem,omitempty"` // why it cannot be published as it is
	Published   bool              `json:"published"`
	PublishedIn []string          `json:"published_in"`
	IconData    string            `json:"icon_data,omitempty"`
	IconNote    string            `json:"icon_note,omitempty"`
	Extra       map[string]string `json:"-"`
}

func (c *catAPI) iconData(sha string) string {
	if sha == "" {
		return ""
	}
	f, fi, err := c.m.OpenBlob(sha)
	if err != nil || fi.Size() > 256<<10 {
		return ""
	}
	defer f.Close()
	b := make([]byte, fi.Size())
	if n, _ := f.Read(b); n != len(b) {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}

func (c *catAPI) entries(catID string) []libEntry {
	pub := c.st.CatPublished(catID)
	out := []libEntry{}
	for _, f := range c.st.CatFiles() {
		e := libEntry{CatFile: f}
		json.Unmarshal([]byte(f.Meta), &e.Meta)
		json.Unmarshal([]byte(f.Edits), &e.Edits)
		rel, err := c.m.Release(f)
		if err != nil {
			e.Problem = err.Error()
		}
		e.Name, e.AppID, e.Version, e.Platform, e.Package, e.Arch, e.Summary, e.Description = rel.Name, rel.AppID, rel.Version, rel.Platform, rel.Package, rel.Arch, rel.Summary, rel.Description
		if e.Name == "" {
			e.Name = e.Meta.Name
		}
		if e.AppID == "" {
			e.AppID = e.Meta.AppID
		}
		_, e.Published = pub[f.ID]
		e.PublishedIn = c.st.CatPublishedIn(f.ID)
		icon := f.IconSHA
		if e.Edits.Icon != nil {
			icon = e.Edits.Icon.SHA256
		} else if f.Kind == "stored" && rel.Icon != nil {
			icon = rel.Icon.SHA256
		}
		e.IconData = c.iconData(icon)
		e.IconNote = e.Meta.IconNote
		if f.Kind == "stored" {
			e.Path = "(stored by an earlier version)"
		}
		out = append(out, e)
	}
	return out
}

func (c *catAPI) libraryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/phe/catalogue/library", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) {
			return
		}
		cat, ok := c.st.Catalogue(r.URL.Query().Get("catalogue"))
		if !ok {
			bad(w, 404, "unknown catalogue")
			return
		}
		last, at := c.m.LastScan()
		reply(w, 200, map[string]any{"entries": c.entries(cat.ID), "library": map[string]any{"path": c.m.Library, "scan_interval": int(c.m.ScanEvery.Seconds()),
			"last_scan": at, "last_result": last}})
	})
	mux.HandleFunc("/phe/catalogue/library/scan", func(w http.ResponseWriter, r *http.Request) {
		if !c.admin(w, r) || r.Method != http.MethodPost {
			return
		}
		res, err := c.m.ScanNow(r.Context())
		if err != nil {
			bad(w, 409, err.Error())
			return
		}
		reply(w, 200, map[string]any{"result": res})
	})
	mux.HandleFunc("/phe/catalogue/library/publish", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Catalogue string
			IDs       []string `json:"ids"`
			Publish   bool
		}
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		if _, ok := c.st.Catalogue(in.Catalogue); !ok {
			bad(w, 404, "unknown catalogue")
			return
		}
		n, problems := c.m.Publish(in.Catalogue, in.IDs, in.Publish)
		reply(w, 200, map[string]any{"changed": n, "problems": problems})
	})
	mux.HandleFunc("/phe/catalogue/library/edit", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID    string
			Edits catalogue.Edits
		}
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		if err := c.m.SaveEdits(in.ID, in.Edits); err != nil {
			bad(w, 400, err.Error())
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/phe/catalogue/library/icon", func(w http.ResponseWriter, r *http.Request) {
		// a new icon for an entry: the PNG as the request body
		if !c.admin(w, r) || r.Method != http.MethodPost {
			return
		}
		f, ok := c.st.CatFile(r.URL.Query().Get("id"))
		if !ok {
			bad(w, 404, "unknown entry")
			return
		}
		sha, size, err := c.m.PutBlob(http.MaxBytesReader(w, r.Body, 4<<20), 4<<20, "")
		if err != nil {
			bad(w, 413, err.Error())
			return
		}
		e := c.m.EditsOf(f)
		e.Icon = &catalogue.Blob{Name: "icon.png", SHA256: sha, Size: size}
		if err := c.m.SaveEdits(f.ID, e); err != nil {
			bad(w, 400, err.Error())
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/phe/catalogue/library/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID         string
			DeleteFile bool `json:"delete_file"`
		}
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		if err := c.m.DeleteEntry(in.ID, in.DeleteFile); err != nil {
			bad(w, 400, err.Error())
			return
		}
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/phe/catalogue/release/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Catalogue string
			AppID     string `json:"app_id"`
			Version   string
		}
		if !c.admin(w, r) || !readJSON(w, r, &in) {
			return
		}
		for _, f := range c.st.CatFiles() {
			if _, pub := c.st.CatPublished(in.Catalogue)[f.ID]; !pub {
				continue
			}
			if rel, err := c.m.Release(f); err == nil && rel.AppID == in.AppID && rel.Version == in.Version {
				if err := c.m.DeleteEntry(f.ID, true); err != nil {
					bad(w, 500, err.Error())
					return
				}
				reply(w, 200, map[string]any{"ok": true})
				return
			}
		}
		bad(w, 404, "no such release")
	})
	mux.HandleFunc("/phe/catalogue/upload", c.upload)
}

func formList(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// upload puts one file into the library and, when a catalogue is named, publishes it there. The text fields are the owner's edits.
func (c *catAPI) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !c.admin(w, r) {
		return
	}
	limit := c.m.MaxFile
	if limit <= 0 {
		limit = 2 << 30
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+64<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		bad(w, 400, "bad upload: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()
	var cat store.Catalogue
	if id := r.FormValue("catalogue"); id != "" {
		var ok bool
		if cat, ok = c.st.Catalogue(id); !ok {
			bad(w, 404, "unknown catalogue")
			return
		}
	}
	hdrs := r.MultipartForm.File["file"]
	if len(hdrs) != 1 {
		bad(w, 400, "one installation file is needed (field \"file\")")
		return
	}
	f, err := hdrs[0].Open()
	if err != nil {
		bad(w, 400, "bad upload")
		return
	}
	entry, err := c.m.AddUpload(hdrs[0].Filename, f, limit)
	f.Close()
	if err != nil {
		bad(w, 400, err.Error())
		return
	}
	e := c.m.EditsOf(entry)
	for k, p := range map[string]*string{"app_id": &e.AppID, "version": &e.Version, "name": &e.Name, "summary": &e.Summary, "description": &e.Description,
		"category": &e.Category, "license": &e.License, "homepage": &e.Homepage, "platform": &e.Platform} {
		if v := strings.TrimSpace(r.FormValue(k)); v != "" {
			*p = v
		}
	}
	if v := r.FormValue("authors"); v != "" {
		e.Authors = formList(v)
	}
	if v := r.FormValue("arch"); v != "" {
		e.Arch = splitList(v)
	}
	if v := r.FormValue("boards"); v != "" {
		e.Boards = splitList(v)
	}
	if ifh := r.MultipartForm.File["icon"]; len(ifh) == 1 {
		if ic, err := ifh[0].Open(); err == nil {
			if sha, size, err := c.m.PutBlob(ic, 4<<20, ""); err == nil {
				e.Icon = &catalogue.Blob{Name: "icon.png", SHA256: sha, Size: size}
			}
			ic.Close()
		}
	}
	if err := c.m.SaveEdits(entry.ID, e); err != nil {
		bad(w, 400, err.Error())
		return
	}
	entry, _ = c.st.CatFile(entry.ID)
	out := map[string]any{"entry": entry.ID}
	if rel, err := c.m.Release(entry); err == nil {
		out["release"] = rel
	}
	if cat.ID != "" && r.FormValue("publish") != "0" {
		if rel, err := c.m.Release(entry); err == nil {
			// uploading again under the same id and version replaces the earlier one
			for _, o := range c.st.CatFiles() {
				if _, pub := c.st.CatPublished(cat.ID)[o.ID]; pub && o.ID != entry.ID {
					if or, err := c.m.Release(o); err == nil && or.AppID == rel.AppID && or.Version == rel.Version {
						c.st.CatPublishSet(cat.ID, o.ID, false, 0)
					}
				}
			}
		}
		if _, problems := c.m.Publish(cat.ID, []string{entry.ID}, true); len(problems) > 0 {
			bad(w, 400, "stored in the library, but not published: "+problems[entry.ID])
			return
		}
		if rel, err := c.m.Release(entry); err == nil {
			out["url"] = prefix(publicBase(r), cat) + "/files/" + rel.File.SHA256 + "/" + rel.File.Name
		}
	}
	if os.Getenv("PHECLOUD_DEBUG") != "" {
		out["path"] = entry.Path
	}
	reply(w, 200, out)
}
