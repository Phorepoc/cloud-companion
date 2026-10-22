// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

const mobileUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/156.0.0.0 Mobile Safari/537.36"

const viewH = 640

// CDP is the chromedp-based Factory: one headless Chromium, one tab per Session.
type CDP struct {
	allocCtx context.Context
	cancel   context.CancelFunc
	MaxH     int
}

func NewCDP(chromePath string, maxH int) *CDP {
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
	return &CDP{allocCtx: ctx, cancel: cancel, MaxH: maxH}
}

func (c *CDP) New(ctx context.Context) (Session, error) {
	tctx, cancel := chromedp.NewContext(c.allocCtx)
	if err := chromedp.Run(tctx); err != nil { // start the tab
		cancel()
		return nil, err
	}
	return &cdpSession{ctx: tctx, cancel: cancel, maxH: c.MaxH}, nil
}

func (c *CDP) Close() { c.cancel() }

type cdpSession struct {
	ctx    context.Context
	cancel context.CancelFunc
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

func (s *cdpSession) settle(ctx context.Context) error {
	time.Sleep(250 * time.Millisecond)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var st string
		if err := s.run(ctx, 5*time.Second, chromedp.Evaluate(`document.readyState`, &st)); err == nil && st == "complete" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(250 * time.Millisecond)
	return nil
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

func (s *cdpSession) Text(ctx context.Context, id int, text string, submit bool) error {
	js := fmt.Sprintf(`(()=>{const e=document.querySelector('[data-phe-id="%d"]');if(!e)return false;
e.scrollIntoView({block:'center'});e.focus();if(e.select)e.select();return true})()`, id)
	var ok bool
	if err := s.run(ctx, 10*time.Second, chromedp.Evaluate(js, &ok)); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no such region %d", id)
	}
	acts := []chromedp.Action{chromedp.ActionFunc(func(c context.Context) error { return inputInsert(c, text) })}
	if submit {
		acts = append(acts, chromedp.KeyEvent(kb.Enter))
	}
	if err := s.run(ctx, 10*time.Second, acts...); err != nil {
		return err
	}
	s.sy = 0
	return s.settle(ctx)
}

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
}

type jsInfo struct {
	URL     string     `json:"url"`
	Title   string     `json:"title"`
	H       int        `json:"h"`
	Layers  []jsLayer  `json:"layers"`
	Regions []jsRegion `json:"regions"`
}

const lazyJS = `(async()=>{
 const H=Math.min(Math.max(document.documentElement.scrollHeight,document.body?document.body.scrollHeight:0),%d);
 for(let y=0;y<H;y+=%d){window.scrollTo(0,y);await new Promise(r=>setTimeout(r,60));}
 window.scrollTo(0,0);
 const imgs=[...document.images].filter(i=>!i.complete);
 await Promise.race([Promise.all(imgs.map(i=>new Promise(r=>{i.addEventListener('load',r);i.addEventListener('error',r);}))),new Promise(r=>setTimeout(r,2500))]);
 await new Promise(r=>setTimeout(r,100));
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
   label:(e.getAttribute('aria-label')||e.innerText||e.value||'').trim().slice(0,40)});
 }
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
	if err := s.run(ctx, 40*time.Second, chromedp.Evaluate(fmt.Sprintf(lazyJS, s.maxH, viewH), &done,
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
		sn.Regions = append(sn.Regions, Region{ID: r.ID, Layer: r.Layer, X: r.X, Y: r.Y, W: r.W, H: r.H, Kind: r.Kind,
			Href: r.Href, Type: r.Type, Name: r.Name, Placeholder: r.Ph, Value: r.Value, Label: r.Label})
	}
	if keepSY > 0 {
		s.ScrollTo(ctx, keepSY)
	}
	return sn, nil
}
