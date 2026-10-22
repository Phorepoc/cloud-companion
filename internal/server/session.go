// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
type dialogAnswer struct {
	ok         bool
	text, pass string
}

type session struct {
	done    chan struct{}
	dialogs map[int]chan dialogAnswer
	dmu     sync.Mutex
	dnext   int
	srv     *Server
	c       *conn
	log     *slog.Logger

	dev    string
	admin  bool
	br     browser.Session
	pid    int
	seq    int
	url    string
	cache  map[tkey]tval
	layers []browser.Layer
	opts   tiles.Options // the per-device byte budget
	loaded bool
}

func (s *session) close() {
	close(s.done)
	if s.br != nil {
		s.br.Close()
	}
}

func (s *session) dispatch(ctx context.Context, t string, raw []byte) error {
	if !s.loaded {
		s.loaded = true
		if b := s.srv.Store.LoadSettings(s.dev); len(b) > 0 {
			var st proto.Settings
			if json.Unmarshal(b, &st) == nil {
				s.opts = tiles.Options{Quality: st.Quality, Gray: st.Gray}
			}
		}
	}
	switch t {
	case proto.TSettings:
		var st proto.Settings
		if err := proto.Decode(raw, &st); err != nil {
			return err
		}
		s.opts = tiles.Options{Quality: st.Quality, Gray: st.Gray}
		b, _ := json.Marshal(st)
		s.srv.Store.SaveSettings(s.dev, b)
		s.cache = nil // re-send every tile with the new budget on the next update
		return nil
	case proto.TDevices:
		if !s.admin {
			return errors.New("admin only")
		}
		ds, err := s.srv.Store.List()
		if err != nil {
			return err
		}
		var out []proto.DeviceInfo
		for _, d := range ds {
			out = append(out, proto.DeviceInfo{ID: d.ID, Name: d.Name, Created: d.Created, Seen: d.Seen, Revoked: d.Revoked, Admin: d.Admin})
		}
		return s.c.send(proto.DeviceList{T: proto.TDeviceList, List: out})
	case proto.TRevoke:
		if !s.admin {
			return errors.New("admin only")
		}
		var r proto.Revoke
		if err := proto.Decode(raw, &r); err != nil {
			return err
		}
		return s.srv.Store.Revoke(r.ID)
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
			b.SetHandlers(browser.Handlers{Dialog: s.onDialog, Download: s.onDownload})
			if ck := s.srv.Store.LoadCookies(s.dev); len(ck) > 0 {
				if err := b.SetCookies(ctx, ck); err != nil {
					s.log.Warn("restore cookies", "err", err)
				}
			}
		}
		if err := s.br.Open(ctx, o.URL, o.Width, o.DPR); err != nil {
			return err
		}
		return s.update(ctx, start, true)
	}
	if s.br == nil {
		return errors.New("no page open")
	}
	start := time.Now()
	progressive := false // page loads and taps (which usually navigate) stream the first screenful first
	switch t {
	case proto.TNavigate:
		var n proto.Navigate
		if err := proto.Decode(raw, &n); err != nil {
			return err
		}
		progressive = true
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
			progressive = true
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
				if l.Kind == "scroll" {
					if err := s.br.TapScroll(ctx, in.Layer, in.X, in.Y); err != nil {
						return err
					}
					break
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
		case "key":
			if err := s.br.Key(ctx, in.Text); err != nil {
				return err
			}
		case "select":
			if err := s.br.Select(ctx, in.ID, in.Index); err != nil {
				return err
			}
		case "text":
			if err := s.br.Text(ctx, in.ID, in.Text, in.Submit, in.Replace); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown input kind %q", in.Kind)
		}
	default:
		return fmt.Errorf("unknown frame %q", t)
	}
	return s.update(ctx, start, progressive)
}

