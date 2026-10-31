// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package files

import (
	"encoding/binary"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Track is one song of the music library: the tags of the file where it has them (ID3v2, FLAC / Ogg comments), otherwise
// what its place says (Artist/Album/NN Title.ext).
type Track struct {
	Path   string `json:"path"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	N      int    `json:"n,omitempty"`
	Cover  string `json:"cover,omitempty"` // cover.jpg / folder.jpg next to it
	Size   int64  `json:"size"`
}

var audioExt = map[string]bool{".mp3": true, ".m4a": true, ".aac": true, ".flac": true, ".ogg": true, ".opus": true, ".oga": true, ".wav": true}

// IsAudio tells whether a name is a song the library reads.
func IsAudio(name string) bool { return audioExt[strings.ToLower(path.Ext(name))] }

// Music walks every folder and returns the songs, by artist, album, number and title (at most 20 000).
func (s *Store) Music() ([]Track, error) {
	root := s.Dir
	os.MkdirAll(root, 0o750)
	covers := map[string]string{}
	var out []Track
	err := filepath.WalkDir(root, func(a string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && a != root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, a)
		rel = filepath.ToSlash(rel)
		if d.IsDir() || len(out) >= 20000 {
			return nil
		}
		low := strings.ToLower(d.Name())
		if low == "cover.jpg" || low == "folder.jpg" || low == "cover.png" || low == "front.jpg" {
			if _, ok := covers[path.Dir(rel)]; !ok || low == "cover.jpg" {
				covers[path.Dir(rel)] = rel
			}
			return nil
		}
		if !IsAudio(d.Name()) {
			return nil
		}
		t := guess(rel)
		if fi, err := d.Info(); err == nil {
			t.Size = fi.Size()
		}
		if f, err := os.Open(a); err == nil {
			tags(f, strings.ToLower(path.Ext(rel)), &t)
			f.Close()
		}
		out = append(out, t)
		return nil
	})
	for i := range out {
		out[i].Cover = covers[path.Dir(out[i].Path)]
	}
	low := strings.ToLower
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if low(a.Artist) != low(b.Artist) {
			return low(a.Artist) < low(b.Artist)
		}
		if low(a.Album) != low(b.Album) {
			return low(a.Album) < low(b.Album)
		}
		if a.N != b.N {
			return a.N < b.N
		}
		return low(a.Title) < low(b.Title)
	})
	if out == nil {
		out = []Track{}
	}
	return out, err
}

// guess reads Artist/Album/NN - Title.ext (a leading "Music/" folder is skipped).
func guess(rel string) Track {
	t := Track{Path: rel, Artist: "Unknown artist", Album: "Unknown album"}
	parts := strings.Split(rel, "/")
	if len(parts) > 1 && strings.EqualFold(parts[0], "music") {
		parts = parts[1:]
	}
	name := strings.TrimSuffix(parts[len(parts)-1], path.Ext(rel))
	if len(parts) >= 3 {
		t.Artist, t.Album = parts[len(parts)-3], parts[len(parts)-2]
	} else if len(parts) == 2 {
		t.Album = parts[0]
	}
	// "03 - Title", "03. Title", "03 Title"
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i > 0 && i <= 3 && i < len(name) {
		if n, err := strconv.Atoi(name[:i]); err == nil {
			rest := strings.TrimLeft(name[i:], " .-_")
			if rest != "" {
				t.N, name = n, rest
			}
		}
	}
	t.Title = name
	return t
}

func tags(r io.ReadSeeker, ext string, t *Track) {
	switch ext {
	case ".mp3", ".aac":
		id3(r, t)
	case ".flac":
		flac(r, t)
	case ".ogg", ".opus", ".oga":
		oggComments(r, t)
	}
}

func set(t *Track, key, v string) {
	v = strings.TrimSpace(strings.TrimRight(v, "\x00"))
	if v == "" {
		return
	}
	switch key {
	case "title":
		t.Title = v
	case "artist":
		t.Artist = v
	case "album":
		t.Album = v
	case "n":
		if i := strings.IndexByte(v, '/'); i > 0 {
			v = v[:i]
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			t.N = n
		}
	}
}

func id3text(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	enc, b := b[0], b[1:]
	switch enc {
	case 1, 2: // UTF-16 (with a BOM) or UTF-16BE
		be := enc == 2
		if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
			be, b = false, b[2:]
		} else if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
			be, b = true, b[2:]
		}
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			if be {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
			}
		}
		return string(utf16.Decode(u))
	case 0: // Latin-1
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	}
	return string(b)
}

func id3(r io.Reader, t *Track) {
	h := make([]byte, 10)
	if _, err := io.ReadFull(r, h); err != nil || string(h[:3]) != "ID3" {
		return
	}
	ver := h[3]
	size := int(h[6]&0x7F)<<21 | int(h[7]&0x7F)<<14 | int(h[8]&0x7F)<<7 | int(h[9]&0x7F)
	if size > 4<<20 {
		size = 4 << 20
	}
	b := make([]byte, size)
	n, _ := io.ReadFull(r, b)
	b = b[:n]
	names := map[string]string{"TIT2": "title", "TPE1": "artist", "TALB": "album", "TRCK": "n", "TT2": "title", "TP1": "artist", "TAL": "album", "TRK": "n"}
	for p := 0; ; {
		var id string
		var fl int
		if ver == 2 {
			if p+6 > len(b) {
				return
			}
			id, fl = string(b[p:p+3]), int(b[p+3])<<16|int(b[p+4])<<8|int(b[p+5])
			p += 6
		} else {
			if p+10 > len(b) {
				return
			}
			id = string(b[p : p+4])
			if ver == 4 {
				fl = int(b[p+4]&0x7F)<<21 | int(b[p+5]&0x7F)<<14 | int(b[p+6]&0x7F)<<7 | int(b[p+7]&0x7F)
			} else {
				fl = int(binary.BigEndian.Uint32(b[p+4 : p+8]))
			}
			p += 10
		}
		if id[0] == 0 || fl <= 0 || p+fl > len(b) {
			return
		}
		if k, ok := names[id]; ok {
			set(t, k, id3text(b[p:p+fl]))
		}
		p += fl
	}
}

func comment(t *Track, c string) {
	k, v, ok := strings.Cut(c, "=")
	if !ok {
		return
	}
	switch strings.ToUpper(k) {
	case "TITLE":
		set(t, "title", v)
	case "ARTIST":
		set(t, "artist", v)
	case "ALBUM":
		set(t, "album", v)
	case "TRACKNUMBER":
		set(t, "n", v)
	}
}

// vorbis reads a Vorbis comment block (vendor, then count × length-prefixed "KEY=value").
func vorbis(b []byte, t *Track) {
	rd := func() (string, bool) {
		if len(b) < 4 {
			return "", false
		}
		n := int(binary.LittleEndian.Uint32(b))
		if n < 0 || 4+n > len(b) {
			return "", false
		}
		s := string(b[4 : 4+n])
		b = b[4+n:]
		return s, true
	}
	if _, ok := rd(); !ok || len(b) < 4 {
		return
	}
	cnt := int(binary.LittleEndian.Uint32(b))
	b = b[4:]
	for i := 0; i < cnt && i < 500; i++ {
		c, ok := rd()
		if !ok {
			return
		}
		comment(t, c)
	}
}

func flac(r io.Reader, t *Track) {
	h := make([]byte, 4)
	if _, err := io.ReadFull(r, h); err != nil || string(h) != "fLaC" {
		return
	}
	for i := 0; i < 64; i++ {
		if _, err := io.ReadFull(r, h); err != nil {
			return
		}
		last, typ, n := h[0]&0x80 != 0, h[0]&0x7F, int(h[1])<<16|int(h[2])<<8|int(h[3])
		if typ == 4 && n < 1<<20 {
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err == nil {
				vorbis(b, t)
			}
			return
		}
		if _, err := io.CopyN(io.Discard, r, int64(n)); err != nil || last {
			return
		}
	}
}

// oggComments finds the comment header in the first 64 KiB (Vorbis "\x03vorbis", Opus "OpusTags").
func oggComments(r io.Reader, t *Track) {
	b := make([]byte, 64<<10)
	n, _ := io.ReadFull(r, b)
	b = b[:n]
	for _, mark := range []string{"\x03vorbis", "OpusTags"} {
		if i := strings.Index(string(b), mark); i >= 0 {
			vorbis(b[i+len(mark):], t)
			return
		}
	}
}
