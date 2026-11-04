// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package catalogue

// The library (docs/catalogue.md, "The library"): one folder (PHECLOUD_CATALOGUE_LIBRARY) holds the package files for every catalogue. A scan walks it,
// reads each file's metadata (internal/pkginfo), and keeps one entry per file; a catalogue then publishes entries. Entries are found by path, changes
// by SHA-256, and a file that has gone is unpublished at once and flagged "missing".

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/pkginfo"
	"github.com/phorepoc/cloud-companion/internal/store"
)

// Meta is what the scanner read from a file.
type Meta struct {
	AppID     string   `json:"app_id"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Vendor    string   `json:"vendor,omitempty"`
	UID       string   `json:"uid,omitempty"`
	Platform  string   `json:"platform"`
	Package   string   `json:"package"`
	Arch      []string `json:"arch,omitempty"`
	Targets   []string `json:"targets,omitempty"`
	Languages []string `json:"languages,omitempty"`
	IconNote  string   `json:"icon_note,omitempty"`
	IconSVG   string   `json:"icon_svg,omitempty"` // the vector source of the icon, when the package's icon is a vector one
	Read      bool     `json:"read"`               // false: the file type carries no metadata we read (name and version come from the owner)
}

// Edits is what the owner set; an empty field means "as read from the file".
type Edits struct {
	AppID       string   `json:"app_id,omitempty"`
	Version     string   `json:"version,omitempty"`
	Name        string   `json:"name,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	License     string   `json:"license,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	Arch        []string `json:"arch,omitempty"`
	Boards      []string `json:"boards,omitempty"`
	Icon        *Blob    `json:"icon,omitempty"`
}

func (e Edits) empty() bool {
	b, _ := json.Marshal(e)
	return string(b) == "{}"
}

// ScanResult says what a scan found.
type ScanResult struct {
	Total      int      `json:"total"`
	Added      []string `json:"added"`
	Changed    []string `json:"changed"`
	Removed    []string `json:"removed"`  // gone from the folder; unpublished
	Restored   []string `json:"restored"` // were missing, are back (not republished)
	Renamed    []string `json:"renamed"`
	Unreadable []string `json:"unreadable"`
	Duration   float64  `json:"seconds"`
}

func nowUnix() int64 { return time.Now().Unix() }

// libPath maps an entry's relative path to the file system, refusing anything outside the library.
func (m *Manager) libPath(rel string) (string, error) {
	if m.Library == "" || rel == "" || strings.Contains(rel, "\\") || path.IsAbs(rel) {
		return "", errors.New("bad library path")
	}
	for _, p := range strings.Split(rel, "/") {
		if p == ".." || p == "" {
			return "", errors.New("bad library path")
		}
	}
	return filepath.Join(m.Library, filepath.FromSlash(rel)), nil
}

