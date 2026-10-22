// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package browser is the cloud-browser backend. Everything that talks to Chromium (DevTools protocol, whose
// domains we use are partly experimental) sits behind the Factory/Session interfaces, so a backend can be swapped.
package browser

import (
	"context"
	"image"
)

// Factory owns the browser process; Session is one tab.
type Factory interface {
	New(ctx context.Context) (Session, error)
	Close()
}

type Session interface {
	// Open sets the viewport (width x 640 CSS px, dpr) and navigates.
	Open(ctx context.Context, url string, width int, dpr float64) error
	// Navigate: url, or action back | forward | reload.
	Navigate(ctx context.Context, url, action string) error
	// Tap clicks at (x, y); see the implementation for the coordinate rules.
	Tap(ctx context.Context, x, y, sy int) error
	// Text inserts text into region id (id 0: the focused element), optionally selecting its content first (replace) and
	// pressing Enter after.
	Text(ctx context.Context, id int, text string, submit, replace bool) error
	// Key presses a named key (Backspace, Enter, Tab, Delete, ArrowLeft/Right/Up/Down) in the focused element.
	Key(ctx context.Context, name string) error
	// Select picks option index of the <select> region id.
	Select(ctx context.Context, id, index int) error
	// SetHandlers installs the callbacks for JavaScript dialogs and refused downloads.
	SetHandlers(h Handlers)
	ScrollTo(ctx context.Context, y int) error
	// Cookies exports the tab's cookie jar (JSON); SetCookies imports one.
	Cookies(ctx context.Context) ([]byte, error)
	SetCookies(ctx context.Context, data []byte) error
	// Snapshot renders the page (height capped) as one image, with fixed/sticky elements and inner scroll boxes cut out into
	// layers. quick = only the first screenful (and no lazy-load pass); Snapshot.Partial then says more follows.
	Snapshot(ctx context.Context, quick bool) (*Snapshot, error)
	// TapScroll taps a point (content coordinates) inside an inner scroll layer, scrolling the box there first.
	TapScroll(ctx context.Context, layer, x, y int) error
	Close()
}

type Snapshot struct {
	URL, Title string
	W, H       int
	Page       *image.RGBA
	Layers     []Layer
	Regions    []Region
	Truncated  bool // page taller than the cap
	Partial    bool // quick snapshot of a taller page: Page covers only the first rows
	Back, Fwd  bool // history
	Focus      Focus
}

type Layer struct {
	ID             int
	Kind           string // fixed | sticky | scroll
	X, Y, W, H     int
	Z              int
	Top, Bottom    *int
	CW, CH, SX, SY int // kind scroll: content size and initial scroll offset
	Img            *image.RGBA
}

type Region struct {
	ID, Layer                 int
	X, Y, W, H                int
	Kind                      string
	Href, Type, Name          string
	Placeholder, Value, Label string
	Opts                      []Opt
}

type Opt struct {
	Label string
	Sel   bool
}

type Focus struct {
	ID         int
	Kind       string // none | text | textarea | select
	Type       string
	Value      string
	Start, End int
	InputMode  string
	EnterKey   string
}

// Dialog.Kind: alert | confirm | prompt | beforeunload | auth (HTTP authentication) | cert (certificate error).
type Dialog struct {
	Kind, Message, Default, URL string
}

type Download struct{ URL, Name string }

// Handlers are called from the browser's event goroutine; Dialog blocks until the phone answers.
type Handlers struct {
	Dialog   func(Dialog) Answer
	Download func(Download)
}

// Answer is the phone's reply to a Dialog: OK = accept/continue/provide credentials; Text = the prompt text or the user
// name (auth); Pass = the password (auth).
type Answer struct {
	OK         bool
	Text, Pass string
}
