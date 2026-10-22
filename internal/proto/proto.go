// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package proto is the phone protocol: one TCP connection carrying length-prefixed CBOR frames.
//
// Frame = 4-byte big-endian length N, then N bytes of CBOR. Every frame is a CBOR map whose "t" key names its type.
package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
)

const (
	Version  = 1
	MaxFrame = 8 << 20
)

// Frame types.
const (
	TChallenge   = "challenge" // S->C  first frame
	TPair        = "pair"      // C->S  pairing code -> new device key
	THello       = "hello"     // C->S  device id + HMAC(key, nonce)
	TWelcome     = "welcome"   // S->C
	TError       = "error"     // S->C
	TOpen        = "open"      // C->S
	TNavigate    = "navigate"  // C->S
	TInput       = "input"     // C->S
	TPage        = "page"      // S->C  page geometry (new page or resized)
	TTile        = "tile"      // S->C
	TLayers      = "layers"    // S->C  full list of fixed/sticky layers
	TRegions     = "regions"   // S->C  full list of tap regions
	TDone        = "done"      // S->C  end of one update
	TPing        = "ping"
	TPong        = "pong"
	TFocus       = "focus"       // S->C  what has keyboard focus after an update
	TDialog      = "dialog"      // S->C  a JavaScript alert/confirm/prompt/beforeunload is waiting
	TDialogReply = "dialogreply" // C->S
	TDownload    = "download"    // S->C  a download was refused
	TSettings    = "settings"    // C->S  per-device byte budget: JPEG quality, grayscale
)

type Header struct {
	T string `cbor:"t"`
}

type Challenge struct {
	T     string `cbor:"t"`
	Proto int    `cbor:"proto"`
	Nonce []byte `cbor:"nonce"`
}

type Pair struct {
	T    string `cbor:"t"`
	Code string `cbor:"code"`
	Name string `cbor:"name"`
}

type Hello struct {
	T   string `cbor:"t"`
	Dev string `cbor:"dev"`
	Mac []byte `cbor:"mac"` // HMAC-SHA256(key, nonce || "phecloud-v1")
}

type Welcome struct {
	T      string `cbor:"t"`
	Dev    string `cbor:"dev"`
	Key    []byte `cbor:"key,omitempty"` // only in reply to pair
	Server string `cbor:"server"`
}

type Error struct {
	T   string `cbor:"t"`
	Msg string `cbor:"msg"`
}

type Open struct {
	T     string  `cbor:"t"`
	URL   string  `cbor:"url"`
	Width int     `cbor:"width"`
	DPR   float64 `cbor:"dpr"`
}

// Navigate: URL, or Action = back | forward | reload.
type Navigate struct {
	T      string `cbor:"t"`
	URL    string `cbor:"url,omitempty"`
	Action string `cbor:"action,omitempty"`
}

// Input: Kind = tap | scroll | text.
//
//	tap:    Layer (0 = page), X,Y in that layer's coordinates, SY = the client's current scroll position (page layer)
//	scroll: Y = new scroll position
//	text:   ID = region id, Text, Submit = press Enter afterwards
type Input struct {
	T       string `cbor:"t"`
	Kind    string `cbor:"kind"`
	Layer   int    `cbor:"layer,omitempty"`
	X       int    `cbor:"x,omitempty"`
	Y       int    `cbor:"y,omitempty"`
	SY      int    `cbor:"sy,omitempty"`
	ID      int    `cbor:"id,omitempty"`
	Text    string `cbor:"text,omitempty"`
	Submit  bool   `cbor:"submit,omitempty"`
	Replace bool   `cbor:"replace,omitempty"` // text: select the field's content first
	Index   int    `cbor:"index,omitempty"`   // select: option index
}

type Page struct {
	T     string `cbor:"t"`
	PID   int    `cbor:"pid"`
	Seq   int    `cbor:"seq"`
	URL   string `cbor:"url"`
	Title string `cbor:"title"`
	W     int    `cbor:"w"`
	H     int    `cbor:"h"`
	TileW int    `cbor:"tw"`
	TileH int    `cbor:"th"`
	Trunc bool   `cbor:"trunc,omitempty"` // the page was cut at the height cap
	Back  bool   `cbor:"back,omitempty"`  // history: can go back
	Fwd   bool   `cbor:"fwd,omitempty"`   // history: can go forward
}

type Tile struct {
	T     string `cbor:"t"`
	PID   int    `cbor:"pid"`
	Layer int    `cbor:"layer"`
	X     int    `cbor:"x"`
	Y     int    `cbor:"y"`
	W     int    `cbor:"w"`
	H     int    `cbor:"h"`
	Fmt   string `cbor:"fmt"` // png | jpeg
	Hash  uint64 `cbor:"hash"`
	Data  []byte `cbor:"data"`
}

