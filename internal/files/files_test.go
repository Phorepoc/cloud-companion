// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package files

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore(t *testing.T) {
	s := New(t.TempDir())
	for _, bad := range []string{"../x", "a/../../x", "a/..", ".phe-up-1"} {
		if _, err := s.List(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if err := s.Mkdir("Docs/Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("Docs", "a.txt", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("Docs", "../evil", strings.NewReader("x")); err == nil {
		t.Error("a name with a slash is refused")
	}
	l, err := s.List("/Docs/")
	if err != nil || len(l) != 2 || !l[0].Dir || l[1].Name != "a.txt" || l[1].Size != 5 || l[1].Type != "text/plain" || l[1].Path != "Docs/a.txt" {
		t.Fatalf("list: %+v %v", l, err)
	}
	s.MaxFile = 3
	if _, err := s.Save("", "big.bin", strings.NewReader("12345")); err == nil {
		t.Error("too large")
	}
	if l, _ := s.List(""); len(l) != 1 {
		t.Errorf("a refused upload leaves nothing behind: %+v", l)
	}
	if err := s.Move("Docs", "Docs/Work/x"); err == nil {
		t.Error("a folder into itself")
	}
	if err := s.Move("Docs/a.txt", "Docs/Work/b.txt"); err != nil {
		t.Fatal(err)
	}
	tk, err := s.Ticket("Docs/Work/b.txt")
	if p, ok := s.Redeem(tk); err != nil || !ok || p != "Docs/Work/b.txt" {
		t.Errorf("ticket: %v %q", err, p)
	}
	if _, ok := s.Redeem("nope"); ok {
		t.Error("unknown ticket")
	}
	if err := s.Delete(""); err == nil {
		t.Error("the root is never deleted")
	}
	if err := s.Delete("Docs"); err != nil {
		t.Fatal(err)
	}
	// a link leading out of the root
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "secret"), []byte("x"), 0o600)
	os.Symlink(out, filepath.Join(s.Dir, "esc"))
	if _, err := s.List("esc"); err == nil {
		t.Error("a symbolic link out of the root")
	}
}

func TestShares(t *testing.T) {
	var saved []Share
	sh := NewShares(nil, func(l []Share) { saved = l })
	a, _ := sh.Add("x.pdf", 0)
	sh.Add("y.pdf", 3)
	if p, ok := sh.Get(a.Token); !ok || p != "x.pdf" || len(saved) != 2 {
		t.Fatal("add / get")
	}
	sh.Remove(a.Token)
	if _, ok := sh.Get(a.Token); ok || len(saved) != 1 {
		t.Error("remove")
	}
	if _, err := sh.Add("z", -1); err == nil {
		t.Error("days")
	}
}

func id3frame(id, text string) []byte {
	b := append([]byte{3}, text...) // UTF-8
	h := make([]byte, 10)
	copy(h, id)
	binary.BigEndian.PutUint32(h[4:], uint32(len(b)))
	return append(h, b...)
}

func TestMusic(t *testing.T) {
	s := New(t.TempDir())
	s.Mkdir("Music/Belle Band/First Light")
	s.Save("Music/Belle Band/First Light", "02 - Second.mp3", strings.NewReader("no tags"))
	s.Save("Music/Belle Band/First Light", "cover.jpg", strings.NewReader("jpg"))
	// an ID3v2.3 tagged file in a loose folder
	var fr []byte
	fr = append(fr, id3frame("TIT2", "Tagged song")...)
	fr = append(fr, id3frame("TPE1", "Tag Artist")...)
	fr = append(fr, id3frame("TALB", "Tag Album")...)
	fr = append(fr, id3frame("TRCK", "7/12")...)
	n := len(fr)
	h := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
	s.Save("Inbox", "x.mp3", strings.NewReader(string(append(h, fr...))))
	// FLAC with a Vorbis comment block
	vc := []byte{}
	le := func(v int) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, uint32(v)); return b }
	vc = append(vc, le(3)...)
	vc = append(vc, "ven"...)
	vc = append(vc, le(2)...)
	for _, c := range []string{"TITLE=Flac song", "ARTIST=Aardvark"} {
		vc = append(vc, le(len(c))...)
		vc = append(vc, c...)
	}
	fl := append([]byte("fLaC"), 0x84, byte(len(vc)>>16), byte(len(vc)>>8), byte(len(vc)))
	s.Save("Loose", "y.flac", strings.NewReader(string(append(fl, vc...))))
	s.Save("Docs", "notes.txt", strings.NewReader("not music"))
	l, err := s.Music()
	if err != nil || len(l) != 3 {
		t.Fatalf("music: %+v %v", l, err)
	}
	if l[0].Title != "Flac song" || l[0].Artist != "Aardvark" {
		t.Errorf("flac: %+v", l[0])
	}
	if l[1].Title != "Second" || l[1].Artist != "Belle Band" || l[1].Album != "First Light" || l[1].N != 2 || l[1].Cover != "Music/Belle Band/First Light/cover.jpg" {
		t.Errorf("from the path: %+v", l[1])
	}
	if l[2].Title != "Tagged song" || l[2].Artist != "Tag Artist" || l[2].Album != "Tag Album" || l[2].N != 7 {
		t.Errorf("id3: %+v", l[2])
	}
}
