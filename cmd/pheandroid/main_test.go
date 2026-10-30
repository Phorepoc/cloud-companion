// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func TestTicketAndMessages(t *testing.T) {
	secret = "testsecret"
	// the same vector as internal/webui/android_test.go (the minting side), expiry in 2100
	if !ticketOK("4102444800.6f7556a99bf59d268dc8fd61eafb859495275d0209bd9228857115aa9d5a4027") {
		t.Fatal("a ticket minted by phecloud must verify")
	}
	for _, bad := range []string{"", "4102444800", "4102444800.00", "1.6f7556a99bf59d268dc8fd61eafb859495275d0209bd9228857115aa9d5a4027", "4102444801.6f7556a99bf59d268dc8fd61eafb859495275d0209bd9228857115aa9d5a4027"} {
		if ticketOK(bad) {
			t.Fatalf("ticket %q must not verify", bad)
		}
	}
	secret = ""
	if ticketOK("4102444800.6f7556a99bf59d268dc8fd61eafb859495275d0209bd9228857115aa9d5a4027") {
		t.Fatal("without a secret nothing verifies")
	}
	// scrcpy control messages, byte layouts from scrcpy's own serialization tests
	m := touchMsg(0, 100.0/1079, 200.0/1919, 1080, 1920)
	if len(m) != 32 || m[0] != 2 || m[1] != 0 || binary.BigEndian.Uint32(m[10:]) != 100 || binary.BigEndian.Uint32(m[14:]) != 200 ||
		binary.BigEndian.Uint16(m[18:]) != 1080 || binary.BigEndian.Uint16(m[20:]) != 1920 || binary.BigEndian.Uint16(m[22:]) != 0xffff || binary.BigEndian.Uint32(m[24:]) != 1 {
		t.Fatalf("touch down: %x", m)
	}
	if u := touchMsg(1, 0, 0, 10, 10); binary.BigEndian.Uint16(u[22:]) != 0 {
		t.Fatalf("touch up has no pressure: %x", u)
	}
	k := keyMsg(1, 66, 0)
	if len(k) != 14 || k[0] != 0 || k[1] != 1 || binary.BigEndian.Uint32(k[2:]) != 66 {
		t.Fatalf("key: %x", k)
	}
	if c := codecString([]byte{0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x29, 0x8d}); c != "avc1.42c029" {
		t.Fatalf("codec string %q", c)
	}
}

func TestScrollClipboardMessages(t *testing.T) {
	// scrcpy's own test vector: position (260,1026) on 1080x1920, hscroll 16, vscroll -16, buttons 1 (here buttons stay 0)
	m := scrollMsg(260.0/1079, 1026.0/1919, 16, -16, 1080, 1920)
	want := []byte{3, 0, 0, 1, 4, 0, 0, 4, 2, 4, 0x38, 7, 0x80, 0x7f, 0xff, 0x80, 0, 0, 0, 0, 0}
	if len(m) != len(want) {
		t.Fatalf("scroll length %d", len(m))
	}
	for i := range want {
		if m[i] != want[i] {
			t.Fatalf("scroll byte %d: got %x want %x\n%x", i, m[i], want[i], m)
		}
	}
	c := clipSetMsg("hello, world!", true)
	if len(c) != 27 || c[0] != 9 || c[9] != 1 || binary.BigEndian.Uint32(c[10:]) != 13 || string(c[14:]) != "hello, world!" {
		t.Fatalf("set clipboard: %x", c)
	}
	if fixed16(0) != 0 || fixed16(-1) >= 0 || fixed16(100) != 0x7fff || fixed16(-100) != -0x8000 {
		t.Fatal("fixed16")
	}
}

// The web app imports web/src/android/stream.js; the bridge embeds and serves a copy for its standalone page. They must stay identical.
func TestStreamModuleCopy(t *testing.T) {
	src, err := os.ReadFile("../../web/src/android/stream.js")
	if err != nil {
		t.Skip("the web sources are not here")
	}
	noCR := func(b []byte) []byte { return bytes.ReplaceAll(b, []byte{13}, nil) } // checkouts on Windows may use CRLF
	if !bytes.Equal(noCR(src), noCR(streamJS)) {
		t.Fatal("cmd/pheandroid/stream.js differs from web/src/android/stream.js: copy it over")
	}
}

func TestDisplayForAndIME(t *testing.T) {
	cases := []struct {
		vw, vh float64
		kind   string
		w, h   int
		dpi    int
	}{
		{390, 844, "phone", 720, 1552, 320}, // an iPhone-shaped window: 2.16
		{360, 640, "phone", 720, 1280, 320},
		{640, 360, "phone", 1280, 720, 320},   // a phone held sideways
		{1280, 800, "large", 1600, 1008, 213}, // 1.6
		{1920, 1080, "large", 1600, 896, 213}, // 1.78
		{2560, 1080, "large", 1600, 672, 213}, // ultrawide: the window's own shape
		{900, 900, "large", 1600, 1600, 213},  // a square window gets a square display
		{800, 1000, "large", 1280, 1600, 213}, // a tablet upright: 1.25
	}
	for _, c := range cases {
		w, h, d := displayFor(c.vw, c.vh, c.kind)
		if w != c.w || h != c.h || d != c.dpi {
			t.Errorf("%v %s: got %dx%d@%d, want %dx%d@%d", [2]float64{c.vw, c.vh}, c.kind, w, h, d, c.w, c.h, c.dpi)
		}
	}
	// the real `dumpsys input_method` lines (Android 13, Redroid): a focused text field with 5 characters selected, then one with the caret only, then no editor
	edit := `  mServedInputConnection=RemoteInputConnectionImpl{connection=com.android.internal.inputmethod.EditableInputConnection@1846c94 finished=false}
  mCursorSelStart=0 mCursorSelEnd=5 mCursorCandStart=-1 mCursorCandEnd=-1
`
	if e, s := parseIME(edit); !e || !s {
		t.Fatalf("selection: %v %v", e, s)
	}
	if e, s := parseIME(strings.Replace(edit, "mCursorSelEnd=5", "mCursorSelEnd=0", 1)); !e || s {
		t.Fatalf("caret only: %v %v", e, s)
	}
	if e, s := parseIME(`  mFallbackInputConnection=RemoteInputConnectionImpl{connection=android.view.inputmethod.BaseInputConnection@6ca20}
  mCursorSelStart=0 mCursorSelEnd=5
`); e || s {
		t.Fatalf("no editor: %v %v", e, s)
	}
}
