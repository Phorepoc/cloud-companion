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
	TDevices     = "devices"     // C->S  (admin) list devices
	TDeviceList  = "devicelist"  // S->C
	TRevoke      = "revoke"      // C->S  (admin) revoke a device
	TTab         = "tab"         // C->S  new | close | switch | list a view
	THub         = "hub"         // C->S  contacts / calendar request (the personal hub)
	THubRes      = "hubres"      // S->C  its answer
	TTabs        = "tabs"        // S->C  the views of this connection
	TZoom        = "zoom"        // C->S  re-render the view at a scale (percent; needs the "zoom" feature)
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
	T        string   `cbor:"t"`
	Dev      string   `cbor:"dev"`
	Key      []byte   `cbor:"key,omitempty"` // only in reply to pair
	Server   string   `cbor:"server"`
	Admin    bool     `cbor:"admin,omitempty"`    // this device may list and revoke devices
	Features []string `cbor:"features,omitempty"` // optional capabilities of this server (none yet; "zoom" = renders at a requested scale)
}

type Error struct {
	T   string `cbor:"t"`
	Msg string `cbor:"msg"`
}

type Open struct {
	T     string  `cbor:"t"`
	V     int     `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
	URL   string  `cbor:"url"`
	Width int     `cbor:"width"`
	DPR   float64 `cbor:"dpr"`
}

// Navigate: URL, or Action = back | forward | reload.
type Navigate struct {
	T      string `cbor:"t"`
	V      int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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
	V       int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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
	V     int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
	PID   int    `cbor:"pid"`
	Seq   int    `cbor:"seq"`
	URL   string `cbor:"url"`
	Title string `cbor:"title"`
	W     int    `cbor:"w"`
	H     int    `cbor:"h"`
	TileW int    `cbor:"tw"`
	TileH int    `cbor:"th"`
	Trunc bool   `cbor:"trunc,omitempty"` // the page was cut at the height cap
	Zoom  int    `cbor:"zoom,omitempty"`  // render scale in percent (0 = 100); tiles, regions and layers are in device pixels at this scale
	Reset bool   `cbor:"reset,omitempty"` // same page re-rendered at another scale: drop all cached tiles
	Back  bool   `cbor:"back,omitempty"`  // history: can go back
	Fwd   bool   `cbor:"fwd,omitempty"`   // history: can go forward
}

type Tile struct {
	T     string `cbor:"t"`
	V     int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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
	V    int     `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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
	V    int      `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
	PID  int      `cbor:"pid"`
	Seq  int      `cbor:"seq"`
	List []Region `cbor:"list"`
}

type Done struct {
	T     string `cbor:"t"`
	V     int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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
	V         int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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
	V       int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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
	V       int    `cbor:"v,omitempty"` // view (tab) id; 0 = view 1
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

type DeviceInfo struct {
	ID      string `cbor:"id"`
	Name    string `cbor:"name"`
	Created int64  `cbor:"created"`
	Seen    int64  `cbor:"seen"`
	Revoked bool   `cbor:"revoked,omitempty"`
	Admin   bool   `cbor:"admin,omitempty"`
}

type DeviceList struct {
	T    string       `cbor:"t"`
	List []DeviceInfo `cbor:"list"`
}

type Revoke struct {
	T  string `cbor:"t"`
	ID string `cbor:"id"`
}

// Marshal encodes one frame without the TCP length prefix (a WebSocket message is one frame).
func Marshal(v any) ([]byte, error) { return cbor.Marshal(v) }

// TypeOf returns the "t" of an encoded frame.
func TypeOf(b []byte) (string, error) {
	var h Header
	err := cbor.Unmarshal(b, &h)
	return h.T, err
}

// MaxTabs is the per-device cap on views (tabs).
const MaxTabs = 3

// ZoomLadder lists the render scales in percent. Each divides 360 exactly (layout width 360/scale css px), so a tile row stays
// 360 device pixels wide.
var ZoomLadder = []int{100, 125, 150, 200, 250, 300}

// SnapZoom returns the ladder entry nearest to pct.
func SnapZoom(pct int) int {
	best := ZoomLadder[0]
	for _, z := range ZoomLadder {
		if abs(z-pct) < abs(best-pct) {
			best = z
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Tab: Action = new | close | switch | list; V = the view (new: omitted, the server picks the next free id).
type Tab struct {
	T      string `cbor:"t"`
	Action string `cbor:"action"`
	V      int    `cbor:"v,omitempty"`
}

type TabInfo struct {
	V     int    `cbor:"v"`
	Title string `cbor:"title,omitempty"`
	URL   string `cbor:"url,omitempty"`
}

type Tabs struct {
	T      string    `cbor:"t"`
	Active int       `cbor:"active"`
	List   []TabInfo `cbor:"list"`
}

type Zoom struct {
	T   string `cbor:"t"`
	V   int    `cbor:"v,omitempty"`
	Pct int    `cbor:"pct"`
}

// ---- the personal hub: contacts (CardDAV) and calendar (CalDAV) through the server ----

type Labeled struct {
	Type  string `cbor:"type,omitempty"`
	Value string `cbor:"value"`
}

type Contact struct {
	Href   string    `cbor:"href,omitempty"`
	ETag   string    `cbor:"etag,omitempty"`
	Name   string    `cbor:"name"` // display name (FN)
	Given  string    `cbor:"given,omitempty"`
	Family string    `cbor:"family,omitempty"`
	Org    string    `cbor:"org,omitempty"`
	Note   string    `cbor:"note,omitempty"`
	Phones []Labeled `cbor:"phones,omitempty"`
	Emails []Labeled `cbor:"emails,omitempty"`
}

// Event times are ISO 8601 as in the calendar: "2026-10-24" (all day), "2026-10-24T09:00:00" (floating local time) or with a
// trailing Z (UTC).
type Event struct {
	Href     string `cbor:"href,omitempty"`
	ETag     string `cbor:"etag,omitempty"`
	Title    string `cbor:"title"`
	Start    string `cbor:"start"`
	End      string `cbor:"end,omitempty"`
	AllDay   bool   `cbor:"allday,omitempty"`
	Location string `cbor:"location,omitempty"`
	Notes    string `cbor:"notes,omitempty"`
	Recurs   bool   `cbor:"recurs,omitempty"` // has a recurrence rule (shown once; editing keeps the rule)
	TZ       string `cbor:"tz,omitempty"`     // IANA zone: on write the local times are in this zone (TZID); on read set when the event had one (start/end are then UTC)
}

// Hub: Op = contacts.list | contacts.save | contacts.delete | events.list | events.save | events.delete.
// list: From/To (events, ISO dates). save: Contact/Event (Href empty = create). delete: Href + ETag.
type Hub struct {
	T       string   `cbor:"t"`
	ID      int      `cbor:"id"`
	Op      string   `cbor:"op"`
	Contact *Contact `cbor:"contact,omitempty"`
	Event   *Event   `cbor:"event,omitempty"`
	Href    string   `cbor:"href,omitempty"`
	ETag    string   `cbor:"etag,omitempty"`
	From    string   `cbor:"from,omitempty"`
	To      string   `cbor:"to,omitempty"`
	Name    string   `cbor:"name,omitempty"`   // apppw.add: a label for the device
	Days    int      `cbor:"days,omitempty"`   // apppw.add: optional expiry in days
	ID2     string   `cbor:"id2,omitempty"`    // apppw.revoke / mail.read: the object id
	Folder  string   `cbor:"folder,omitempty"` // mail.list: folder id
	Pos     int      `cbor:"pos,omitempty"`    // mail.list: offset
}

type HubRes struct {
	T        string       `cbor:"t"`
	ID       int          `cbor:"id"`
	OK       bool         `cbor:"ok"`
	Err      string       `cbor:"err,omitempty"`
	Contacts []Contact    `cbor:"contacts,omitempty"`
	Events   []Event      `cbor:"events,omitempty"`
	Contact  *Contact     `cbor:"contact,omitempty"`
	Event    *Event       `cbor:"event,omitempty"`
	AppPws   []AppPw      `cbor:"apppws,omitempty"`
	Secret   string       `cbor:"secret,omitempty"` // apppw.add: shown once
	Owner    string       `cbor:"owner,omitempty"`
	Domain   string       `cbor:"domain,omitempty"`
	Folders  []MailFolder `cbor:"folders,omitempty"`
	Mails    []MailItem   `cbor:"mails,omitempty"`
	Total    int          `cbor:"total,omitempty"`
	Mail     *MailBody    `cbor:"mail,omitempty"`
}

// ---- hub: app passwords (admin devices) and mail (read-only) ----

type AppPw struct {
	ID      string `cbor:"id"`
	Name    string `cbor:"name"`
	Created string `cbor:"created,omitempty"`
	Expires string `cbor:"expires,omitempty"`
}

type MailFolder struct {
	ID     string `cbor:"id"`
	Name   string `cbor:"name"`
	Role   string `cbor:"role,omitempty"` // inbox | sent | drafts | trash | junk | archive
	Total  int    `cbor:"total"`
	Unread int    `cbor:"unread"`
	Parent string `cbor:"parent,omitempty"`
}

type MailItem struct {
	ID      string `cbor:"id"`
	Subject string `cbor:"subject"`
	From    string `cbor:"from"`
	Date    string `cbor:"date"` // RFC 3339
	Preview string `cbor:"preview,omitempty"`
	Unread  bool   `cbor:"unread,omitempty"`
	Attach  bool   `cbor:"attach,omitempty"`
}

type MailBody struct {
	MailItem
	To   []string `cbor:"to,omitempty"`
	Cc   []string `cbor:"cc,omitempty"`
	Text string   `cbor:"text"`
	HTML bool     `cbor:"html,omitempty"` // the text was made from an HTML body
}
