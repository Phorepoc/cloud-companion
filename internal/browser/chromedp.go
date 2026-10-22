// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/cdproto/target"
	"image"
	"image/draw"
	"image/png"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

const mobileUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/156.0.0.0 Mobile Safari/537.36"

const viewH = 640

// CDP is the chromedp-based Factory: one headless Chromium; each Session is a tab in its own browser context
// (isolated cookies); a small pool of warm tabs is kept ready.
type CDP struct {
	rootCtx context.Context // holds the browser connection; tabs derive from it
	cancel  context.CancelFunc
	MaxH    int
	pool    chan *tab
	mu      sync.Mutex
	boxes   map[target.ID]*handlerBox // tab target id -> its handlers (download events are browser-level)
}

type tab struct {
	ctx    context.Context
	cancel context.CancelFunc
	ns     *netState
	box    *handlerBox
}

func NewCDP(chromePath string, maxH, poolSize int) *CDP {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
	)
	if chromePath != "" {
		opts = append(opts, chromedp.ExecPath(chromePath))
	}
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	if maxH <= 0 {
		maxH = 8192
	}
	rootCtx, rootCancel := chromedp.NewContext(ctx)
	chromedp.Run(rootCtx) // start the browser process
	c := &CDP{rootCtx: rootCtx, cancel: func() { rootCancel(); cancel() }, MaxH: maxH, pool: make(chan *tab, max(poolSize, 1)), boxes: map[target.ID]*handlerBox{}}
	chromedp.ListenBrowser(rootCtx, func(ev any) {
		if e, ok := ev.(*browser.EventDownloadWillBegin); ok {
			c.mu.Lock()
			b := c.boxes[target.ID(e.FrameID)]
			c.mu.Unlock()
			if b != nil {
				if h := b.get(); h.Download != nil {
					h.Download(Download{URL: e.URL, Name: e.SuggestedFilename})
				}
			}
		}
	})
	for i := 0; i < poolSize; i++ {
		c.refill()
	}
	return c
}

type handlerBox struct {
	mu sync.Mutex
	h  Handlers
}

func (b *handlerBox) get() Handlers { b.mu.Lock(); defer b.mu.Unlock(); return b.h }

func (c *CDP) newTab() (*tab, error) {
	tctx, cancel := chromedp.NewContext(c.rootCtx, chromedp.WithNewBrowserContext())
	ns := newNetState()
	box := &handlerBox{}
	chromedp.ListenTarget(tctx, func(ev any) {
		ns.handle(ev)
		if d, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			go func() { // the page is blocked until we answer; never block the event loop
				h := box.get()
				accept, text := false, ""
				if h.Dialog != nil {
					accept, text = h.Dialog(Dialog{Kind: string(d.Type), Message: d.Message, Default: d.DefaultPrompt, URL: d.URL})
				}
				chromedp.Run(tctx, page.HandleJavaScriptDialog(accept).WithPromptText(text))
			}()
		}
	})
	if err := chromedp.Run(tctx, network.Enable(), page.Enable()); err != nil { // also starts the tab
		cancel()
		return nil, err
	}
	fc := chromedp.FromContext(tctx)
	// downloads are refused (and reported through Handlers.Download)
	err := chromedp.Run(tctx, chromedp.ActionFunc(func(x context.Context) error {
		return browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorDeny).WithBrowserContextID(fc.BrowserContextID).
			WithEventsEnabled(true).Do(cdp.WithExecutor(x, fc.Browser))
	}))
	if err != nil {
		cancel()
		return nil, err
	}
	tid := fc.Target.TargetID
	c.mu.Lock()
	c.boxes[tid] = box
	c.mu.Unlock()
	return &tab{tctx, func() {
		c.mu.Lock()
		delete(c.boxes, tid)
		c.mu.Unlock()
		cancel()
	}, ns, box}, nil
}

// refill warms one more tab in the background (dropped if the pool is already full).
func (c *CDP) refill() {
	go func() {
		t, err := c.newTab()
		if err != nil {
			return
		}
		select {
		case c.pool <- t:
		default:
			t.cancel()
		}
	}()
}

