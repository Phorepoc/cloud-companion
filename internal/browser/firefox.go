// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package browser

// Firefox is the cloud browser for the phone link, on the Firefox of the browser container (deploy/browser): no remote-control
// interface is turned on (navigator.webdriver stays false); everything goes through our extension, over the socket the web
// app's hub holds (internal/webui/browser.go). Each phone view is a tab in its own small popup window, kept behind the window
// the Browser page streams, laid out at the phone's width. The profile is the one Firefox profile: sign-ins are shared with
// the Browser page.
//
// Each paired device gets its own Firefox container (its own cookies: Cookies and SetCookies are that container's jar, in the shape
// the old engine exported), so sign-ins do not cross between devices or into the Browser page. Taps are Firefox's own input events
// (trusted), made through the extension's privileged half (deploy/browser/ext/phe-parent.js, a WebExtension experiment that
// mozilla.cfg allows); typing goes through the editor and is trusted too. A certificate Firefox refuses is put to the phone (a
// "cert" Dialog), and on OK Firefox's own temporary exception for that site is made and the page loads. Downloads of a device's
// container are reported to its phone (Handlers.Download) and saved to Files, Downloads. Without the privileged half (the
// extension is told by pClick returning false) a tap falls back to events dispatched inside the page (isTrusted false).

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Firefox is the Factory. The hub calls Attach when the extension connects, Detach when it goes, and Handle with each of its
// messages (Handle says whether the message was for the phone side).
type Firefox struct {
	MaxH int

	mu    sync.Mutex
	write func(context.Context, []byte) error
	conn  int // which connection write belongs to: a late Detach of an old one leaves a newer one alone
	next  int
	wait  map[int]chan ffReply
	tabs  map[int]*ffSession
}

type ffReply struct {
	V   json.RawMessage `json:"v"`
	Err string          `json:"error"`
}

func NewFirefox(maxH int) *Firefox {
	if maxH <= 0 {
		maxH = 8192
	}
	return &Firefox{MaxH: maxH, wait: map[int]chan ffReply{}, tabs: map[int]*ffSession{}}
}

var errNoFirefox = errors.New("the browser is starting or not reachable")

// Attach returns the connection's number, for its Detach.
func (f *Firefox) Attach(write func(context.Context, []byte) error) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drop()
	f.conn++
	f.write = write
	return f.conn
}

// Detach: the extension went away (Firefox restarted): calls waiting fail, and the phone views are gone with their windows.
func (f *Firefox) Detach(conn int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if conn == f.conn {
		f.drop()
	}
}

// drop: calls waiting on the connection fail, and the phone views were in its Firefox (with f.mu held).
func (f *Firefox) drop() {
	f.write = nil
	for id, ch := range f.wait {
		ch <- ffReply{Err: errNoFirefox.Error()}
		delete(f.wait, id)
	}
	for t, s := range f.tabs {
		s.gone = true
		delete(f.tabs, t)
	}
}