// Layer. Kind fixed: X,Y = viewport position. Kind sticky: X,Y = natural document position at scroll 0,
// Top/Bottom = the CSS offset the element sticks at (px).
type Layer struct {
	T      string `cbor:"t,omitempty"`
	ID     int    `cbor:"id"`
	Kind   string `cbor:"kind"`
	X      int    `cbor:"x"`
	Y      int    `cbor:"y"`
	W      int    `cbor:"w"`
	H      int    `cbor:"h"`
	Z      int    `cbor:"z"`
	Top    *int   `cbor:"top,omitempty"`
	Bottom *int   `cbor:"bottom,omitempty"`
	// kind scroll (an inner overflow:auto box): X,Y,W,H = the visible box in document coordinates; CW,CH = the content
	// size (the layer tiles cover CW x CH); SX,SY = the initial scroll offset.
	CW int `cbor:"cw,omitempty"`
	CH int `cbor:"ch,omitempty"`
	SX int `cbor:"sx,omitempty"`
	SY int `cbor:"sy,omitempty"`
}

type Layers struct {
	T    string  `cbor:"t"`
	PID  int     `cbor:"pid"`
	Seq  int     `cbor:"seq"`
	List []Layer `cbor:"list"`
}

// Region: a tap target. Layer 0 = page (document coordinates); else relative to that layer's top-left.
type Region struct {
	ID          int    `cbor:"id"`
	Layer       int    `cbor:"layer"`
	X           int    `cbor:"x"`
	Y           int    `cbor:"y"`
	W           int    `cbor:"w"`
	H           int    `cbor:"h"`
	Kind        string `cbor:"kind"` // link | button | input | select | textarea
	Href        string `cbor:"href,omitempty"`
	Type        string `cbor:"type,omitempty"`
	Name        string `cbor:"name,omitempty"`
	Placeholder string `cbor:"ph,omitempty"`
	Value       string `cbor:"value,omitempty"`
	Label       string `cbor:"label,omitempty"`
	Opts        []Opt  `cbor:"opts,omitempty"` // select: the options
}

type Regions struct {
	T    string   `cbor:"t"`
	PID  int      `cbor:"pid"`
	Seq  int      `cbor:"seq"`
	List []Region `cbor:"list"`
}

type Done struct {
	T     string `cbor:"t"`
	PID   int    `cbor:"pid"`
	Seq   int    `cbor:"seq"`
	Ms    int    `cbor:"ms"` // server render time
	Tiles int    `cbor:"tiles"`
	Bytes int    `cbor:"bytes"`          // tile payload bytes in this update
	More  bool   `cbor:"more,omitempty"` // progressive load: another `done` follows (the first one marks the first screenful)
}

// Write encodes v and writes one frame.
func Write(w io.Writer, v any) (int, error) {
	b, err := cbor.Marshal(v)
	if err != nil {
		return 0, err
	}
	if len(b) > MaxFrame {
		return 0, fmt.Errorf("frame too large: %d", len(b))
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	return w.Write(buf)
}

// Read reads one frame and returns its type and raw CBOR payload.
func Read(r io.Reader) (string, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxFrame {
		return "", nil, errors.New("bad frame length")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", nil, err
	}
	var h Header
	if err := cbor.Unmarshal(b, &h); err != nil {
		return "", nil, err
	}
	return h.T, b, nil
}

// Decode decodes a raw payload into v.
func Decode(raw []byte, v any) error { return cbor.Unmarshal(raw, v) }

// Opt is one <select> option.
type Opt struct {
	Label string `cbor:"l"`
	Sel   bool   `cbor:"s,omitempty"`
}

// Focus is sent after every update. Kind: none | text | textarea | select (a focused <select>); Type is the HTML input
// type (text, search, email, url, tel, number, password, ...). Value is empty for password fields.
type Focus struct {
	T         string `cbor:"t"`
	PID       int    `cbor:"pid"`
	Seq       int    `cbor:"seq"`
	ID        int    `cbor:"id,omitempty"`
	Kind      string `cbor:"kind"`
	Type      string `cbor:"type,omitempty"`
	Value     string `cbor:"value,omitempty"`
	Start     int    `cbor:"start"`
	End       int    `cbor:"end"`
	InputMode string `cbor:"im,omitempty"`
	Enter     string `cbor:"enter,omitempty"` // enterkeyhint
}

// Dialog: Kind = alert | confirm | prompt | beforeunload. The page stays blocked until the phone sends DialogReply
// (or 60 s pass, then it is dismissed).
type Dialog struct {
	T       string `cbor:"t"`
	ID      int    `cbor:"id"`
	Kind    string `cbor:"kind"`
	Message string `cbor:"msg"`
	Default string `cbor:"def,omitempty"`
	URL     string `cbor:"url,omitempty"`
}

type DialogReply struct {
	T    string `cbor:"t"`
	ID   int    `cbor:"id"`
	OK   bool   `cbor:"ok"`
	Text string `cbor:"text,omitempty"`
	Pass string `cbor:"pass,omitempty"` // auth dialogs: Text = user name, Pass = password
}

// Download is informational: v1 never downloads in the cloud.
type Download struct {
	T       string `cbor:"t"`
	URL     string `cbor:"url"`
	Name    string `cbor:"name"`
	Refused bool   `cbor:"refused"`
}

// Settings is the per-device byte budget (stored by the server per device and applied to every later tile).
// Quality 0 = default (70); Gray = grayscale tiles.
type Settings struct {
	T       string `cbor:"t"`
	Quality int    `cbor:"q,omitempty"`
	Gray    bool   `cbor:"gray,omitempty"`
}