func (c *CDP) New(ctx context.Context) (Session, error) {
	var t *tab
	select {
	case t = <-c.pool:
	default:
		var err error
		if t, err = c.newTab(); err != nil {
			return nil, err
		}
	}
	c.refill()
	return &cdpSession{ctx: t.ctx, cancel: t.cancel, ns: t.ns, box: t.box, maxH: c.MaxH}, nil
}

func (c *CDP) Close() { c.cancel() }

// netState counts in-flight requests so that "settled" is event-driven: no requests in flight and a short quiet time.
type netState struct {
	mu       sync.Mutex
	inflight map[network.RequestID]struct{}
	last     time.Time
	ch       chan struct{}
}

func newNetState() *netState {
	return &netState{inflight: map[network.RequestID]struct{}{}, last: time.Now(), ch: make(chan struct{}, 1)}
}

func (n *netState) touch() {
	n.mu.Lock()
	n.last = time.Now()
	n.mu.Unlock()
}

func (n *netState) handle(ev any) {
	n.mu.Lock()
	switch e := ev.(type) {
	case *network.EventRequestWillBeSent:
		if e.Type == network.ResourceTypeWebSocket || e.Type == network.ResourceTypeEventSource || e.Type == network.ResourceTypePing {
			n.mu.Unlock()
			return
		}
		n.inflight[e.RequestID] = struct{}{}
	case *network.EventLoadingFinished:
		delete(n.inflight, e.RequestID)
	case *network.EventLoadingFailed:
		delete(n.inflight, e.RequestID)
	default:
		n.mu.Unlock()
		return
	}
	n.last = time.Now()
	n.mu.Unlock()
	select {
	case n.ch <- struct{}{}:
	default:
	}
}

const quiet = 60 * time.Millisecond

type cdpSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	ns     *netState
	box    *handlerBox
	maxH   int
	width  int
	dpr    float64
	sy     int
}

func (s *cdpSession) Close() { s.cancel() }

func (s *cdpSession) run(parent context.Context, d time.Duration, a ...chromedp.Action) error {
	ctx, cancel := context.WithTimeout(s.ctx, d)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	return chromedp.Run(ctx, a...)
}

func (s *cdpSession) Open(ctx context.Context, url string, width int, dpr float64) error {
	if width <= 0 {
		width = 360
	}
	s.width, s.dpr = width, 1 // v1: tiles are in CSS px, dpr is fixed at 1
	s.ns.touch()
	err := s.run(ctx, 30*time.Second,
		emulation.SetUserAgentOverride(mobileUA),
		emulation.SetDeviceMetricsOverride(int64(width), viewH, 1, true),
		chromedp.Navigate(url),
	)
	if err != nil {
		return err
	}
	s.sy = 0
	return s.settle(ctx)
}

const frameJS = `new Promise(r=>{const f=()=>requestAnimationFrame(()=>requestAnimationFrame(r));
 if(document.readyState==='complete')f();else addEventListener('load',f,{once:true});setTimeout(r,1500)})`