// Handle takes the extension's replies ({"t":"r"}) and questions about phone tabs ({"t":"pask"}); false: not ours.
func (f *Firefox) Handle(m []byte) bool {
	head := m[:min(len(m), 16)]
	switch {
	case bytes.Contains(head, []byte(`"t":"r"`)):
		var r struct {
			R int `json:"r"`
			ffReply
		}
		if json.Unmarshal(m, &r) == nil {
			f.mu.Lock()
			ch := f.wait[r.R]
			delete(f.wait, r.R)
			f.mu.Unlock()
			if ch != nil {
				ch <- r.ffReply
			}
		}
		return true
	case bytes.Contains(head, []byte(`"t":"pdl"`)):
		var q struct {
			Tab  int    `json:"tab"`
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if json.Unmarshal(m, &q) == nil {
			f.mu.Lock()
			s := f.tabs[q.Tab]
			f.mu.Unlock()
			if s != nil {
				s.hmu.Lock()
				h := s.h
				s.hmu.Unlock()
				if h.Download != nil {
					go h.Download(Download{URL: q.URL, Name: q.Name})
				}
			}
		}
		return true
	case bytes.Contains(head, []byte(`"t":"pask"`)):
		var q struct {
			ID   string `json:"id"`
			Tab  int    `json:"tab"`
			Kind string `json:"kind"`
			Msg  string `json:"msg"`
			URL  string `json:"url"`
		}
		if json.Unmarshal(m, &q) == nil {
			go func() {
				a := f.Ask(q.Tab, Dialog{Kind: q.Kind, Message: q.Msg, URL: q.URL})
				_, _ = f.call(context.Background(), 10*time.Second, map[string]any{"c": "pAnswer", "id": q.ID, "ok": a.OK, "text": a.Text, "pass": a.Pass})
			}()
		}
		return true
	}
	return false
}

// Ask puts a page's question (a JavaScript dialog, HTTP authentication) to the phone showing that tab; nobody there: cancel.
func (f *Firefox) Ask(tab int, d Dialog) Answer {
	f.mu.Lock()
	s := f.tabs[tab]
	f.mu.Unlock()
	if s == nil {
		return Answer{}
	}
	s.hmu.Lock()
	h := s.h
	s.hmu.Unlock()
	if h.Dialog == nil {
		return Answer{}
	}
	return h.Dialog(d)
}

func (f *Firefox) call(ctx context.Context, d time.Duration, cmd map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	w := f.write
	if w == nil {
		f.mu.Unlock()
		return nil, errNoFirefox
	}
	f.next++
	id := f.next
	ch := make(chan ffReply, 1)
	f.wait[id] = ch
	f.mu.Unlock()
	cmd["r"] = id
	b, _ := json.Marshal(cmd)
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	drop := func() { f.mu.Lock(); delete(f.wait, id); f.mu.Unlock() }
	if err := w(ctx, b); err != nil {
		drop()
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Err != "" {
			return nil, errors.New(r.Err)
		}
		return r.V, nil
	case <-ctx.Done():
		drop()
		return nil, fmt.Errorf("%v: %w", cmd["c"], ctx.Err())
	}
}

func (f *Firefox) New(ctx context.Context) (Session, error) { return f.NewFor(ctx, "") }

// NewFor opens a view for a device: its tabs share the device's own container (cookies).
func (f *Firefox) NewFor(ctx context.Context, dev string) (Session, error) {
	v, err := f.call(ctx, 20*time.Second, map[string]any{"c": "pOpen", "w": 360, "h": viewH, "dev": dev})
	if err != nil {
		return nil, err
	}
	var tab int
	if err := json.Unmarshal(v, &tab); err != nil || tab == 0 {
		return nil, fmt.Errorf("opening a tab: %s", v)
	}
	s := &ffSession{f: f, tab: tab, dev: dev, maxH: f.MaxH, width: 360, height: viewH}
	f.mu.Lock()
	f.tabs[tab] = s
	f.mu.Unlock()
	return s, nil
}

func (f *Firefox) Close() {}

const viewH = 640

type ffSession struct {
	f      *Firefox
	tab    int
	dev    string // the device: its container
	gone   bool   // its Firefox went away (f.mu): the next command opens the view again, on the same page
	closed bool
	url    string
	maxH   int

	hmu sync.Mutex
	h   Handlers

	width, height int    // the window, device px at scale 100
	site          string // "desktop", "mobile", ""
	opened        bool
	pct           int
	sy            int
	cert          *certErr // a main-frame load failed on a certificate (from the last pNav / pSettle)
}

type certErr struct{ URL, Text string }

// noteReply keeps what pNav and pSettle say about a refused certificate.
func (s *ffSession) noteReply(v json.RawMessage) {
	var r struct {
		Cert *struct{ URL, Text string }
	}
	if len(v) > 0 && v[0] == '{' && json.Unmarshal(v, &r) == nil && r.Cert != nil {
		s.cert = &certErr{r.Cert.URL, r.Cert.Text}
	}
}

// certFlow: a main-frame load failed on a certificate. Ask the phone (a "cert" Dialog, as the old engine did); on OK Firefox makes
// its temporary exception for the site and the page loads again.
func (s *ffSession) certFlow(ctx context.Context) error {
	ce := s.cert
	s.cert = nil
	s.hmu.Lock()
	h := s.h
	s.hmu.Unlock()
	if ce == nil || h.Dialog == nil || ce.URL == "" {
		return nil
	}
	if !h.Dialog(Dialog{Kind: "cert", Message: ce.Text + " " + ce.URL, URL: ce.URL}).OK {
		return nil
	}
	if _, err := s.do(ctx, 10*time.Second, "pCert", nil); err != nil {
		return err
	}
	if _, err := s.do(ctx, 30*time.Second, "pNav", map[string]any{"action": "reload"}); err != nil {
		return err
	}
	s.cert = nil
	return s.settle(ctx)
}

// alive opens the view again after Firefox restarted: same size, zoom and site version, the last page.
func (s *ffSession) alive(ctx context.Context) error {
	s.f.mu.Lock()
	gone := s.gone
	s.f.mu.Unlock()
	if !gone {
		return nil
	}
	if s.closed {
		return errNoFirefox
	}
	v, err := s.f.call(ctx, 20*time.Second, map[string]any{"c": "pOpen", "w": 360, "h": viewH, "dev": s.dev})
	if err != nil {
		return err
	}
	var tab int
	if err := json.Unmarshal(v, &tab); err != nil || tab == 0 {
		return fmt.Errorf("opening a tab: %s", v)
	}
	s.f.mu.Lock()
	s.tab, s.gone = tab, false
	s.f.tabs[tab] = s
	s.f.mu.Unlock()
	if !s.opened {
		return nil
	}
	if _, err := s.do(ctx, 10*time.Second, "pSite", map[string]any{"mobile": s.mobile(), "reload": false}); err != nil {
		return err
	}
	if err := s.applyView(ctx); err != nil {
		return err
	}
	if s.url != "" {
		if _, err := s.do(ctx, 30*time.Second, "pNav", map[string]any{"url": s.url}); err != nil {
			return err
		}
		return s.settle(ctx)
	}
	return nil
}

func (s *ffSession) do(ctx context.Context, d time.Duration, c string, args map[string]any) (json.RawMessage, error) {
	if err := s.alive(ctx); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	args["c"], args["tab"] = c, s.tab
	return s.f.call(ctx, d, args)
}

// eval runs js in the tab's main frame, in our content-script world: it shares the DOM, not the page's scripts, so what we keep
// there (window.__phe) the page never sees. A promise is waited for.
func (s *ffSession) eval(ctx context.Context, d time.Duration, js string, out any) error {
	v, err := s.do(ctx, d, "pEval", map[string]any{"code": js})
	if err != nil {
		return err
	}
	if out == nil || len(v) == 0 || string(v) == "null" {
		return nil
	}
	return json.Unmarshal(v, out)
}

func (s *ffSession) Close() {
	s.f.mu.Lock()
	delete(s.f.tabs, s.tab)
	s.f.mu.Unlock()
	s.closed = true
	_, _ = s.f.call(context.Background(), 5*time.Second, map[string]any{"c": "pClose", "tab": s.tab})
}

func (s *ffSession) SetHandlers(h Handlers) { s.hmu.Lock(); s.h = h; s.hmu.Unlock() }

// mobile: the phone version of sites, as the old engine chose it (asked for, or a narrow window).
func (s *ffSession) mobile() bool {
	switch s.site {
	case "desktop":
		return false
	case "mobile":
		return true
	}
	return s.height <= 0 || s.width < 600
}

func (s *ffSession) Open(ctx context.Context, url string, width int, dpr float64) error {
	if width > 0 {
		s.width = width
	}
	s.opened = true
	if _, err := s.do(ctx, 10*time.Second, "pSite", map[string]any{"mobile": s.mobile(), "reload": false}); err != nil {
		return err
	}
	if err := s.applyView(ctx); err != nil {
		return err
	}
	return s.Navigate(ctx, url, "")
}

// settle: nothing in flight for 60 ms, the page loaded, two animation frames (capped at 15 s), as the old engine waited.
func (s *ffSession) settle(ctx context.Context) error {
	v, err := s.do(ctx, 20*time.Second, "pSettle", nil)
	if err != nil {
		return err
	}
	s.noteReply(v)
	return nil
}

func (s *ffSession) Navigate(ctx context.Context, url, action string) error {
	if url != "" {
		s.url = url
	}
	v, err := s.do(ctx, 30*time.Second, "pNav", map[string]any{"url": url, "action": action})
	if err != nil {
		return err
	}
	s.noteReply(v)
	s.sy = 0
	if err := s.settle(ctx); err != nil {
		return err
	}
	return s.certFlow(ctx)
}

// Cookies exports the device's container jar (JSON, the shape the old engine exported); SetCookies imports one.
func (s *ffSession) Cookies(ctx context.Context) ([]byte, error) {
	v, err := s.do(ctx, 10*time.Second, "pCookies", nil)
	if err != nil {
		return nil, err
	}
	return []byte(v), nil
}

func (s *ffSession) SetCookies(ctx context.Context, data []byte) error {
	var cs json.RawMessage
	if json.Unmarshal(data, &cs) != nil || len(cs) == 0 {
		return nil
	}
	_, err := s.do(ctx, 20*time.Second, "pSetCookies", map[string]any{"cookies": cs})
	return err
}

// click taps at viewport point (x, y): Firefox's own (trusted) input events, or, without the privileged path, events in the page.
func (s *ffSession) click(ctx context.Context, x, y int) error {
	v, err := s.do(ctx, 10*time.Second, "pClick", map[string]any{"x": x, "y": y})
	if err != nil {
		return err
	}
	if string(v) == "true" {
		return nil
	}
	return s.eval(ctx, 10*time.Second, fmt.Sprintf(clickJS, x, y), nil)
}

// Tap clicks (a trusted tap) at (x, y). sy < 0: viewport coordinates (fixed layers). Otherwise y is a document coordinate: scroll to sy (or
// centre the target if it would be off screen), then click there.
func (s *ffSession) Tap(ctx context.Context, x, y, sy int) error {
	vy := y
	if sy >= 0 {
		_, ch := s.cssView()
		for try := 0; try < 2; try++ {
			if err := s.ScrollTo(ctx, sy); err != nil {
				return err
			}
			var act float64
			if err := s.eval(ctx, 5*time.Second, `window.scrollY`, &act); err != nil {
				return err
			}
			vy = y - int(act+0.5)
			if vy >= 0 && vy < ch {
				break
			}
			sy = max(0, y-ch/2)
		}
	}
	if err := s.click(ctx, x, vy); err != nil {
		return err
	}
	if err := s.settle(ctx); err != nil {
		return err
	}
	return s.certFlow(ctx)
}

// clickJS (the fallback of click) clicks what is at viewport point (x, y) as a finger would: pointer and mouse events, focus, then click (whose
// default action follows links, ticks boxes, submits forms).
const clickJS = `(()=>{const X=%d,Y=%d;let e=document.elementFromPoint(X,Y);if(!e)return false;
 const o={bubbles:true,cancelable:true,composed:true,clientX:X,clientY:Y,screenX:X,screenY:Y,view:window,button:0};
 const p=Object.assign({pointerId:1,pointerType:'touch',isPrimary:true},o);
 e.dispatchEvent(new PointerEvent('pointerdown',Object.assign({buttons:1},p)));e.dispatchEvent(new MouseEvent('mousedown',Object.assign({buttons:1},o)));
 const f=e.closest('input,textarea,select,button,a[href],[tabindex],[contenteditable=""],[contenteditable=true]');
 if(f&&f.focus)f.focus();
 e.dispatchEvent(new PointerEvent('pointerup',p));e.dispatchEvent(new MouseEvent('mouseup',o));
 e.dispatchEvent(new MouseEvent('click',Object.assign({detail:1},o)));return true})()`

// editJS: helpers for typing into the focused element through the editor (execCommand), whose input events are the browser's
// own (isTrusted), with key events around each key for the page's listeners.
const editJS = `const A=()=>{let a=document.activeElement;while(a&&a.shadowRoot&&a.shadowRoot.activeElement)a=a.shadowRoot.activeElement;return a};
 const K=(type,key,code)=>{const a=A()||document.body;return a.dispatchEvent(new KeyboardEvent(type,{key,code:code||key,bubbles:true,cancelable:true,composed:true}))};
 const ins=t=>{if(K('keydown',t.length===1?t:'Unidentified')){document.execCommand('insertText',false,t)}K('keyup',t.length===1?t:'Unidentified')};
 const enter=()=>{const a=A();if(!K('keydown','Enter')){K('keyup','Enter');return}
  K('keypress','Enter');
  if(a&&a.tagName==='INPUT'&&a.form){const f=a.form;if(f.requestSubmit)f.requestSubmit();else f.submit()}
  else if(a&&(a.tagName==='TEXTAREA'||a.isContentEditable))document.execCommand('insertText',false,'\n');
  else if(a&&(a.tagName==='BUTTON'||a.tagName==='A'))a.click();
  K('keyup','Enter')};`

func (s *ffSession) Text(ctx context.Context, id int, text string, submit, replace bool) error {
	js := fmt.Sprintf(`(()=>{%s
 const id=%d;if(id>0){const e=(window.__phe&&window.__phe.els.get(id)?.deref());if(!e)return false;
  e.scrollIntoView({block:'center'});e.focus();if(%v){if(e.select)e.select();else document.execCommand('selectAll')}}
 const t=%s;if(t)ins(t);if(%v)enter();return true})()`, editJS, id, replace, jsString(text), submit)
	var ok bool
	if err := s.eval(ctx, 10*time.Second, js, &ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no such region %d", id)
	}
	s.sy = 0
	return s.settle(ctx)
}

var keyJS = map[string]string{
	"Backspace":  `if(K('keydown','Backspace'))document.execCommand('delete');K('keyup','Backspace')`,
	"Delete":     `if(K('keydown','Delete'))document.execCommand('forwardDelete');K('keyup','Delete')`,
	"Enter":      `enter()`,
	"Escape":     `K('keydown','Escape');K('keyup','Escape')`,
	"ArrowLeft":  `if(K('keydown','ArrowLeft'))getSelection().modify('move','backward','character');K('keyup','ArrowLeft')`,
	"ArrowRight": `if(K('keydown','ArrowRight'))getSelection().modify('move','forward','character');K('keyup','ArrowRight')`,
	"ArrowUp":    `if(K('keydown','ArrowUp'))getSelection().modify('move','backward','line');K('keyup','ArrowUp')`,
	"ArrowDown":  `if(K('keydown','ArrowDown'))getSelection().modify('move','forward','line');K('keyup','ArrowDown')`,
	"Tab": `if(K('keydown','Tab')){const l=[...document.querySelectorAll('a[href],button,input:not([type=hidden]),select,textarea,[tabindex]')].filter(e=>e.tabIndex>=0&&!e.disabled&&e.getClientRects().length);
  const i=l.indexOf(A());const n=l[(i+1)%%l.length];if(n)n.focus()}K('keyup','Tab')`,
}

// Key presses one key in the focused element: a named key, or one character the person typed.
func (s *ffSession) Key(ctx context.Context, name string) error {
	body, ok := keyJS[name]
	if ok {
		body = strings.ReplaceAll(body, "%%", "%")
	} else if utf8.RuneCountInString(name) == 1 && name >= " " {
		body = "ins(" + jsString(name) + ")"
	} else {
		return fmt.Errorf("unknown key %q", name)
	}
	if err := s.eval(ctx, 10*time.Second, "(()=>{"+editJS+body+";return true})()", nil); err != nil {
		return err
	}
	if utf8.RuneCountInString(name) == 1 || name == "Backspace" || name == "Delete" || strings.HasPrefix(name, "Arrow") {
		return nil // editing: the page is shown as it is now
	}
	return s.settle(ctx)
}

func (s *ffSession) Select(ctx context.Context, id, index int) error {
	js := fmt.Sprintf(`(()=>{const e=(window.__phe&&window.__phe.els.get(%d)?.deref());if(!e||e.tagName!=='SELECT'||!e.options[%d])return false;
e.selectedIndex=%d;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));return true})()`, id, index, index)
	var ok bool
	if err := s.eval(ctx, 10*time.Second, js, &ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no such select/option %d/%d", id, index)
	}
	s.sy = 0
	return s.settle(ctx)
}

func (s *ffSession) ScrollTo(ctx context.Context, y int) error {
	s.sy = y
	return s.eval(ctx, 5*time.Second, fmt.Sprintf(`window.scrollTo(0,%d);true`, y), nil)
}

// ---- snapshot ----

func (s *ffSession) shot(ctx context.Context, x, y, w, h int) (*image.RGBA, error) {
	out := image.NewRGBA(image.Rect(0, 0, scaleInt(w, s.scalePct()), scaleInt(h, s.scalePct())))
	// in bands: one capture of a very long page is too big for Firefox to draw at once
	band := 4096 * 100 / s.scalePct()
	for top := 0; top < h; top += band {
		bh := min(band, h-top)
		v, err := s.do(ctx, 40*time.Second, "pShot", map[string]any{"x": x, "y": y + top, "w": w, "h": bh, "scale": float64(s.scalePct()) / 100})
		if err != nil {
			return nil, err
		}
		var url string
		if err := json.Unmarshal(v, &url); err != nil {
			return nil, err
		}
		i := strings.IndexByte(url, ',')
		if i < 0 {
			return nil, errors.New("capture: not an image")
		}
		raw, err := base64.StdEncoding.DecodeString(url[i+1:])
		if err != nil {
			return nil, err
		}
		im, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		at := scaleInt(top, s.scalePct())
		draw.Draw(out, image.Rect(0, at, out.Bounds().Dx(), at+im.Bounds().Dy()), im, im.Bounds().Min, draw.Src)
	}
	return out, nil
}

const quickRows = 768

func (s *ffSession) Snapshot(ctx context.Context, quick bool) (*Snapshot, error) {
	keepSY := s.sy
	ch := s.cssViewH()
	if !quick {
		var done bool
		if err := s.eval(ctx, 40*time.Second, fmt.Sprintf(lazyJS, s.maxH*100/s.scalePct(), ch, ch), &done); err != nil {
			return nil, fmt.Errorf("lazy pass: %w", err)
		}
	} else {
		_ = s.eval(ctx, 5*time.Second, `window.scrollTo(0,0);true`, nil)
	}
	var js string
	if err := s.eval(ctx, 15*time.Second, extractJS, &js); err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	var info jsInfo
	if err := json.Unmarshal([]byte(js), &info); err != nil {
		return nil, err
	}
	sn := &Snapshot{URL: info.URL, Title: info.Title, W: s.width}
	if strings.HasPrefix(info.URL, "http") {
		s.url = info.URL
	}
	sn.Focus = Focus{ID: info.Focus.ID, Kind: info.Focus.Kind, Type: info.Focus.Type, Value: info.Focus.Value,
		Start: info.Focus.Start, End: info.Focus.End, InputMode: info.Focus.IM, EnterKey: info.Focus.Enter}
	if sn.Focus.Kind == "" {
		sn.Focus.Kind = "none"
	}
	if v, err := s.do(ctx, 5*time.Second, "pHist", nil); err == nil {
		var hb struct{ Back, Fwd bool }
		if json.Unmarshal(v, &hb) == nil {
			sn.Back, sn.Fwd = hb.Back, hb.Fwd
		}
	}
	h := max(info.H, ch)
	if lim := s.maxH * 100 / s.scalePct(); h > lim {
		h, sn.Truncated = lim, true
	}
	sn.H = h
	capH := h
	if qr := (quickRows*100 + s.scalePct() - 1) / s.scalePct(); quick && h > qr {
		capH, sn.Partial = qr, true
	}
	for _, l := range info.Layers {
		var img *image.RGBA
		var err error
		cw, chh := 0, 0
		if l.Kind == "scroll" {
			img, err = s.shotScroll(ctx, l)
			if err == nil {
				cw, chh = l.CW, l.CH
			}
		} else {
			img, err = s.shot(ctx, l.X, l.Y, l.W, l.H)
		}
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", l.ID, err)
		}
		sn.Layers = append(sn.Layers, Layer{ID: l.ID, Kind: l.Kind, X: l.X, Y: l.Y, W: l.W, H: l.H, Z: l.Z, Top: l.Top, Bottom: l.Bottom,
			CW: cw, CH: chh, SX: l.SX, SY: l.SY, Img: img})
	}
	if len(info.Layers) > 0 {
		_ = s.eval(ctx, 5*time.Second, hideJS, nil)
	}
	img, err := s.shot(ctx, 0, 0, s.width*100/s.scalePct(), capH)
	if len(info.Layers) > 0 {
		_ = s.eval(ctx, 5*time.Second, unhideJS, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("page: %w", err)
	}
	sn.Page = img
	for _, r := range info.Regions {
		if r.Layer == 0 && r.Y >= h {
			continue
		}
		reg := Region{ID: r.ID, Layer: r.Layer, X: r.X, Y: r.Y, W: r.W, H: r.H, Kind: r.Kind,
			Href: r.Href, Type: r.Type, Name: r.Name, Placeholder: r.Ph, Value: r.Value, Label: r.Label}
		for _, o := range r.Opts {
			reg.Opts = append(reg.Opts, Opt{Label: o.L, Sel: o.S})
		}
		sn.Regions = append(sn.Regions, reg)
	}
	if p := s.scalePct(); p != 100 { // geometry goes out in device pixels at the render scale
		sn.H = scaleInt(sn.H, p)
		for i := range sn.Layers {
			l := &sn.Layers[i]
			l.X, l.Y, l.W, l.H = scaleInt(l.X, p), scaleInt(l.Y, p), scaleInt(l.W, p), scaleInt(l.H, p)
			l.CW, l.CH, l.SX, l.SY = scaleInt(l.CW, p), scaleInt(l.CH, p), scaleInt(l.SX, p), scaleInt(l.SY, p)
			if l.Top != nil {
				v := scaleInt(*l.Top, p)
				l.Top = &v
			}
			if l.Bottom != nil {
				v := scaleInt(*l.Bottom, p)
				l.Bottom = &v
			}
		}
		for i := range sn.Regions {
			r := &sn.Regions[i]
			r.X, r.Y, r.W, r.H = scaleInt(r.X, p), scaleInt(r.Y, p), scaleInt(r.W, p), scaleInt(r.H, p)
		}
	}
	if keepSY > 0 {
		_ = s.ScrollTo(ctx, keepSY)
	}
	return sn, nil
}

// shotScroll renders the whole content of an inner scroll box (expanded for the shot, then put back); falls back to the box.
func (s *ffSession) shotScroll(ctx context.Context, l jsLayer) (*image.RGBA, error) {
	js := fmt.Sprintf(`(()=>{const e=(window.__phe&&window.__phe.layer&&window.__phe.layer.get(%d));if(!e)return null;
 e.__pheStyle=e.getAttribute('style');
 e.style.setProperty('overflow','visible','important');
 if(e.scrollHeight>e.clientHeight){e.style.setProperty('height','auto','important');e.style.setProperty('max-height','none','important');}
 if(e.scrollWidth>e.clientWidth){e.style.setProperty('width',e.scrollWidth+'px','important');e.style.setProperty('max-width','none','important');}
 const r=e.getBoundingClientRect();return [Math.round(r.left),Math.round(r.top+scrollY),Math.round(r.width),Math.round(r.height)];})()`, l.ID)
	var box []int
	if err := s.eval(ctx, 5*time.Second, js, &box); err != nil || len(box) != 4 {
		return s.shot(ctx, l.X, l.Y, l.W, l.H)
	}
	restore := fmt.Sprintf(`(()=>{const e=(window.__phe&&window.__phe.layer&&window.__phe.layer.get(%d));if(!e)return false;
 if(e.__pheStyle===null)e.removeAttribute('style');else e.setAttribute('style',e.__pheStyle);return true})()`, l.ID)
	defer s.eval(ctx, 5*time.Second, restore, nil)
	w, h := min(box[2], l.CW), min(box[3], l.CH)
	if w < l.W || h < l.H {
		return s.shot(ctx, l.X, l.Y, l.W, l.H)
	}
	return s.shot(ctx, box[0], box[1], w, h)
}

func (s *ffSession) TapScroll(ctx context.Context, layer, x, y int) error {
	js := fmt.Sprintf(`(()=>{const e=(window.__phe&&window.__phe.layer&&window.__phe.layer.get(%d));if(!e)return null;
 const X=%d,Y=%d;
 e.scrollTo(Math.max(0,Math.min(X-e.clientWidth/2,e.scrollWidth-e.clientWidth)),Math.max(0,Math.min(Y-e.clientHeight/2,e.scrollHeight-e.clientHeight)));
 let b=e.getBoundingClientRect();
 if(b.top<0||b.bottom>innerHeight){window.scrollBy(0,b.top-40);b=e.getBoundingClientRect();}
 return [Math.round(b.left+(X-e.scrollLeft)),Math.round(b.top+(Y-e.scrollTop))];})()`, layer, x, y)
	var pt []int
	if err := s.eval(ctx, 5*time.Second, js, &pt); err != nil || len(pt) != 2 {
		return fmt.Errorf("no scroll layer %d", layer)
	}
	if err := s.click(ctx, pt[0], pt[1]); err != nil {
		return err
	}
	s.sy = 0
	if err := s.settle(ctx); err != nil {
		return err
	}
	return s.certFlow(ctx)
}

// ---- size and scale ----

func (s *ffSession) scalePct() int {
	if s.pct <= 0 {
		return 100
	}
	return s.pct
}

// cssView is the layout viewport in css px at the current scale.
func (s *ffSession) cssView() (int, int) {
	p := s.scalePct()
	w, h := s.width, s.height
	if w <= 0 {
		w = 360
	}
	if h <= 0 {
		h = viewH
	}
	return w * 100 / p, h * 100 / p
}

func (s *ffSession) cssViewH() int { _, h := s.cssView(); return h }

// applyView: the popup's page area is width x height device px; Firefox's zoom (this tab only) makes the layout width/scale css px.
func (s *ffSession) applyView(ctx context.Context) error {
	h := s.height
	if h <= 0 {
		h = viewH
	}
	_, err := s.do(ctx, 10*time.Second, "pView", map[string]any{"w": s.width, "h": h, "zoom": float64(s.scalePct()) / 100})
	return err
}

func (s *ffSession) SetScale(ctx context.Context, pct int) error {
	s.pct = pct
	if err := s.applyView(ctx); err != nil {
		return err
	}
	return s.settle(ctx)
}

func (s *ffSession) SetViewport(ctx context.Context, width, height int) error {
	if width == s.width && height == s.height {
		return nil
	}
	was := s.mobile()
	s.width, s.height = width, height
	if !s.opened {
		return nil
	}
	if now := s.mobile(); now != was { // pages loaded from now on get the version that fits the window
		if _, err := s.do(ctx, 10*time.Second, "pSite", map[string]any{"mobile": now, "reload": false}); err != nil {
			return err
		}
	}
	if err := s.applyView(ctx); err != nil {
		return err
	}
	return s.settle(ctx)
}

func (s *ffSession) SetSite(ctx context.Context, site string) error {
	was := s.mobile()
	s.site = site
	if !s.opened || s.mobile() == was {
		return nil
	}
	if _, err := s.do(ctx, 30*time.Second, "pSite", map[string]any{"mobile": s.mobile(), "reload": true}); err != nil {
		return err
	}
	return s.settle(ctx)
}

func scaleInt(v, pct int) int { return (v*pct + 50) / 100 }

func jsString(s string) string { b, _ := json.Marshal(s); return string(b) }
