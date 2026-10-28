// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package catalogue is the self-hosted software catalogue (docs/catalogue.md): the owner uploads installation files, and Cloud Companion serves
// them, together with the listings of other catalogues it federates, as an index.json in the format of Phorepoc's official index
// (https://phorepoc.github.io/index/index.json), so the phone's Catalogue app can read it unchanged.
package catalogue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// Manager owns the catalogue files on disk and the sync of federated sources.
type Manager struct {
	St      *store.Store
	Dir     string       // PHECLOUD_CATALOGUE_DIR: blobs/ holds every file, named by its SHA-256
	Client  *http.Client // fetches sources (tests replace it)
	MaxFile int64        // the largest file accepted (upload, mirror); 0 = 2 GiB

	mu      sync.Mutex
	syncing map[string]bool
	vok     sync.Map // sha256 -> "size/mtime" of the last full verification
}

func New(st *store.Store, dir string) *Manager {
	return &Manager{St: st, Dir: dir, Client: &http.Client{Timeout: 30 * time.Minute}, syncing: map[string]bool{}}
}

func (m *Manager) maxFile() int64 {
	if m.MaxFile > 0 {
		return m.MaxFile
	}
	return 2 << 30
}

// ---- files

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func IsSHA256(s string) bool { return shaRe.MatchString(s) }

func (m *Manager) blobPath(sha string) string { return filepath.Join(m.Dir, "blobs", sha[:2], sha) }

// PutBlob stores what r delivers (at most max bytes) under its SHA-256 and returns it. When want is not empty the content must hash to it, else nothing is kept.
func (m *Manager) PutBlob(r io.Reader, max int64, want string) (sha string, size int64, err error) {
	tmpDir := filepath.Join(m.Dir, "tmp")
	if err = os.MkdirAll(tmpDir, 0o755); err != nil {
		return
	}
	f, err := os.CreateTemp(tmpDir, "up-*")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(r, max+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return
	}
	if size > max {
		return "", 0, fmt.Errorf("the file is larger than the limit (%d bytes)", max)
	}
	sha = hex.EncodeToString(h.Sum(nil))
	if want != "" && sha != want {
		return "", 0, fmt.Errorf("sha256 mismatch: the listing says %s, the file is %s", want, sha)
	}
	dst := m.blobPath(sha)
	if _, serr := os.Stat(dst); serr == nil {
		return // already there
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	err = os.Rename(f.Name(), dst)
	return
}

// OpenBlob opens a stored file after checking that its content still hashes to its name (re-hashed when it changed since the last check). A file that
// does not match is never handed out.
func (m *Manager) OpenBlob(sha string) (*os.File, os.FileInfo, error) {
	if !IsSHA256(sha) {
		return nil, nil, os.ErrNotExist
	}
	f, err := os.Open(m.blobPath(sha))
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	stamp := fmt.Sprint(fi.Size(), "/", fi.ModTime().UnixNano())
	if v, ok := m.vok.Load(sha); !ok || v != stamp {
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return nil, nil, err
		}
		if hex.EncodeToString(h.Sum(nil)) != sha {
			f.Close()
			return nil, nil, errors.New("the stored file does not match its SHA-256")
		}
		m.vok.Store(sha, stamp)
		f.Seek(0, io.SeekStart)
	}
	return f, fi, nil
}

