// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package files is the owner's own storage behind the Files section and the Music library: a folder on the server
// (PHECLOUD_FILES_DIR, by default <data>/files), shared by every paired device. Paths are always slash paths relative to the
// root; nothing outside the root can be reached (no "..", no symbolic links followed out of it).
package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one file or folder.
type Entry struct {
	Name string    `json:"name"`
	Path string    `json:"path"`
	Dir  bool      `json:"dir"`
	Size int64     `json:"size"`
	Mod  time.Time `json:"mod"`
	Type string    `json:"type,omitempty"` // the media type, from the name
}

// Store is the root folder.
type Store struct {
	Dir     string
	MaxFile int64 // bytes; 0 = 4 GiB
	mu      sync.Mutex
	tickets map[string]ticket
}

type ticket struct {
	path string
	exp  time.Time
}

// New returns a Store at dir (made when first written).
func New(dir string) *Store { return &Store{Dir: dir, tickets: map[string]ticket{}} }

// ErrBadPath is a path that leaves the root or names something hidden by the store.
var ErrBadPath = errors.New("that is not a path in your files")

// Clean turns p into a clean slash path inside the root ("" is the root itself).
func Clean(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", ErrBadPath
		}
	}
	c := strings.TrimPrefix(path.Clean("/"+p), "/")
	if strings.ContainsRune(c, 0) || strings.HasPrefix(path.Base(c), ".phe-") {
		return "", ErrBadPath
	}
	return c, nil
}

func (s *Store) abs(p string) (string, string, error) {
	c, err := Clean(p)
	if err != nil {
		return "", "", err
	}
	a := filepath.Join(s.Dir, filepath.FromSlash(c))
	// a symbolic link must not lead out of the root
	if real, err := filepath.EvalSymlinks(a); err == nil {
		root, _ := filepath.EvalSymlinks(s.Dir)
		if root == "" {
			root = s.Dir
		}
		if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
			return "", "", ErrBadPath
		}
	}
	return c, a, nil
}

func typeOf(name string) string {
	t := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if t == "" {
		switch strings.ToLower(path.Ext(name)) {
		case ".flac":
			t = "audio/flac"
		case ".opus":
			t = "audio/ogg"
		case ".m4a":
			t = "audio/mp4"
		case ".mkv":
			t = "video/x-matroska"
		}
	}
	if i := strings.IndexByte(t, ';'); i > 0 {
		t = t[:i]
	}
	return t
}

func entry(c string, fi fs.FileInfo) Entry {
	e := Entry{Name: fi.Name(), Path: c, Dir: fi.IsDir(), Mod: fi.ModTime().UTC()}
	if !e.Dir {
		e.Size = fi.Size()
		e.Type = typeOf(fi.Name())
	}
	return e
}

// List returns a folder's entries, folders first, then by name.
func (s *Store) List(p string) ([]Entry, error) {
	c, a, err := s.abs(p)
	if err != nil {
		return nil, err
	}
	if c == "" {
		os.MkdirAll(a, 0o750)
	}
	des, err := os.ReadDir(a)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errors.New("that folder does not exist")
		}
		return nil, err
	}
	out := []Entry{}
	for _, d := range des {
		if strings.HasPrefix(d.Name(), ".phe-") {
			continue
		}
		fi, err := d.Info()
		if err != nil {
			continue
		}
		out = append(out, entry(path.Join(c, d.Name()), fi))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Stat describes one path.
func (s *Store) Stat(p string) (Entry, error) {
	c, a, err := s.abs(p)
	if err != nil {
		return Entry{}, err
	}
	fi, err := os.Stat(a)
	if err != nil {
		return Entry{}, errors.New("that file does not exist")
	}
	return entry(c, fi), nil
}

// Open opens a file for reading (for http.ServeContent).
func (s *Store) Open(p string) (*os.File, Entry, error) {
	e, err := s.Stat(p)
	if err != nil {
		return nil, e, err
	}
	if e.Dir {
		return nil, e, errors.New("that is a folder")
	}
	_, a, _ := s.abs(p)
	f, err := os.Open(a)
	return f, e, err
}

// Mkdir makes a folder (and its parents).
func (s *Store) Mkdir(p string) error {
	c, a, err := s.abs(p)
	if err != nil || c == "" {
		return ErrBadPath
	}
	return os.MkdirAll(a, 0o750)
}

func validName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, "/\\\x00") && len(n) <= 255 && !strings.HasPrefix(n, ".phe-")
}

// Save writes r to dir/name (replacing a file of that name), through a temporary file so a cut-off upload leaves nothing behind.
func (s *Store) Save(dir, name string, r io.Reader) (Entry, error) {
	if !validName(name) {
		return Entry{}, errors.New("that is not a valid file name")
	}
	c, a, err := s.abs(path.Join(dir, name))
	if err != nil {
		return Entry{}, err
	}
	if fi, err := os.Stat(a); err == nil && fi.IsDir() {
		return Entry{}, errors.New("a folder has that name")
	}
	if err := os.MkdirAll(filepath.Dir(a), 0o750); err != nil {
		return Entry{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(a), ".phe-up-*")
	if err != nil {
		return Entry{}, err
	}
	defer os.Remove(tmp.Name())
	max := s.MaxFile
	if max <= 0 {
		max = 4 << 30
	}
	n, err := io.Copy(tmp, io.LimitReader(r, max+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Entry{}, err
	}
	if n > max {
		return Entry{}, errors.New("that file is too large")
	}
	if err := os.Rename(tmp.Name(), a); err != nil {
		return Entry{}, err
	}
	fi, _ := os.Stat(a)
	return entry(c, fi), nil
}

// Move renames or moves a file or folder; the target must not exist.
func (s *Store) Move(from, to string) error {
	fc, fa, err := s.abs(from)
	if err != nil || fc == "" {
		return ErrBadPath
	}
	tc, ta, err := s.abs(to)
	if err != nil || tc == "" || !validName(path.Base(tc)) {
		return ErrBadPath
	}
	if tc == fc || strings.HasPrefix(tc, fc+"/") {
		return errors.New("a folder cannot go inside itself")
	}
	if _, err := os.Lstat(ta); err == nil {
		return errors.New("something with that name is already there")
	}
	if _, err := os.Lstat(fa); err != nil {
		return errors.New("that file does not exist")
	}
	os.MkdirAll(filepath.Dir(ta), 0o750)
	return os.Rename(fa, ta)
}

// Delete removes a file or a whole folder (never the root).
func (s *Store) Delete(p string) error {
	c, a, err := s.abs(p)
	if err != nil || c == "" {
		return ErrBadPath
	}
	if _, err := os.Lstat(a); err != nil {
		return errors.New("that file does not exist")
	}
	return os.RemoveAll(a)
}

func token() string {
	b := make([]byte, 18)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Ticket returns a short random token that lets the browser's own media players (which cannot sign requests) read one file for
// the next 12 hours.
func (s *Store) Ticket(p string) (string, error) {
	e, err := s.Stat(p)
	if err != nil {
		return "", err
	}
	if e.Dir {
		return "", errors.New("that is a folder")
	}
	t := token()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.tickets {
		if now.After(v.exp) {
			delete(s.tickets, k)
		}
	}
	s.tickets[t] = ticket{e.Path, now.Add(12 * time.Hour)}
	return t, nil
}

// Redeem returns the path a ticket is for.
func (s *Store) Redeem(t string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.tickets[t]
	if !ok || time.Now().After(v.exp) {
		return "", false
	}
	return v.path, true
}