// update snapshots the page and sends what changed since the last update.
// update snapshots the page and sends what changed. progressive (page loads): the first screenful goes out first with
// done.more = true, the rest follows after the lazy-load pass.
func (s *session) update(ctx context.Context, start time.Time, progressive bool) error {
	if progressive {
		t0 := time.Now()
		sn, err := s.br.Snapshot(ctx, true)
		if err != nil {
			return err
		}
		if err := s.sendSnapshot(ctx, start, sn, int(time.Since(t0).Milliseconds()), sn.Partial); err != nil {
			return err
		}
		if !sn.Partial {
			return nil
		}
	}
	t0 := time.Now()
	sn, err := s.br.Snapshot(ctx, false)
	if err != nil {
		return err
	}
	return s.sendSnapshot(ctx, start, sn, int(time.Since(t0).Milliseconds()), false)
}

func (s *session) sendSnapshot(ctx context.Context, start time.Time, sn *browser.Snapshot, snapMs int, more bool) error {
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
		TileW: tiles.TileW, TileH: tiles.TileH, Trunc: sn.Truncated, Back: sn.Back, Fwd: sn.Fwd}); err != nil {
		return err
	}
	var ll []proto.Layer
	for _, l := range sn.Layers {
		ll = append(ll, proto.Layer{ID: l.ID, Kind: l.Kind, X: l.X, Y: l.Y, W: l.W, H: l.H, Z: l.Z, Top: l.Top, Bottom: l.Bottom,
			CW: l.CW, CH: l.CH, SX: l.SX, SY: l.SY})
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
			if err := tiles.EncodeOpt(im, &t, s.opts); err != nil {
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
		pr := proto.Region{ID: r.ID, Layer: r.Layer, X: r.X, Y: r.Y, W: r.W, H: r.H, Kind: r.Kind, Href: r.Href,
			Type: r.Type, Name: r.Name, Placeholder: r.Placeholder, Value: r.Value, Label: r.Label}
		for _, o := range r.Opts {
			pr.Opts = append(pr.Opts, proto.Opt{Label: o.Label, Sel: o.Sel})
		}
		rr = append(rr, pr)
	}
	if err := s.c.send(proto.Regions{T: proto.TRegions, PID: s.pid, Seq: s.seq, List: rr}); err != nil {
		return err
	}
	f := sn.Focus
	if err := s.c.send(proto.Focus{T: proto.TFocus, PID: s.pid, Seq: s.seq, ID: f.ID, Kind: f.Kind, Type: f.Type, Value: f.Value,
		Start: f.Start, End: f.End, InputMode: f.InputMode, Enter: f.EnterKey}); err != nil {
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
	}(), "h", sn.H, "layers", len(sn.Layers), "regions", len(rr), "tiles", nt, "bytes", nb, "ms", ms, "snapshot_ms", snapMs, "more", more)
	return s.c.send(proto.Done{T: proto.TDone, PID: s.pid, Seq: s.seq, Ms: ms, Tiles: nt, Bytes: nb, More: more})
}

// onDialog runs on the browser's event goroutine: ask the phone and wait for its reply (60 s, then dismiss).
func (s *session) onDialog(d browser.Dialog) browser.Answer {
	s.dmu.Lock()
	s.dnext++
	id := s.dnext
	ch := make(chan dialogAnswer, 1)
	s.dialogs[id] = ch
	s.dmu.Unlock()
	defer func() { s.dmu.Lock(); delete(s.dialogs, id); s.dmu.Unlock() }()
	if err := s.c.send(proto.Dialog{T: proto.TDialog, ID: id, Kind: d.Kind, Message: d.Message, Default: d.Default, URL: d.URL}); err != nil {
		return browser.Answer{}
	}
	select {
	case a := <-ch:
		return browser.Answer{OK: a.ok, Text: a.text, Pass: a.pass}
	case <-time.After(60 * time.Second):
	case <-s.done:
	}
	return browser.Answer{}
}

func (s *session) dialogReply(raw []byte) {
	var r proto.DialogReply
	if proto.Decode(raw, &r) != nil {
		return
	}
	s.dmu.Lock()
	ch := s.dialogs[r.ID]
	s.dmu.Unlock()
	if ch != nil {
		select {
		case ch <- dialogAnswer{r.OK, r.Text, r.Pass}:
		default:
		}
	}
}

func (s *session) onDownload(d browser.Download) {
	s.log.Info("download refused", "url", d.URL, "name", d.Name)
	s.c.send(proto.Download{T: proto.TDownload, URL: d.URL, Name: d.Name, Refused: true})
}