// settle waits until no request is in flight and none finished in the last 60 ms, the page has loaded, and two
// animation frames have run. Capped at 15 s.
func (s *cdpSession) settle(ctx context.Context) error {
	s.ns.touch()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s.ns.mu.Lock()
		busy, last := len(s.ns.inflight) > 0, s.ns.last
		s.ns.mu.Unlock()
		wait := time.Until(deadline)
		if !busy {
			if w := quiet - time.Since(last); w <= 0 {
				break
			} else {
				wait = w
			}
		}
		select {
		case <-s.ns.ch:
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.run(ctx, 5*time.Second, chromedp.Evaluate(frameJS, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
	return nil
}

// Cookies exports the tab's cookie jar as JSON; SetCookies imports one.
func (s *cdpSession) Cookies(ctx context.Context) ([]byte, error) {
	var cs []*network.Cookie
	err := s.run(ctx, 5*time.Second, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		cs, err = storage.GetCookies().WithBrowserContextID(s.bcid()).Do(s.browserCtx(c))
		return err
	}))
	if err != nil {
		return nil, err
	}
	return json.Marshal(cs)
}

func (s *cdpSession) SetCookies(ctx context.Context, data []byte) error {
	var cs []*network.Cookie
	if err := json.Unmarshal(data, &cs); err != nil || len(cs) == 0 {
		return err
	}
	var ps []*network.CookieParam
	for _, c := range cs {
		p := &network.CookieParam{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure,
			HTTPOnly: c.HTTPOnly, SameSite: c.SameSite}
		if c.Expires > 0 && !c.Session {
			t := cdp.TimeSinceEpoch(time.Unix(int64(c.Expires), 0))
			p.Expires = &t
		}
		ps = append(ps, p)
	}
	return s.run(ctx, 5*time.Second, chromedp.ActionFunc(func(c context.Context) error {
		return storage.SetCookies(ps).WithBrowserContextID(s.bcid()).Do(s.browserCtx(c))
	}))
}

func (s *cdpSession) Navigate(ctx context.Context, url, action string) error {
	var a chromedp.Action
	switch action {
	case "back":
		a = chromedp.NavigateBack()
	case "forward":
		a = chromedp.NavigateForward()
	case "reload":
		a = chromedp.Reload()
	default:
		a = chromedp.Navigate(url)
	}
	if err := s.run(ctx, 30*time.Second, a); err != nil {
		return err
	}
	s.sy = 0
	return s.settle(ctx)
}

// Tap clicks at (x, y). sy < 0: x,y are viewport coordinates (fixed layers). Otherwise y is a document
// coordinate: scroll to sy (or to centre the target if it would be off screen), then click.
func (s *cdpSession) Tap(ctx context.Context, x, y, sy int) error {
	vy := y
	if sy >= 0 {
		for try := 0; try < 2; try++ {
			if err := s.ScrollTo(ctx, sy); err != nil {
				return err
			}
			var act float64
			if err := s.run(ctx, 5*time.Second, chromedp.Evaluate(`window.scrollY`, &act)); err != nil {
				return err
			}
			vy = y - int(act+0.5)
			if vy >= 0 && vy < viewH {
				break
			}
			sy = max(0, y-viewH/2)
		}
	}
	err := s.run(ctx, 15*time.Second, chromedp.MouseClickXY(float64(x), float64(vy)))
	if err != nil {
		return err
	}
	return s.settle(ctx)
}

func (s *cdpSession) Text(ctx context.Context, id int, text string, submit, replace bool) error {
	if id > 0 {
		js := fmt.Sprintf(`(()=>{const e=document.querySelector('[data-phe-id="%d"]');if(!e)return false;
e.scrollIntoView({block:'center'});e.focus();if(%v&&e.select)e.select();return true})()`, id, replace)
		var ok bool
		if err := s.run(ctx, 10*time.Second, chromedp.Evaluate(js, &ok)); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no such region %d", id)
		}
	}
	var acts []chromedp.Action
	if text != "" {
		acts = append(acts, chromedp.ActionFunc(func(c context.Context) error { return inputInsert(c, text) }))
	}
	if submit {
		acts = append(acts, chromedp.KeyEvent(kb.Enter))
	}
	if len(acts) > 0 {
		if err := s.run(ctx, 10*time.Second, acts...); err != nil {
			return err
		}
	}
	s.sy = 0
	return s.settle(ctx)
}

var keyNames = map[string]string{
	"Backspace": kb.Backspace, "Enter": kb.Enter, "Tab": kb.Tab, "Delete": kb.Delete, "ArrowLeft": kb.ArrowLeft,
	"ArrowRight": kb.ArrowRight, "ArrowUp": kb.ArrowUp, "ArrowDown": kb.ArrowDown, "Escape": kb.Escape,
}

func (s *cdpSession) Key(ctx context.Context, name string) error {
	k, ok := keyNames[name]
	if !ok {
		return fmt.Errorf("unknown key %q", name)
	}
	if err := s.run(ctx, 10*time.Second, chromedp.KeyEvent(k)); err != nil {
		return err
	}
	return s.settle(ctx)
}

