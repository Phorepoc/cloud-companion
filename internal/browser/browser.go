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
	// Text focuses region id, inserts text, optionally presses Enter.
	Text(ctx context.Context, id int, text string, submit bool) error
	ScrollTo(ctx context.Context, y int) error
	// Snapshot renders the whole page (height capped) as one image, with fixed/sticky elements cut out into layers.
	Snapshot(ctx context.Context) (*Snapshot, error)
	Close()
}

type Snapshot struct {
	URL, Title string
	W, H       int
	Page       *image.RGBA
	Layers     []Layer
	Regions    []Region
	Truncated  bool // page taller than the cap
}

type Layer struct {
	ID          int
	Kind        string // fixed | sticky
	X, Y, W, H  int
	Z           int
	Top, Bottom *int
	Img         *image.RGBA
}

type Region struct {
	ID, Layer                 int
	X, Y, W, H                int
	Kind                      string
	Href, Type, Name          string
	Placeholder, Value, Label string
}
