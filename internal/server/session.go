// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/phorepoc/phecloud/internal/browser"
	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/tiles"
)

type tkey struct{ layer, x, y int }
type tval struct {
	hash uint64
	w, h int
}

// session is one phone connection with one browser tab. Requests are handled one at a time.
type session struct {
	srv *Server
	c   *conn
	log *slog.Logger

	dev    string
	br     browser.Session
	pid    int
	seq    int
	url    string
	cache  map[tkey]tval
	layers []browser.Layer
}

func (s *session) close() {
	if s.br != nil {
		s.br.Close()
	}
}

func (s *session) dispatch(ctx context.Context, t string, raw []byte) error {
	switch t {
	case proto.TPing:
		return s.c.send(proto.Header{T: proto.TPong})
	case proto.TOpen:
		var o proto.Open
		if err := proto.Decode(raw, &o); err != nil {
			return err
		}
		start := time.Now()
		if s.br == nil {
			b, err := s.srv.Browser.New(ctx)
			if err != nil {
				return err
			}
			s.br = b
			if ck := s.srv.Store.LoadCookies(s.dev); len(ck) > 0 {
				if err := b.SetCookies(ctx, ck); err != nil {
					s.log.Warn("restore cookies", "err", err)
				}
			}
		}
		if err := s.br.Open(ctx, o.URL, o.Width, o.DPR); err != nil {
			return err
		}
		return s.update(ctx, start)
	}
	if s.br == nil {
		return errors.New("no page open")
	}
	start := time.Now()
	switch t {
	case proto.TNavigate:
		var n proto.Navigate
		if err := proto.Decode(raw, &n); err != nil {
			return err
		}
		if err := s.br.Navigate(ctx, n.URL, n.Action); err != nil {
			return err
		}
	case proto.TInput:
		var in proto.Input
		if err := proto.Decode(raw, &in); err != nil {
			return err
		}
		switch in.Kind {
		case "tap":
			x, y, sy := in.X, in.Y, in.SY
			if in.Layer != 0 {
				var l *browser.Layer
				for i := range s.layers {
					if s.layers[i].ID == in.Layer {
						l = &s.layers[i]
					}
				}
				if l == nil {
					return fmt.Errorf("no layer %d", in.Layer)
				}
				x, y = l.X+in.X, l.Y+in.Y
				if l.Kind == "fixed" {
					sy = -1 // viewport coordinates, no scrolling
				}
			}
			if err := s.br.Tap(ctx, x, y, sy); err != nil {
				return err
			}
		case "scroll":
			if err := s.br.ScrollTo(ctx, in.Y); err != nil {
				return err
			}
		case "text":
			if err := s.br.Text(ctx, in.ID, in.Text, in.Submit); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown input kind %q", in.Kind)
		}
	default:
		return fmt.Errorf("unknown frame %q", t)
	}
	return s.update(ctx, start)
}

// update snapshots the page and sends what changed since the last update.
func (s *session) update(ctx context.Context, start time.Time) error {
	t0 := time.Now()
	sn, err := s.br.Snapshot(ctx)
	if err != nil {
		return err
	}
	snapMs := int(time.Since(t0).Milliseconds())
	fresh := sn.URL != s.url || s.cache == nil
	if fresh {
		s.pid++
		s.seq = 0
		s.cache = map[tkey]tval{}
		s.url = sn.URL
	}
	s.seq++
	s.layers = sn.Layers
	if err := s.c.send(proto.Page{T: proto.TPage, PID: s.pid, Seq: s.seq, URL: sn.URL, Title: sn.Title, W: sn.W, H: sn.H,
		TileW: tiles.TileW, TileH: tiles.TileH, Trunc: sn.Truncated}); err != nil {
		return err
	}
	var ll []proto.Layer
	for _, l := range sn.Layers {
		ll = append(ll, proto.Layer{ID: l.ID, Kind: l.Kind, X: l.X, Y: l.Y, W: l.W, H: l.H, Z: l.Z, Top: l.Top, Bottom: l.Bottom})
	}
	if err := s.c.send(proto.Layers{T: proto.TLayers, PID: s.pid, Seq: s.seq, List: ll}); err != nil {
		return err
	}
	nt, nb := 0, 0
	send := func(layer int, img *browser.Layer, page bool) error {
		var im = sn.Page
		if !page {
			im = img.Img
		}
		for _, t := range tiles.Cut(im) {
			k := tkey{layer, t.X, t.Y}
			if old, ok := s.cache[k]; ok && old.hash == t.Hash && old.w == t.W && old.h == t.H {
				continue
			}
			if err := tiles.Encode(im, &t); err != nil {
				return err
			}
			s.cache[k] = tval{t.Hash, t.W, t.H}
			nt++
			nb += len(t.Data)
			if err := s.c.send(proto.Tile{T: proto.TTile, PID: s.pid, Layer: layer, X: t.X, Y: t.Y, W: t.W, H: t.H, Fmt: t.Fmt, Hash: t.Hash, Data: t.Data}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := send(0, nil, true); err != nil {
		return err
	}
	for i := range sn.Layers {
		if err := send(sn.Layers[i].ID, &sn.Layers[i], false); err != nil {
			return err
		}
	}
	var rr []proto.Region
	for _, r := range sn.Regions {
		rr = append(rr, proto.Region{ID: r.ID, Layer: r.Layer, X: r.X, Y: r.Y, W: r.W, H: r.H, Kind: r.Kind, Href: r.Href,
			Type: r.Type, Name: r.Name, Placeholder: r.Placeholder, Value: r.Value, Label: r.Label})
	}
	if err := s.c.send(proto.Regions{T: proto.TRegions, PID: s.pid, Seq: s.seq, List: rr}); err != nil {
		return err
	}
	if ck, err := s.br.Cookies(ctx); err != nil {
		s.log.Warn("cookies export", "err", err)
	} else {
		s.srv.Store.SaveCookies(s.dev, ck)
		s.log.Debug("cookies saved", "bytes", len(ck))
	}
	ms := int(time.Since(start).Milliseconds())
	s.log.Info("update", "pid", s.pid, "seq", s.seq, "url", func() string {
		if len(sn.URL) > 100 {
			return sn.URL[:100]
		}
		return sn.URL
	}(), "h", sn.H, "layers", len(sn.Layers), "regions", len(rr), "tiles", nt, "bytes", nb, "ms", ms, "snapshot_ms", snapMs)
	return s.c.send(proto.Done{T: proto.TDone, PID: s.pid, Seq: s.seq, Ms: ms, Tiles: nt, Bytes: nb})
}
