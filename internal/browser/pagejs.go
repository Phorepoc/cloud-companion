// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package browser

// The scripts that read a page for the phone (from the Chromium backend, unchanged): they run in our content-script world.

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
	CW     int    `json:"cw"`
	CH     int    `json:"ch"`
	SX     int    `json:"sx"`
	SY     int    `json:"sy"`
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
 if(window.__pheLazy===location.href||H<=%d||!lazy){window.__pheLazy=location.href;window.scrollTo(0,0);return false;}
 for(let y=0;y<H;y+=%d){window.scrollTo(0,y);await new Promise(r=>setTimeout(r,40));}
 window.scrollTo(0,0);
 const imgs=[...document.images].filter(i=>!i.complete);
 await Promise.race([Promise.all(imgs.map(i=>new Promise(r=>{i.addEventListener('load',r);i.addEventListener('error',r);}))),new Promise(r=>setTimeout(r,2500))]);
 await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
 window.__pheLazy=location.href;
 return true;})()`

// extractJS runs at scroll 0, in our isolated world: it numbers layer roots and tap targets in maps kept there (window.__phe of
// that world), so the page sees no attributes or globals of ours.
const extractJS = `(()=>{
 const out={layers:[],regions:[],title:document.title,url:location.href,
  h:Math.max(document.documentElement.scrollHeight,document.body?document.body.scrollHeight:0)};
 const W=window.__phe||(window.__phe={ids:new WeakMap(),els:new Map(),next:1});
 W.layer=new Map();W.layerOf=new WeakMap();
 const idOf=e=>{let v=W.ids.get(e);if(!v){v=W.next++;W.ids.set(e,v);W.els.set(v,new WeakRef(e));}return v;};
 const vh=innerHeight,vw=innerWidth;
 const roots=[];
 const all=document.body?document.body.querySelectorAll('*'):[];
 for(const e of all){
  if(out.layers.length>=8)break;
  const cs=getComputedStyle(e);
  if(cs.visibility==='hidden'||cs.display==='none'||+cs.opacity===0)continue;
  if(roots.some(l=>l.contains(e)))continue;
  const isScr=(['auto','scroll'].includes(cs.overflowY)&&e.scrollHeight>e.clientHeight+8)||(['auto','scroll'].includes(cs.overflowX)&&e.scrollWidth>e.clientWidth+8);
  if(cs.position!=='fixed'&&cs.position!=='sticky'){
   if(!isScr)continue;
   // an inner scroll box of known size becomes its own scrollable layer (its content is rendered in full)
   const rr=e.getBoundingClientRect();
   if(e.clientWidth<40||e.clientHeight<40||rr.left<0||rr.left>=vw||rr.top+scrollY>=100000)continue;
   const sid=out.layers.length+1,sx0=Math.round(rr.left),sy0=Math.round(rr.top+scrollY);
   W.layer.set(sid,e);W.layerOf.set(e,sid);
   roots.push(e);
   out.layers.push({id:sid,kind:'scroll',x:sx0,y:sy0,w:Math.min(vw-sx0,e.clientWidth),h:e.clientHeight,z:0,
    cw:Math.min(e.scrollWidth,2048),ch:Math.min(e.scrollHeight,4096),sx:Math.round(e.scrollLeft),sy:Math.round(e.scrollTop)});
   continue;
  }
  const r=e.getBoundingClientRect();
  if(r.width<4||r.height<4)continue;
  if(cs.position==='fixed'&&(r.bottom<=0||r.top>=vh||r.right<=0||r.left>=vw))continue;
  const x=Math.max(0,Math.round(r.left)),y=Math.max(0,Math.round(r.top));
  const w=Math.min(vw-x,Math.round(r.width)),h=Math.min(cs.position==='fixed'?vh-y:100000,Math.round(r.height));
  if(w<4||h<4)continue;
  const px=v=>{const n=parseFloat(v);return isNaN(n)?null:Math.round(n)};
  const id=out.layers.length+1;
  W.layer.set(id,e);W.layerOf.set(e,id);
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
  let lr=e;while(lr&&!W.layerOf.has(lr))lr=lr.parentElement;
  let layer=0,x=Math.round(r.left),y=Math.round(r.top);
  if(lr){layer=W.layerOf.get(lr);const b=lr.getBoundingClientRect();x=Math.round(r.left-b.left);y=Math.round(r.top-b.top);
   if(out.layers[layer-1]&&out.layers[layer-1].kind==='scroll'){x+=Math.round(lr.scrollLeft);y+=Math.round(lr.scrollTop);}}
  else if(r.left>=vw||r.right<=0)continue;
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
 return JSON.stringify(out);})()`

// hideJS / unhideJS hide the layer roots for the page shot, on their own inline style (put back exactly after)
const hideJS = `(()=>{const W=window.__phe;if(!W||!W.layer)return true;
 for(const e of W.layer.values()){e.__pheSt=[e.hasAttribute('style'),e.style.getPropertyValue('visibility'),e.style.getPropertyPriority('visibility')];
  e.style.setProperty('visibility','hidden','important');}return true})()`
const unhideJS = `(()=>{const W=window.__phe;if(!W||!W.layer)return true;
 for(const e of W.layer.values()){const st=e.__pheSt;if(!st)continue;delete e.__pheSt;
  if(st[1])e.style.setProperty('visibility',st[1],st[2]);else e.style.removeProperty('visibility');
  if(!st[0]&&!e.getAttribute('style'))e.removeAttribute('style');}return true})()`