func (m *Manager) blobOK(sha string) bool {
	f, _, err := m.OpenBlob(sha)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// ---- names and package types

// SafeName reduces a file name to something a phone's file system and the index consumer accept: no directories, no "..", at most 100 characters.
func SafeName(s string) string {
	s = path.Base(strings.ReplaceAll(s, "\\", "/"))
	if s == "." || s == "/" || s == ".." {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_', r == '+', r == '(', r == ')', r == ' ':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), " .")
	if len(out) > 100 { // keep the extension
		ext := path.Ext(out)
		if len(ext) > 12 {
			ext = ""
		}
		out = out[:100-len(ext)] + ext
	}
	return out
}

var packageTypes = map[string]string{ // extension -> platform
	"sis": "symbian", "sisx": "symbian", "apk": "android", "aab": "android", "xapk": "android", "exe": "windows", "msi": "windows", "msix": "windows",
	"appimage": "linux", "deb": "linux", "rpm": "linux", "flatpak": "linux", "snap": "linux", "dmg": "macos", "pkg": "macos", "ipa": "ios",
	"zip": "other", "tar.gz": "other", "tgz": "other", "7z": "other", "jar": "other", "jad": "other", "ipk": "other",
}

// PackageOf names the package type of a file by its extension ("sisx", "apk", "tar.gz", ...); "file" when it has none.
func PackageOf(name string) string {
	l := strings.ToLower(name)
	if strings.HasSuffix(l, ".tar.gz") {
		return "tar.gz"
	}
	if e := strings.TrimPrefix(path.Ext(l), "."); e != "" && len(e) <= 12 {
		return e
	}
	return "file"
}

// PlatformOf is the default platform of a package type.
func PlatformOf(pkg string) string {
	if p, ok := packageTypes[pkg]; ok {
		return p
	}
	return "other"
}

var tokenRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// ---- the hosted release

// Blob is a file of a catalogue (an icon, a screenshot, an installation file).
type Blob struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Release is one uploaded version of an app (what cat_releases.data holds as JSON).
type Release struct {
	AppID        string   `json:"app_id"`
	Version      string   `json:"version"`
	Name         string   `json:"name"`
	Summary      string   `json:"summary,omitempty"`
	Description  string   `json:"description,omitempty"`
	Category     string   `json:"category,omitempty"`
	License      string   `json:"license,omitempty"`
	Homepage     string   `json:"homepage,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	Platform     string   `json:"platform"` // symbian | android | windows | linux | macos | ios | other (or any lower-case word)
	Package      string   `json:"package"`  // sis | sisx | apk | exe | msi | appimage | deb | zip | ...
	Arch         []string `json:"arch,omitempty"`
	Boards       []string `json:"boards,omitempty"`
	MinCore      string   `json:"min_core,omitempty"`
	Legacy       bool     `json:"legacy,omitempty"`
	NeedsPlatsec bool     `json:"needs_platsec_hack,omitempty"`
	File         Blob     `json:"file"`
	Icon         *Blob    `json:"icon,omitempty"`
	Screenshots  []Blob   `json:"screenshots,omitempty"`
	Published    int64    `json:"published"`
}

var categories = map[string]bool{"app": true, "game": true, "driver": true, "library": true, "codec": true, "theme": true, "tool": true, "system": true}

var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~-]{0,63}$`)

// Normalize checks the owner's metadata and fills the defaults; the file part (File) is set by the caller.
func (r *Release) Normalize() error {
	r.AppID = strings.ToLower(strings.TrimSpace(r.AppID))
	if !store.IsCatalogueID(r.AppID) {
		return errors.New("the app id is lower case letters, digits, '-' and '.', 2 to 63 characters")
	}
	if !versionRe.MatchString(r.Version) {
		return errors.New("the version is up to 64 letters, digits and . _ + ~ -")
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || len(r.Name) > 64 {
		return errors.New("a name of 1 to 64 characters is needed")
	}
	if r.Summary = strings.TrimSpace(r.Summary); r.Summary == "" {
		r.Summary = strings.TrimSpace(strings.SplitN(strings.TrimSpace(r.Description), "\n", 2)[0])
	}
	if r.Summary == "" {
		r.Summary = r.Name
	}
	if len(r.Summary) > 140 {
		r.Summary = r.Summary[:140]
	}
	if r.Category == "" {
		r.Category = "app"
	}
	if !categories[r.Category] {
		return errors.New("the category is one of app, game, driver, library, codec, theme, tool, system")
	}
	if r.License == "" {
		r.License = "LicenseRef-unspecified"
	}
	if r.Homepage != "" && !regexp.MustCompile(`^https?://`).MatchString(r.Homepage) {
		return errors.New("the homepage is an http(s) address")
	}
	r.File.Name = SafeName(r.File.Name)
	if r.File.Name == "" {
		return errors.New("the file has no usable name")
	}
	if r.Package = strings.ToLower(strings.TrimSpace(r.Package)); r.Package == "" {
		r.Package = PackageOf(r.File.Name)
	}
	if !tokenRe.MatchString(r.Package) {
		return errors.New("the package type is a short lower case word (sis, apk, exe, deb ...)")
	}
	if r.Platform = strings.ToLower(strings.TrimSpace(r.Platform)); r.Platform == "" {
		r.Platform = PlatformOf(r.Package)
	}
	if !tokenRe.MatchString(r.Platform) {
		return errors.New("the platform is a short lower case word (symbian, android, windows, linux ...)")
	}
	if len(r.Arch) == 0 {
		// a Symbian package for Phorepoc phones is ARM unless the owner says otherwise; anything else is "any", which no Catalogue client matches
		// against its own architecture, so a phone does not list it
		if r.Platform == "symbian" {
			r.Arch = []string{"arm"}
		} else {
			r.Arch = []string{"any"}
		}
	}
	for _, a := range r.Arch {
		if !tokenRe.MatchString(a) {
			return errors.New("an architecture is a short lower case word (arm, x32, aarch64, x86_64, any)")
		}
	}
	return nil
}

// ---- merge helpers

func jsonCopy(v map[string]any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	json.Unmarshal(b, &out)
	return out
}

func str(v any) string { s, _ := v.(string); return s }

func isoTime(t int64) string { return time.Unix(t, 0).UTC().Format("2006-01-02T15:04:05Z") }

// hostedItem builds one index item from every uploaded version of an app (rels are oldest first); the newest upload gives the metadata.
func (m *Manager) hostedItem(c store.Catalogue, prefix string, rels []Release, files map[string]string) map[string]any {
	last := rels[len(rels)-1]
	item := map[string]any{
		"id": last.AppID, "name": last.Name, "summary": last.Summary, "category": last.Category, "license": last.License,
		"source": map[string]any{"type": "companion", "catalogue": c.ID},
	}
	if last.Description != "" {
		item["description"] = last.Description
	}
	if last.Homepage != "" {
		item["homepage"] = last.Homepage
	}
	if len(last.Authors) > 0 {
		item["authors"] = last.Authors
	}
	comp := map[string]any{"arch": last.Arch}
	if len(last.Boards) > 0 {
		comp["boards"] = last.Boards
	}
	if last.MinCore != "" {
		comp["min_core"] = last.MinCore
	}
	if last.Legacy {
		comp["legacy"] = true
	}
	if last.NeedsPlatsec {
		comp["needs_platsec_hack"] = true
	}
	item["compatibility"] = comp
	if last.Platform != "symbian" { // a Symbian entry looks exactly like an official one
		item["platform"] = last.Platform
	}
	file := func(b Blob) string {
		files[b.SHA256] = b.Name
		return prefix + "/files/" + b.SHA256 + "/" + b.Name
	}
	if last.Icon != nil {
		item["icon_url"] = file(*last.Icon)
	}
	shots := []string{}
	for _, s := range last.Screenshots {
		shots = append(shots, file(s))
	}
	item["screenshot_urls"] = shots
	tags, resolved := []string{}, []any{}
	for _, r := range rels {
		tags = append(tags, r.Version)
		e := map[string]any{"tag": r.Version, "published": isoTime(r.Published), "asset": r.File.Name, "url": file(r.File), "size": r.File.Size, "sha256": r.File.SHA256}
		if r.Platform != "symbian" || (r.Package != "sis" && r.Package != "sisx") {
			e["package"] = r.Package
		}
		resolved = append(resolved, e)
	}
	item["releases"] = tags
	item["resolved"] = resolved
	return item
}

// Cand is one source's claim on an app id.
type Cand struct {
	SourceID  string `json:"source_id"` // "local" for the hosted entries
	Source    string `json:"source"`
	Priority  int    `json:"priority"`
	Version   string `json:"version"` // the newest release's tag
	Published string `json:"published"`
	Mirrored  bool   `json:"mirrored"`
	Reason    string `json:"reason,omitempty"` // why it lost
	item      map[string]any
	mirror    int // effective: -1 not a mirroring source, else 0/1
}

// AppReport is one app id of the merged catalogue: who won and who else lists it.
type AppReport struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Winner Cand   `json:"winner"`
	Others []Cand `json:"others,omitempty"`
}

func newest(item map[string]any) (tag, pub string) {
	rs, _ := item["resolved"].([]any)
	for _, x := range rs {
		r, _ := x.(map[string]any)
		if p := str(r["published"]); tag == "" || p > pub {
			tag, pub = str(r["tag"]), p
		}
	}
	return
}

// Build merges the hosted releases and the stored listings of the enabled sources of one catalogue into the served items. prefix is the public
// address of the catalogue (token included for a private one); files is what the catalogue may serve locally (sha256 -> name).
func (m *Manager) Build(c store.Catalogue, prefix string) (items []any, report []AppReport, files map[string]string) {
	files = map[string]string{}
	cands := map[string][]Cand{}
	byApp := map[string][]Release{}
	for _, row := range m.St.CatReleases(c.ID) {
		var r Release
		if json.Unmarshal([]byte(row.Data), &r) == nil && m.blobOK(r.File.SHA256) {
			r.Published = row.Created
			byApp[row.AppID] = append(byApp[row.AppID], r)
		}
	}
	for id, rels := range byApp {
		sort.SliceStable(rels, func(i, j int) bool { return rels[i].Published < rels[j].Published })
		it := m.hostedItem(c, prefix, rels, map[string]string{}) // a throw-away files map: only the winner's files are served
		tag, pub := newest(it)
		cands[id] = append(cands[id], Cand{SourceID: "local", Source: "this catalogue", Priority: c.Priority, Version: tag, Published: pub, item: it, mirror: -1})
	}
	for _, src := range m.St.CatSources(c.ID) {
		if !src.Enabled {
			continue
		}
		mir := m.St.CatMirrors(src.ID)
		for _, row := range m.St.CatSourceItems(src.ID) {
			var it map[string]any
			if json.Unmarshal([]byte(row.Data), &it) != nil {
				continue
			}
			eff := src.Mirror
			if row.Mirror >= 0 {
				eff = row.Mirror == 1
			}
			cd := Cand{SourceID: src.ID, Source: src.Name, Priority: src.Priority, item: it, mirror: 0}
			cd.Version, cd.Published = newest(it)
			if eff {
				cd.mirror = 1
				// the mirrored files: rewrite each release whose cached copy is verified; the others keep the source's own address
				rs, _ := it["resolved"].([]any)
				all := len(rs) > 0
				for _, x := range rs {
					r, _ := x.(map[string]any)
					sha := str(r["sha256"])
					if mir[sha].State == "ok" && m.blobOK(sha) {
						continue
					}
					all = false
				}
				cd.Mirrored = all
			}
			cands[row.AppID] = append(cands[row.AppID], cd)
		}
	}
	ids := make([]string, 0, len(cands))
	for id := range cands {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		cs := cands[id]
		sort.SliceStable(cs, func(i, j int) bool {
			a, b := cs[i], cs[j]
			if a.Priority != b.Priority {
				return a.Priority > b.Priority
			}
			if a.Published != b.Published {
				return a.Published > b.Published
			}
			if (a.SourceID == "local") != (b.SourceID == "local") {
				return a.SourceID == "local"
			}
			return a.SourceID < b.SourceID
		})
		w := cs[0]
		rep := AppReport{ID: id, Name: str(w.item["name"]), Winner: w}
		for _, o := range cs[1:] {
			switch {
			case o.Priority < w.Priority:
				o.Reason = "lower priority"
			case o.Published < w.Published:
				o.Reason = "same priority, older release"
			default:
				o.Reason = "same priority, tie broken by source order"
			}
			rep.Others = append(rep.Others, o)
		}
		item := jsonCopy(w.item)
		if w.SourceID == "local" {
			// serve the hosted files of the winner
			var rels []Release
			for _, row := range m.St.CatReleases(c.ID) {
				var r Release
				if row.AppID == id && json.Unmarshal([]byte(row.Data), &r) == nil && m.blobOK(r.File.SHA256) {
					r.Published = row.Created
					rels = append(rels, r)
				}
			}
			sort.SliceStable(rels, func(i, j int) bool { return rels[i].Published < rels[j].Published })
			item = m.hostedItem(c, prefix, rels, files)
		} else if w.mirror == 1 {
			mir := m.St.CatMirrors(w.SourceID)
			rs, _ := item["resolved"].([]any)
			for _, x := range rs {
				r, _ := x.(map[string]any)
				sha := str(r["sha256"])
				if mir[sha].State == "ok" && m.blobOK(sha) {
					name := SafeName(str(r["asset"]))
					if name == "" {
						name = sha
					}
					files[sha] = name
					r["url"] = prefix + "/files/" + sha + "/" + name
				}
			}
		}
		items = append(items, item)
		report = append(report, rep)
	}
	if items == nil {
		items = []any{}
	}
	return
}

// Index is the document served at .../index.json.
func (m *Manager) Index(c store.Catalogue, prefix string) (map[string]any, map[string]string) {
	items, _, files := m.Build(c, prefix)
	return map[string]any{
		"format":    1,
		"generated": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"catalogue": map[string]any{"id": c.ID, "name": c.Name},
		"items":     items,
	}, files
}

// Prune removes the stored file with this SHA-256 when nothing refers to it any more.
func (m *Manager) Prune(sha string) {
	if IsSHA256(sha) && !m.St.CatBlobReferenced(sha) {
		os.Remove(m.blobPath(sha))
		m.vok.Delete(sha)
	}
}
