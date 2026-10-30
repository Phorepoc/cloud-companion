// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/phorepoc/cloud-companion/internal/browser"
	"github.com/phorepoc/cloud-companion/internal/proto"
	"github.com/phorepoc/cloud-companion/internal/tiles"
)

type tkey struct{ layer, x, y int }
type tval struct {
	hash uint64
	w, h int
}

type dialogAnswer struct {
	ok         bool
	text, pass string
}

// tab is one view of a connection: one browser tab and what the phone has of it. Frames of a view carry its id (`v`).
type tab struct {
	id     int
	br     browser.Session
	pid    int
	seq    int
	url    string
	title  string
	cache  map[tkey]tval
	layers []browser.Layer
	zoom   int  // render scale in percent
	reset  bool // the next update re-renders the same page at another scale
	vw, vh int  // the viewport size last asked for (0 = the 360 x 640 default)
	sent   string
}

// session is one device connection with up to proto.MaxTabs views. Requests are handled one at a time; the embedded *tab is
// the view the current request is about.
type session struct {
	done    chan struct{}
	dialogs map[int]chan dialogAnswer
	dmu     sync.Mutex
	dnext   int
	srv     *Server
	c       *conn
	log     *slog.Logger

	dev     string
	admin   bool
	sid     int
	devName string
	ops     chan func(ctx context.Context) error // actions other devices ask this session to run (close a tab, open a page)
	pubMu   sync.Mutex
	pub     []proto.TabInfo // the tab list as last sent to this device
	opts    tiles.Options   // the per-device byte budget
	loaded  bool

	tabs   map[int]*tab
	active int
	*tab
}

func (s *session) close() {
	close(s.done)
	for _, t := range s.tabs {
		if t.br != nil {
			t.br.Close()
		}
	}
}

func viewOf(raw []byte) int {
	var h struct {
		V int `cbor:"v"`
	}
	proto.Decode(raw, &h)
	if h.V <= 0 {
		return 1
	}
	return h.V
}

// tabFor returns the view v, creating it when allowed (per-device cap).
func (s *session) tabFor(v int, create bool) (*tab, error) {
	if t := s.tabs[v]; t != nil {
		return t, nil
	}
	if !create {
		return nil, fmt.Errorf("no view %d", v)
	}
	if len(s.tabs) >= proto.MaxTabs {
		return nil, fmt.Errorf("tab limit (%d per device)", proto.MaxTabs)
	}
	t := &tab{id: v, zoom: 100}
	if s.tabs == nil {
		s.tabs = map[int]*tab{}
	}
	s.tabs[v] = t
	return t, nil
}

func (s *session) sendTabs() error {
	var ids []int
	for id := range s.tabs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := proto.Tabs{T: proto.TTabs, Active: s.active}
	for _, id := range ids {
		t := s.tabs[id]
		out.List = append(out.List, proto.TabInfo{V: id, Title: t.title, URL: truncate(t.url, 200)})
	}
	s.pubMu.Lock()
	s.pub = out.List
	s.pubMu.Unlock()
	return s.c.send(out)
}

func truncate(u string, n int) string {
	if len(u) > n {
		return u[:n]
	}
	return u
}