func (s *cdpSession) Select(ctx context.Context, id, index int) error {
	js := fmt.Sprintf(`(()=>{const e=document.querySelector('[data-phe-id="%d"]');if(!e||e.tagName!=='SELECT'||!e.options[%d])return false;
e.selectedIndex=%d;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));return true})()`, id, index, index)
	var ok bool
	if err := s.run(ctx, 10*time.Second, chromedp.Evaluate(js, &ok)); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no such select/option %d/%d", id, index)
	}
	s.sy = 0
	return s.settle(ctx)
}

func (s *cdpSession) SetHandlers(h Handlers) { s.box.mu.Lock(); s.box.h = h; s.box.mu.Unlock() }

func (s *cdpSession) ScrollTo(ctx context.Context, y int) error {
	s.sy = y
	return s.run(ctx, 5*time.Second, chromedp.Evaluate(fmt.Sprintf(`window.scrollTo(0,%d)`, y), nil))
}

// ---- snapshot ----

type jsLayer struct {
	ID     int    `json:"id"`
	Kind   string `json:"kind"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Z      int    `json:"z"`
	Top    *int   `json:"top"`
	Bottom *int   `json:"bottom"`
}

type jsRegion struct {
	ID    int    `json:"id"`
	Layer int    `json:"layer"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Kind  string `json:"kind"`
	Href  string `json:"href"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Ph    string `json:"ph"`
	Value string `json:"value"`
	Label string `json:"label"`
	Opts  []struct {
		L string `json:"l"`
		S bool   `json:"s"`
	} `json:"opts"`
}

type jsFocus struct {
	ID    int    `json:"id"`
	Kind  string `json:"kind"`
	Type  string `json:"type"`
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	IM    string `json:"im"`
	Enter string `json:"enter"`
}

type jsInfo struct {
	URL     string     `json:"url"`
	Title   string     `json:"title"`
	H       int        `json:"h"`
	Layers  []jsLayer  `json:"layers"`
	Regions []jsRegion `json:"regions"`
	Focus   jsFocus    `json:"focus"`
}

// lazyJS scrolls through the page once to trigger lazy loading, but only when the page can have lazy content.
const lazyJS = `(async()=>{
 const H=Math.min(Math.max(document.documentElement.scrollHeight,document.body?document.body.scrollHeight:0),%d);
 const lazy=[...document.images].some(i=>!i.complete||i.loading==='lazy')||document.querySelector('[data-src],[data-lazy-src],[data-srcset]');
 if(H<=%d||!lazy){window.scrollTo(0,0);return false;}
 for(let y=0;y<H;y+=%d){window.scrollTo(0,y);await new Promise(r=>setTimeout(r,40));}
 window.scrollTo(0,0);
 const imgs=[...document.images].filter(i=>!i.complete);
 await Promise.race([Promise.all(imgs.map(i=>new Promise(r=>{i.addEventListener('load',r);i.addEventListener('error',r);}))),new Promise(r=>setTimeout(r,2500))]);
 await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
 return true;})()`

// extractJS runs at scroll 0. It tags layer roots with data-phe-layer and every tap target with data-phe-id.
const extractJS = `(()=>{
 const out={layers:[],regions:[],title:document.title,url:location.href,
  h:Math.max(document.documentElement.scrollHeight,document.body?document.body.scrollHeight:0)};
 document.querySelectorAll('[data-phe-layer]').forEach(e=>e.removeAttribute('data-phe-layer'));
 let nid=window.__pheNext||1;
 const idOf=e=>{let v=e.getAttribute('data-phe-id');if(!v){v=String(nid++);e.setAttribute('data-phe-id',v);}return +v;};
 const vh=innerHeight,vw=innerWidth;
 const roots=[];
 const all=document.body?document.body.querySelectorAll('*'):[];
 for(const e of all){
  if(out.layers.length>=8)break;
  const cs=getComputedStyle(e);
  if(cs.position!=='fixed'&&cs.position!=='sticky')continue;
  if(cs.visibility==='hidden'||cs.display==='none'||+cs.opacity===0)continue;
  if(roots.some(l=>l.contains(e)))continue;
  const r=e.getBoundingClientRect();
  if(r.width<4||r.height<4)continue;
  if(cs.position==='fixed'&&(r.bottom<=0||r.top>=vh||r.right<=0||r.left>=vw))continue;
  const x=Math.max(0,Math.round(r.left)),y=Math.max(0,Math.round(r.top));
  const w=Math.min(vw-x,Math.round(r.width)),h=Math.min(cs.position==='fixed'?vh-y:100000,Math.round(r.height));
  if(w<4||h<4)continue;
  const px=v=>{const n=parseFloat(v);return isNaN(n)?null:Math.round(n)};
  const id=out.layers.length+1;
  e.setAttribute('data-phe-layer',String(id));
  roots.push(e);
  out.layers.push({id,kind:cs.position,x,y,w,h,z:parseInt(cs.zIndex)||0,top:px(cs.top),bottom:px(cs.bottom)});
 }
 const sel='a[href],button,input:not([type=hidden]),select,textarea,[role=button],[role=link],[onclick]';
 const els=document.querySelectorAll(sel);
 for(const e of els){
  if(out.regions.length>=1500)break;
  const cs=getComputedStyle(e);
  if(cs.visibility==='hidden'||cs.display==='none')continue;
  const r=e.getBoundingClientRect();
  if(r.width<4||r.height<4)continue;
  const lr=e.closest('[data-phe-layer]');
  let layer=0,x=Math.round(r.left),y=Math.round(r.top);
  if(lr){layer=+lr.getAttribute('data-phe-layer');const b=lr.getBoundingClientRect();x=Math.round(r.left-b.left);y=Math.round(r.top-b.top);}
  if(r.left>=vw||r.right<=0)continue;
  const tag=e.tagName.toLowerCase();
  let kind='button';
  if(tag==='a'||e.getAttribute('role')==='link')kind='link';
  else if(tag==='input'){const t=(e.type||'text').toLowerCase();kind=['button','submit','reset','checkbox','radio','image'].includes(t)?'button':'input';}
  else if(tag==='select')kind='select';
  else if(tag==='textarea')kind='textarea';
  out.regions.push({id:idOf(e),layer,x,y,w:Math.round(r.width),h:Math.round(r.height),kind,
   href:tag==='a'?e.href:'',type:e.type||'',name:e.name||'',ph:e.placeholder||'',
   value:(tag==='input'||tag==='textarea')&&e.type!=='password'?String(e.value||'').slice(0,64):'',
   label:(e.getAttribute('aria-label')||e.innerText||e.value||'').trim().slice(0,40),opts:tag==='select'?[...e.options].slice(0,100).map(o=>({l:(o.label||o.text||'').slice(0,40),s:o.selected})):undefined});
 }
 out.focus={kind:'none'};
 {const a=document.activeElement;
  if(a&&a!==document.body&&a!==document.documentElement){
   const tg=a.tagName.toLowerCase(),ty=(a.type||'').toLowerCase();let kind='none';
   if(tg==='textarea')kind='textarea';
   else if(tg==='input'&&!['button','submit','reset','checkbox','radio','image','file','range','color'].includes(ty))kind='text';
   else if(tg==='select')kind='select';
   else if(a.isContentEditable)kind='textarea';
   if(kind!=='none'){let st=-1,en=-1;try{st=a.selectionStart;en=a.selectionEnd;}catch(e){}
    out.focus={id:idOf(a),kind,type:tg==='input'?ty:'',value:ty==='password'?'':String(a.value!==undefined?a.value:(a.innerText||'')).slice(0,200),
     start:st===null?-1:st,end:en===null?-1:en,im:a.inputMode||'',enter:a.enterKeyHint||''};}
  }}
 window.__pheNext=nid;
 return JSON.stringify(out);})()`

const hideCSS = `(()=>{let s=document.getElementById('phe-hide');if(!s){s=document.createElement('style');s.id='phe-hide';
 s.textContent='[data-phe-layer]{visibility:hidden!important}';document.head.appendChild(s);}return true})()`
const unhideCSS = `(()=>{const s=document.getElementById('phe-hide');if(s)s.remove();return true})()`

func (s *cdpSession) shot(ctx context.Context, x, y, w, h int, beyond bool) (*image.RGBA, error) {
	var buf []byte
	err := s.run(ctx, 40*time.Second, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		buf, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).
			WithCaptureBeyondViewport(beyond).
			WithClip(&page.Viewport{X: float64(x), Y: float64(y), Width: float64(w), Height: float64(h), Scale: 1}).Do(c)
		return err
	}))
	if err != nil {
		return nil, err
	}
	im, err := png.Decode(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(im.Bounds())
	draw.Draw(rgba, rgba.Bounds(), im, im.Bounds().Min, draw.Src)
	return rgba, nil
}

func (s *cdpSession) Snapshot(ctx context.Context) (*Snapshot, error) {
	keepSY := s.sy
	// 1. trigger lazy loading, return to the top.
	var done bool
	if err := s.run(ctx, 40*time.Second, chromedp.Evaluate(fmt.Sprintf(lazyJS, s.maxH, viewH, viewH), &done,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		return nil, fmt.Errorf("lazy pass: %w", err)
	}
	// 2. extract layers + regions.
	var js string
	if err := s.run(ctx, 15*time.Second, chromedp.Evaluate(extractJS, &js)); err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	var info jsInfo
	if err := json.Unmarshal([]byte(js), &info); err != nil {
		return nil, err
	}
	sn := &Snapshot{URL: info.URL, Title: info.Title, W: s.width}
	sn.Focus = Focus{ID: info.Focus.ID, Kind: info.Focus.Kind, Type: info.Focus.Type, Value: info.Focus.Value,
		Start: info.Focus.Start, End: info.Focus.End, InputMode: info.Focus.IM, EnterKey: info.Focus.Enter}
	if sn.Focus.Kind == "" {
		sn.Focus.Kind = "none"
	}
	s.run(ctx, 5*time.Second, chromedp.ActionFunc(func(c context.Context) error {
		idx, entries, err := page.GetNavigationHistory().Do(c)
		if err == nil {
			sn.Back = idx > 0 && entries[idx-1].URL != "about:blank" // the first entry of a fresh tab is about:blank
			sn.Fwd = int(idx) < len(entries)-1
		}
		return nil
	}))
	h := info.H
	if h < viewH {
		h = viewH
	}
	if h > s.maxH {
		h, sn.Truncated = s.maxH, true
	}
	sn.H = h
	// 3. layers as their own images (fixed ones inside the viewport, sticky ones beyond it).
	for _, l := range info.Layers {
		img, err := s.shot(ctx, l.X, l.Y, l.W, l.H, l.Kind == "sticky")
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", l.ID, err)
		}
		sn.Layers = append(sn.Layers, Layer{ID: l.ID, Kind: l.Kind, X: l.X, Y: l.Y, W: l.W, H: l.H, Z: l.Z, Top: l.Top, Bottom: l.Bottom, Img: img})
	}
	// 4. the page itself with the layers hidden.
	if len(info.Layers) > 0 {
		s.run(ctx, 5*time.Second, chromedp.Evaluate(hideCSS, nil))
	}
	img, err := s.shot(ctx, 0, 0, s.width, h, true)
	if len(info.Layers) > 0 {
		s.run(ctx, 5*time.Second, chromedp.Evaluate(unhideCSS, nil))
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
	if keepSY > 0 {
		s.ScrollTo(ctx, keepSY)
	}
	return sn, nil
}

func (s *cdpSession) bcid() cdp.BrowserContextID {
	return chromedp.FromContext(s.ctx).BrowserContextID
}

// browserCtx makes c address the Browser target: storage.* calls with a browserContextId are browser-level.
func (s *cdpSession) browserCtx(c context.Context) context.Context {
	return cdp.WithExecutor(c, chromedp.FromContext(s.ctx).Browser)
}