func isPackageFile(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") {
		return false
	}
	_, ok := packageTypes[PackageOf(name)]
	return ok
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var slugRe = regexp.MustCompile(`[^a-z0-9.]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-.")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// inspect reads the file into meta (and stores its icon).
func (m *Manager) inspect(f *store.CatFile, abs string) (iconNote string) {
	pkg := PackageOf(f.Path)
	meta := Meta{Platform: PlatformOf(pkg), Package: pkg}
	stem := strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path))
	meta.Name = strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(stem))
	meta.AppID = slug(stem)
	f.Error = ""
	f.Status = "ok"
	f.IconSHA = ""
	info, err := pkginfo.Inspect(abs)
	switch {
	case err == nil:
		meta.Read = true
		if info.AppID != "" {
			meta.AppID = info.AppID
		}
		if info.Name != "" {
			meta.Name = info.Name
		}
		meta.Version, meta.Vendor, meta.UID, meta.Arch, meta.Targets, meta.Languages, meta.IconNote = info.Version, info.Vendor, info.UID, info.Arch, info.Targets, info.Languages, info.IconNote
		if len(info.IconSVG) <= 128<<10 {
			meta.IconSVG = info.IconSVG
		}
		meta.Platform = info.Platform
		if len(info.Icon) > 0 {
			if sha, _, perr := m.PutBlob(strings.NewReader(string(info.Icon)), 4<<20, ""); perr == nil {
				f.IconSHA = sha
			}
		}
	case errors.Is(err, pkginfo.ErrUnsupported):
	default:
		f.Status, f.Error = "unreadable", err.Error()
	}
	b, _ := json.Marshal(meta)
	f.Meta = string(b)
	return
}

func (m *Manager) metaOf(f store.CatFile) (md Meta) {
	json.Unmarshal([]byte(f.Meta), &md)
	return
}

func (m *Manager) editsOf(f store.CatFile) (e Edits) {
	json.Unmarshal([]byte(f.Edits), &e)
	return
}

// Reading rule for edits after the file changed: the new file replaces what was read; the owner's edits stay, except that
//   - when the app identity differs (another package UID / name), all edits go (it is another app), and
//   - an edited version always goes (the new file has its own).
func applyChange(f *store.CatFile, old Meta, nu Meta) {
	var e Edits
	json.Unmarshal([]byte(f.Edits), &e)
	if old.Read && nu.Read && old.AppID != nu.AppID {
		e = Edits{}
	}
	e.Version = ""
	b, _ := json.Marshal(e)
	f.Edits = string(b)
	if e.empty() {
		f.Edits = ""
	}
}

// Scan walks the library folder and brings the entries up to date. Only one scan runs at a time.
func (m *Manager) Scan(ctx context.Context) (ScanResult, error) {
	var res ScanResult
	if m.Library == "" {
		return res, errors.New("no library folder is configured")
	}
	if !m.scanMu.TryLock() {
		return res, errors.New("a scan is already running")
	}
	defer m.scanMu.Unlock()
	start := time.Now()
	if err := os.MkdirAll(m.Library, 0o755); err != nil {
		return res, err
	}
	existing := map[string]store.CatFile{}
	for _, f := range m.St.CatFiles() {
		if f.Kind == "library" {
			existing[f.Path] = f
		}
	}
	seen := map[string]bool{}
	var fresh []string // paths of new files, for rename detection
	err := filepath.WalkDir(m.Library, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil // an unreadable directory is skipped, the files in it count as missing
		}
		if d.IsDir() {
			if p != m.Library && (strings.HasPrefix(d.Name(), ".") || d.Name() == "tmp") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !isPackageFile(d.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(m.Library, p)
		rel = filepath.ToSlash(rel)
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		seen[rel] = true
		if f, ok := existing[rel]; ok {
			m.rescanOne(&res, f, p, fi)
		} else {
			fresh = append(fresh, rel)
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	// files that vanished
	var gone []store.CatFile
	for rel, f := range existing {
		if !seen[rel] && f.Status != "missing" {
			gone = append(gone, f)
		}
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].Path < gone[j].Path })
	sort.Strings(fresh)
	goneBySHA := map[string]*store.CatFile{}
	for i := range gone {
		goneBySHA[gone[i].SHA256] = &gone[i]
	}
	renamed := map[string]bool{}
	for _, rel := range fresh {
		abs := filepath.Join(m.Library, filepath.FromSlash(rel))
		sha, size, err := hashFile(abs)
		if err != nil {
			continue
		}
		fi, _ := os.Stat(abs)
		if g, ok := goneBySHA[sha]; ok && !renamed[g.ID] { // same content under a new name: the same entry moved
			g.Path = rel
			g.MTime = fi.ModTime().UnixNano()
			g.Scanned = nowUnix()
			m.St.CatFilePut(*g)
			renamed[g.ID] = true
			res.Renamed = append(res.Renamed, existingName(*g))
			continue
		}
		f := store.CatFile{ID: newID(), Path: rel, Kind: "library", SHA256: sha, Size: size, MTime: fi.ModTime().UnixNano(), Added: nowUnix(), Scanned: nowUnix(), Changed: nowUnix()}
		m.inspect(&f, abs)
		m.St.CatFilePut(f)
		res.Added = append(res.Added, rel)
		if f.Status == "unreadable" {
			res.Unreadable = append(res.Unreadable, rel)
		}
	}
	for _, g := range gone {
		if renamed[g.ID] {
			continue
		}
		g.Status = "missing"
		g.Error = "the file has gone from the library folder (noticed " + time.Now().UTC().Format("2006-01-02 15:04 UTC") + ")"
		g.Scanned = nowUnix()
		m.St.CatUnpublishEverywhere(g.ID)
		m.St.CatFilePut(g)
		m.vok.Delete(g.SHA256)
		res.Removed = append(res.Removed, g.Path)
	}
	res.Total = len(m.St.CatFiles())
	res.Duration = time.Since(start).Seconds()
	sort.Strings(res.Added)
	return res, nil
}

func existingName(f store.CatFile) string { return f.Path }

// rescanOne checks a known file: unchanged (same size and modification time, and it was fine), restored, or changed (different SHA-256).
func (m *Manager) rescanOne(res *ScanResult, f store.CatFile, abs string, fi fs.FileInfo) {
	f.Scanned = nowUnix()
	wasMissing := f.Status == "missing"
	if !wasMissing && fi.Size() == f.Size && fi.ModTime().UnixNano() == f.MTime {
		m.St.CatFilePut(f)
		return
	}
	sha, size, err := hashFile(abs)
	if err != nil {
		return
	}
	f.Size, f.MTime = size, fi.ModTime().UnixNano()
	if sha == f.SHA256 {
		if wasMissing {
			m.inspect(&f, abs) // content as before
			res.Restored = append(res.Restored, f.Path)
		}
		m.St.CatFilePut(f)
		return
	}
	old := m.metaOf(f)
	oldSHA := f.SHA256
	f.SHA256, f.Changed = sha, nowUnix()
	m.inspect(&f, abs)
	applyChange(&f, old, m.metaOf(f))
	if f.Status == "unreadable" {
		res.Unreadable = append(res.Unreadable, f.Path)
		m.St.CatUnpublishEverywhere(f.ID)
	}
	m.St.CatFilePut(f)
	m.vok.Delete(oldSHA)
	if wasMissing {
		res.Restored = append(res.Restored, f.Path)
	}
	res.Changed = append(res.Changed, f.Path)
}

// ScanLoop scans once now and then every interval (0 = only the first time).
func (m *Manager) ScanLoop(ctx context.Context, interval time.Duration, log func(string, ...any)) {
	run := func() {
		if r, err := m.ScanNow(ctx); err != nil {
			log("catalogue library scan failed", "err", err)
		} else if len(r.Added)+len(r.Changed)+len(r.Removed) > 0 {
			log("catalogue library scanned", "added", len(r.Added), "changed", len(r.Changed), "removed", len(r.Removed))
		}
	}
	run()
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// AddUpload stores an uploaded file in the library (uploads/), scans it and returns its entry. An identical file already there is reused.
func (m *Manager) AddUpload(name string, r io.Reader, max int64) (store.CatFile, error) {
	var zero store.CatFile
	name = SafeName(name)
	if name == "" || !isPackageFile(name) {
		return zero, errors.New("that kind of file is not a package this catalogue serves (.sis, .sisx, .apk, .exe, .deb, .zip ...)")
	}
	if m.Library == "" {
		return zero, errors.New("no library folder is configured")
	}
	dir := filepath.Join(m.Library, "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return zero, err
	}
	tmp, err := os.CreateTemp(dir, ".up-*")
	if err != nil {
		return zero, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, max+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return zero, err
	}
	if n > max {
		return zero, fmt.Errorf("the file is larger than the limit (%d bytes)", max)
	}
	sha := hex.EncodeToString(h.Sum(nil))
	target := filepath.Join(dir, name)
	if s, _, err := hashFile(target); err == nil && s != sha { // the name is taken by another file
		ext := PackageOf(name)
		base := strings.TrimSuffix(name, "."+ext)
		if ext == "file" {
			base = name
		}
		name = fmt.Sprintf("%s-%s.%s", base, sha[:8], ext)
		target = filepath.Join(dir, name)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return zero, err
	}
	if _, err := m.ScanNow(context.Background()); err != nil {
		return zero, err
	}
	rel := "uploads/" + name
	for _, f := range m.St.CatFiles() {
		if f.Kind == "library" && f.Path == rel {
			return f, nil
		}
	}
	return zero, errors.New("the file was stored but not recognised as a package")
}

// ---- entries as the owner and the index see them

// Release builds the release an entry stands for: what the scanner read (or the stored release of an old upload), with the owner's edits on top.
func (m *Manager) Release(f store.CatFile) (Release, error) {
	var r Release
	if f.Kind == "stored" {
		json.Unmarshal([]byte(f.Legacy), &r)
	} else {
		md := m.metaOf(f)
		r = Release{AppID: md.AppID, Version: md.Version, Name: md.Name, Platform: md.Platform, Package: md.Package, Arch: md.Arch}
		r.File = Blob{Name: path.Base(f.Path), SHA256: f.SHA256, Size: f.Size}
		if f.IconSHA != "" {
			r.Icon = &Blob{Name: "icon.png", SHA256: f.IconSHA}
		}
		if md.Vendor != "" {
			r.Authors = []string{md.Vendor}
		}
	}
	e := m.editsOf(f)
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&r.AppID, e.AppID)
	set(&r.Version, e.Version)
	set(&r.Name, e.Name)
	set(&r.Summary, e.Summary)
	set(&r.Description, e.Description)
	set(&r.Category, e.Category)
	set(&r.License, e.License)
	set(&r.Homepage, e.Homepage)
	set(&r.Platform, e.Platform)
	if len(e.Authors) > 0 {
		r.Authors = e.Authors
	}
	if len(e.Arch) > 0 {
		r.Arch = e.Arch
	}
	if len(e.Boards) > 0 {
		r.Boards = e.Boards
	}
	if e.Icon != nil {
		r.Icon = e.Icon
	}
	if f.Kind == "library" && r.Version == "" {
		return r, errors.New("the file carries no version: set one in the details")
	}
	return r, r.Normalize()
}

// hosted returns the published releases of a catalogue, by app id, oldest first. Missing files and clashes (the same app id and version published
// twice) are left out.
func (m *Manager) hosted(catID string) map[string][]Release {
	pub := m.St.CatPublished(catID)
	files := m.St.CatFiles()
	sort.SliceStable(files, func(i, j int) bool { return pub[files[i].ID] < pub[files[j].ID] })
	out := map[string][]Release{}
	have := map[string]bool{}
	for _, f := range files {
		created, ok := pub[f.ID]
		if !ok || f.Status == "missing" {
			continue
		}
		r, err := m.Release(f)
		if err != nil || have[r.AppID+"|"+r.Version] || !m.blobOK(r.File.SHA256) {
			continue
		}
		have[r.AppID+"|"+r.Version] = true
		r.Published = created
		out[r.AppID] = append(out[r.AppID], r)
	}
	for id := range out {
		rels := out[id]
		sort.SliceStable(rels, func(i, j int) bool { return rels[i].Published < rels[j].Published })
	}
	return out
}

// Publish publishes or unpublishes entries in a catalogue and says, per entry, why it could not.
func (m *Manager) Publish(catID string, ids []string, on bool) (changed int, problems map[string]string) {
	problems = map[string]string{}
	for _, id := range ids {
		f, ok := m.St.CatFile(id)
		if !ok {
			problems[id] = "unknown entry"
			continue
		}
		if on {
			if f.Status == "missing" {
				problems[id] = "the file is missing from the library folder"
				continue
			}
			r, err := m.Release(f)
			if err != nil {
				problems[id] = err.Error()
				continue
			}
			clash := ""
			for pid := range m.St.CatPublished(catID) {
				if pid == id {
					continue
				}
				if o, ok := m.St.CatFile(pid); ok {
					if or, err := m.Release(o); err == nil && or.AppID == r.AppID && or.Version == r.Version {
						clash = o.Path
						if clash == "" {
							clash = "an earlier upload"
						}
					}
				}
			}
			if clash != "" {
				problems[id] = fmt.Sprintf("version %s of %s is already published from %s: unpublish that first", r.Version, r.AppID, clash)
				continue
			}
		}
		if ch, err := m.St.CatPublishSet(catID, id, on, 0); err != nil {
			problems[id] = "could not save"
		} else if ch {
			changed++
		}
	}
	return
}

// SaveEdits replaces the owner's edits of an entry.
func (m *Manager) SaveEdits(id string, e Edits) error {
	f, ok := m.St.CatFile(id)
	if !ok {
		return errors.New("unknown entry")
	}
	if e.empty() {
		f.Edits = ""
	} else {
		b, _ := json.Marshal(e)
		f.Edits = string(b)
	}
	// the result must be a valid release (when the file has the data for one)
	if _, err := m.Release(f); err != nil && f.Kind == "stored" {
		return err
	} else if err != nil && (e.Version != "" || e.AppID != "" || e.Name != "") {
		return err
	}
	return m.St.CatFilePut(f)
}

// DeleteEntry removes an entry; with deleteFile it also deletes the file from the library folder.
func (m *Manager) DeleteEntry(id string, deleteFile bool) error {
	f, ok := m.St.CatFile(id)
	if !ok {
		return errors.New("unknown entry")
	}
	if deleteFile && f.Kind == "library" && f.Status != "missing" {
		p, err := m.libPath(f.Path)
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := m.St.CatFileDelete(id); err != nil {
		return err
	}
	e := m.editsOf(f)
	for _, sha := range []string{f.IconSHA, f.SHA256} {
		if f.Kind == "stored" || sha == f.IconSHA {
			m.Prune(sha)
		}
	}
	if e.Icon != nil {
		m.Prune(e.Icon.SHA256)
	}
	return nil
}

// Hosted is the published releases of a catalogue by app id (what the index offers from this server's own files).
func (m *Manager) Hosted(catID string) map[string][]Release { return m.hosted(catID) }

// EditsOf is the owner's edits of an entry.
func (m *Manager) EditsOf(f store.CatFile) Edits { return m.editsOf(f) }

// ScanNow runs a scan and remembers its result for the owner view.
func (m *Manager) ScanNow(ctx context.Context) (ScanResult, error) {
	res, err := m.Scan(ctx)
	if err == nil {
		m.lastMu.Lock()
		m.last, m.lastAt = res, time.Now().Unix()
		m.lastMu.Unlock()
	}
	return res, err
}

// LastScan is the result and time of the most recent scan.
func (m *Manager) LastScan() (ScanResult, int64) {
	m.lastMu.Lock()
	defer m.lastMu.Unlock()
	return m.last, m.lastAt
}