func (s *session) startBrowser(ctx context.Context, t *tab) error {
	if t.br != nil {
		return nil
	}
	b, err := s.srv.Browser.New(ctx)
	if err != nil {
		return err
	}
	t.br = b
	id := t.id
	b.SetHandlers(browser.Handlers{
		Dialog:   func(d browser.Dialog) browser.Answer { return s.onDialog(id, d) },
		Download: func(d browser.Download) { s.onDownload(id, d) },
	})
	if ck := s.srv.Store.LoadCookies(s.dev); len(ck) > 0 {
		if err := b.SetCookies(ctx, ck); err != nil {
			s.log.Warn("restore cookies", "err", err)
		}
	}
	return nil
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
		for _, tb := range s.tabs {
			tb.cache = nil // re-send every tile with the new budget on the next update
		}
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
	case proto.TSessions:
		return s.c.send(proto.SessionList{T: proto.TSessionList, List: s.srv.sessionList(s)})
	case proto.TRemote:
		return s.remote(raw)
	case proto.THub:
		return s.hubRequest(raw)
	case proto.TSMS:
		return s.smsSync(raw)
	case proto.TTab:
		return s.tabAction(ctx, raw)
	case proto.TOpen:
		var o proto.Open
		if err := proto.Decode(raw, &o); err != nil {
			return err
		}
		tb, err := s.tabFor(viewOf(raw), true)
		if err != nil {
			return err
		}
		s.tab, s.active = tb, tb.id
		start := time.Now()
		if err := s.startBrowser(ctx, tb); err != nil {
			return err
		}
		if o.Height > 0 { // a window that is not phone sized: lay the page out at its size
			w, h := clampViewport(o.Width, o.Height)
			if err := tb.br.SetViewport(ctx, w, h); err != nil {
				return err
			}
			o.Width = w
		}
		if err := tb.br.Open(ctx, o.URL, o.Width, o.DPR); err != nil {
			return err
		}
		return s.update(ctx, start, true)
	}
	tb, err := s.tabFor(viewOf(raw), false)
	if err != nil {
		return err
	}
	s.tab, s.active = tb, tb.id
	if s.br == nil {
		return errors.New("no page open")
	}
	start := time.Now()
	progressive := false // page loads and taps (which usually navigate) stream the first screenful first
	z := float64(s.zoom) / 100
	css := func(v int) int { return int(math.Round(float64(v) / z)) } // device pixels at the render scale -> css px
	switch t {
	case proto.TZoom:
		var zr proto.Zoom
		if err := proto.Decode(raw, &zr); err != nil {
			return err
		}
		pct := proto.SnapZoom(zr.Pct)
		if pct == s.zoom {
			return nil
		}
		if err := s.br.SetScale(ctx, pct); err != nil {
			return err
		}
		s.zoom, s.reset = pct, true
	case proto.TViewport:
		var vp proto.Viewport
		if err := proto.Decode(raw, &vp); err != nil {
			return err
		}
		w, h := clampViewport(vp.Width, vp.Height)
		if w == s.vw && h == s.vh {
			return nil
		}
		// every tab of this device is laid out for the window (the others are re-sent when they next update)
		for _, t := range s.tabs {
			if t.br == nil {
				continue
			}
			if err := t.br.SetViewport(ctx, w, h); err != nil {
				return err
			}
			t.vw, t.vh, t.reset = w, h, true
		}
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
					if err := s.br.TapScroll(ctx, in.Layer, css(in.X), css(in.Y)); err != nil {
						return err
					}
					break
				}
				x, y = l.X+in.X, l.Y+in.Y
				if l.Kind == "fixed" {
					sy = -1 // viewport coordinates, no scrolling
				}
			}
			if sy >= 0 {
				sy = css(sy)
			}
			if err := s.br.Tap(ctx, css(x), css(y), sy); err != nil {
				return err
			}
		case "scroll":
			if err := s.br.ScrollTo(ctx, css(in.Y)); err != nil {
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

// tabAction: new | close | switch | list.
func (s *session) tabAction(ctx context.Context, raw []byte) error {
	var ta proto.Tab
	if err := proto.Decode(raw, &ta); err != nil {
		return err
	}
	switch ta.Action {
	case "list":
		return s.sendTabs()
	case "new":
		id := 1
		for s.tabs[id] != nil {
			id++
		}
		tb, err := s.tabFor(id, true)
		if err != nil {
			return err
		}
		s.tab, s.active = tb, id
		return s.sendTabs()
	case "close":
		tb := s.tabs[ta.V]
		if tb == nil {
			return fmt.Errorf("no view %d", ta.V)
		}
		if tb.br != nil {
			tb.br.Close()
		}
		delete(s.tabs, ta.V)
		if s.tab == tb {
			s.tab = nil
		}
		if s.active == ta.V { // fall back to the lowest remaining view and show it
			s.active = 0
			for id := 1; id <= 64; id++ {
				if s.tabs[id] != nil {
					s.active = id
					break
				}
			}
		}
		if err := s.sendTabs(); err != nil {
			return err
		}
		if s.active != 0 {
			return s.showTab(ctx, s.tabs[s.active])
		}
		return nil
	case "switch":
		tb := s.tabs[ta.V]
		if tb == nil {
			return fmt.Errorf("no view %d", ta.V)
		}
		s.active = ta.V
		if err := s.sendTabs(); err != nil {
			return err
		}
		return s.showTab(ctx, tb)
	}
	return fmt.Errorf("unknown tab action %q", ta.Action)
}

// showTab re-sends a view in full (the phone drops what it had when it switches views).
func (s *session) showTab(ctx context.Context, tb *tab) error {
	s.tab = tb
	if tb.br == nil || tb.url == "" {
		return nil
	}
	tb.cache = nil
	return s.update(ctx, time.Now(), false)
}

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
	reset := false
	switch {
	case s.reset:
		// the same page re-rendered at another scale: same pid, the phone drops its tiles
		s.reset, reset = false, true
		s.cache = map[tkey]tval{}
		s.seq = 0
	case sn.URL != s.url || s.cache == nil:
		s.pid++
		s.seq = 0
		s.cache = map[tkey]tval{}
	}
	s.url = sn.URL
	s.title = sn.Title
	s.seq++
	s.layers = sn.Layers
	v := s.id
	zoom := 0
	if s.zoom != 100 {
		zoom = s.zoom
	}
	if err := s.c.send(proto.Page{T: proto.TPage, V: v, PID: s.pid, Seq: s.seq, URL: sn.URL, Title: sn.Title, W: sn.W, H: sn.H,
		TileW: tiles.TileW, TileH: tiles.TileH, Trunc: sn.Truncated, Back: sn.Back, Fwd: sn.Fwd, Zoom: zoom, Reset: reset}); err != nil {
		return err
	}
	var ll []proto.Layer
	for _, l := range sn.Layers {
		ll = append(ll, proto.Layer{ID: l.ID, Kind: l.Kind, X: l.X, Y: l.Y, W: l.W, H: l.H, Z: l.Z, Top: l.Top, Bottom: l.Bottom,
			CW: l.CW, CH: l.CH, SX: l.SX, SY: l.SY})
	}
	if err := s.c.send(proto.Layers{T: proto.TLayers, V: v, PID: s.pid, Seq: s.seq, List: ll}); err != nil {
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
			if err := s.c.send(proto.Tile{T: proto.TTile, V: v, PID: s.pid, Layer: layer, X: t.X, Y: t.Y, W: t.W, H: t.H, Fmt: t.Fmt, Hash: t.Hash, Data: t.Data}); err != nil {
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
	if err := s.c.send(proto.Regions{T: proto.TRegions, V: v, PID: s.pid, Seq: s.seq, List: rr}); err != nil {
		return err
	}
	f := sn.Focus
	if err := s.c.send(proto.Focus{T: proto.TFocus, V: v, PID: s.pid, Seq: s.seq, ID: f.ID, Kind: f.Kind, Type: f.Type, Value: f.Value,
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
	s.log.Info("update", "v", v, "pid", s.pid, "seq", s.seq, "zoom", s.zoom, "url", truncate(sn.URL, 100), "h", sn.H, "layers", len(sn.Layers),
		"regions", len(rr), "tiles", nt, "bytes", nb, "ms", ms, "snapshot_ms", snapMs, "more", more)
	// the tab strip shows titles: the list goes out before `done`, so a step that ends at `done` has consumed it
	if sig := s.title + "\x00" + s.url; !more && sig != s.sent {
		s.sent = sig
		if err := s.sendTabs(); err != nil {
			return err
		}
	}
	return s.c.send(proto.Done{T: proto.TDone, V: v, PID: s.pid, Seq: s.seq, Ms: ms, Tiles: nt, Bytes: nb, More: more})
}

// onDialog runs on the browser's event goroutine: ask the phone and wait for its reply (60 s, then dismiss).
func (s *session) onDialog(view int, d browser.Dialog) browser.Answer {
	s.dmu.Lock()
	s.dnext++
	id := s.dnext
	ch := make(chan dialogAnswer, 1)
	s.dialogs[id] = ch
	s.dmu.Unlock()
	defer func() { s.dmu.Lock(); delete(s.dialogs, id); s.dmu.Unlock() }()
	if err := s.c.send(proto.Dialog{T: proto.TDialog, V: view, ID: id, Kind: d.Kind, Message: d.Message, Default: d.Default, URL: d.URL}); err != nil {
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

func (s *session) onDownload(view int, d browser.Download) {
	s.log.Info("download refused", "url", d.URL, "name", d.Name)
	s.c.send(proto.Download{T: proto.TDownload, V: view, URL: d.URL, Name: d.Name, Refused: true})
}
