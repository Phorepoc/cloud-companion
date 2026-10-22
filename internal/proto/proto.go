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
	TChallenge = "challenge" // S->C  first frame
	TPair      = "pair"      // C->S  pairing code -> new device key
	THello     = "hello"     // C->S  device id + HMAC(key, nonce)
	TWelcome   = "welcome"   // S->C
	TError     = "error"     // S->C
	TOpen      = "open"      // C->S
	TNavigate  = "navigate"  // C->S
	TInput     = "input"     // C->S
	TPage      = "page"      // S->C  page geometry (new page or resized)
	TTile      = "tile"      // S->C
	TLayers    = "layers"    // S->C  full list of fixed/sticky layers
	TRegions   = "regions"   // S->C  full list of tap regions
	TDone      = "done"      // S->C  end of one update
	TPing      = "ping"
	TPong      = "pong"
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
	T      string `cbor:"t"`
	Kind   string `cbor:"kind"`
	Layer  int    `cbor:"layer,omitempty"`
	X      int    `cbor:"x,omitempty"`
	Y      int    `cbor:"y,omitempty"`
	SY     int    `cbor:"sy,omitempty"`
	ID     int    `cbor:"id,omitempty"`
	Text   string `cbor:"text,omitempty"`
	Submit bool   `cbor:"submit,omitempty"`
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
	Bytes int    `cbor:"bytes"` // tile payload bytes in this update
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
