// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// The Android stream as a module: the web app (AndroidScreen.vue) and the bridge's own standalone page (cmd/pheandroid/index.html, which serves a copy of
// this file, kept identical by a test) both use it. No framework, no dependencies: WebCodecs for video and audio, a WebSocket to the bridge, pointer / wheel /
// keyboard / clipboard events on the canvas going back, plain fetch for app installs.
//
//   const s = new AndroidStream(canvas, { base: '/android/', onState, onStats, onNote, onSize })
//   s.connect(ticket, { codec, size, fps, kbps, audio })   s.close()   s.key(code)   s.setSound(true)   s.copyFromPhone()   s.pasteToPhone()   s.installApk(file)   s.installFdroid(id)

const KEYS = { Escape: 4, Enter: 66, Backspace: 67, Delete: 112, Tab: 61, ArrowUp: 19, ArrowDown: 20, ArrowLeft: 21, ArrowRight: 22, Home: 3, ' ': 62 }

export class AndroidStream {
  constructor(canvas, opts = {}) {
    this.cv = canvas
    this.g = canvas.getContext('2d')
    this.base = opts.base || './'
    this.vpEl = opts.viewportEl || null
    this.cb = { onState() {}, onStats() {}, onNote() {}, onSize() {}, ...opts }
    this.ws = null; this.dec = null; this.gotKey = false; this.ts = 0
    this.W = canvas.width; this.H = canvas.height; this.rtt = 0; this.sid = ''
    this.ac = null; this.adec = null; this.aNext = 0; this.audioCfg = null; this.soundOn = false; this.audioOn = false
    this.phoneClip = ''; this.down = false; this.pingTimer = 0
    this.state = 'idle'
    this.off = []
    this.bind()
  }

  setState(s, message = '') { this.state = s; this.cb.onState(s, message) }
  note(t) { this.cb.onNote(t) }

  // ---- connection ----
  connect(ticket, o = {}) {
    this.close(true)
    if (!('VideoDecoder' in window)) { this.setState('error', 'This browser has no WebCodecs video decoder (it needs a secure https page and a current browser).'); return }
    this.gotKey = false; this.sid = ''; this.audioCfg = null; this.bye = ''
    const prefix = this.base.startsWith('/') || this.base.startsWith('http') ? this.base : location.pathname.replace(/[^/]*$/, '') + this.base.replace(/^\.\//, '')
    const q = ['t=' + encodeURIComponent(ticket)].concat(['codec', 'size', 'fps', 'kbps'].filter((k) => o[k]).map((k) => k + '=' + encodeURIComponent(o[k])))
    if (o.audio === false) q.push('audio=0')
    this.kindOpt = o.kind || 'auto'
    const vp = this.viewport()
    q.push('vw=' + vp.w, 'vh=' + vp.h, 'kind=' + vp.kind)
    this.sentVp = vp
    const url = (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + prefix.replace(/\/?$/, '/') + 'ws?' + q.join('&')
    this.setState('connecting')
    const ws = new WebSocket(url); ws.binaryType = 'arraybuffer'; this.ws = ws
    ws.onopen = () => { this.ping() }
    ws.onclose = (e) => {
      if (ws !== this.ws) return
      clearInterval(this.pingTimer)
      this.setState('closed', this.bye || e.reason || (e.code === 1006 || e.code === 1008 ? 'Not allowed or not reachable.' : 'Disconnected.'))
    }
    ws.onmessage = (e) => this.message(e.data)
    clearInterval(this.pingTimer)
    this.pingTimer = setInterval(() => this.ping(), 2000)
  }

  close(quiet) {
    clearInterval(this.pingTimer)
    const ws = this.ws; this.ws = null
    if (ws) { try { ws.close() } catch (e) { /* closing */ } }
    if (this.dec) { try { this.dec.close() } catch (e) { /* closed */ } this.dec = null }
    if (this.adec) { try { this.adec.close() } catch (e) { /* closed */ } this.adec = null }
    if (!quiet) this.setState('closed', '')
  }

  dispose() { this.closeMenu(); this.close(true); this.off.forEach((f) => f()); this.off = []; if (this.ac) { try { this.ac.close() } catch (e) { /* closed */ } this.ac = null } }

  // the display follows the viewing device: the usable window area and a kind (phone-shaped display, or a large landscape one for tablets / laptops / desktops)
  viewport() {
    // the area the screen may use: the element the page gave (its stage), or the window
    const el = this.vpEl
    const r = el ? el.getBoundingClientRect() : null
    const w = Math.round(r && r.width > 0 ? r.width : window.innerWidth), h = Math.round(r && r.height > 0 ? r.height : window.innerHeight)
    let kind = this.kindOpt
    if (kind !== 'phone' && kind !== 'large') kind = Math.min(window.screen.width, window.screen.height) >= 600 ? 'large' : 'phone'
    // phone mode chosen by hand in a window that is wider than tall: a portrait phone-shaped display (not a landscape one the size of the window)
    if (this.kindOpt === 'phone' && w > h) return { w: 360, h: 720, kind }
    return { w, h, kind }
  }

  /** The user chose a mode: 'phone' or 'large' (desktop), or 'auto' to follow the window again. */
  setKind(kind) {
    this.kindOpt = kind
    this.sendViewport(true)
  }

  sendViewport(force = false) {
    const v = this.viewport(), o = this.sentVp
    if (!force && o && o.kind === v.kind && Math.abs(v.w - o.w) < o.w * 0.05 && Math.abs(v.h - o.h) < o.h * 0.05) return
    this.sentVp = v
    this.send({ t: 'viewport', ...v })
  }

  send(o) { if (this.ws && this.ws.readyState === 1) this.ws.send(JSON.stringify(o)) }
  ping() { this.send({ t: 'ping', ts: performance.now() }) }

  message(d) {
    if (typeof d === 'string') {
      const m = JSON.parse(d)
      if (m.t === 'cfg') {
        this.W = m.w; this.H = m.h; this.cv.width = m.w; this.cv.height = m.h
        this.cb.onSize(m.w, m.h)
        if (this.dec) { try { this.dec.close() } catch (e) { /* closed */ } }
        this.dec = new VideoDecoder({ output: (f) => { this.g.drawImage(f, 0, 0, this.W, this.H); f.close() }, error: (er) => this.note('Video decoder: ' + er.message) })
        this.dec.configure({ codec: m.codec, optimizeForLatency: true })
        this.gotKey = false
        if (this.state !== 'live') this.setState('live')
      } else if (m.t === 'hello') { this.sid = m.sid }
      else if (m.t === 'audio') { this.audioOn = m.on; window.__audioMsg = m.on; if (!m.on) this.note('This Android has no audio capture; video only.') }
      else if (m.t === 'clip') { this.phoneClip = m.s; window.__clip = m.s; this.tryWriteClip() }
      else if (m.t === 'ime') { this.ime = { editor: m.editor, selection: m.selection }; this.updateMenu() }
      else if (m.t === 'stats') this.cb.onStats({ ...m, rtt: this.rtt, w: this.W, h: this.H })
      else if (m.t === 'pong') this.rtt = performance.now() - m.ts
      else if (m.t === 'bye') this.bye = m.reason
      else if (m.t === 'starting') this.setState('starting', m.phase) // the Android container was parked: it is being started / is booting
      return
    }
    const u = new Uint8Array(d)
    if (u[0] & 0x10) { this.audioPacket(u); return }
    if (!this.dec || this.dec.state !== 'configured') return
    const key = (u[0] & 2) !== 0
    if (!this.gotKey && !key) return
    this.gotKey = true
    this.dec.decode(new EncodedVideoChunk({ type: key ? 'key' : 'delta', timestamp: (this.ts += 33333), data: u.subarray(1) }))
  }

  // ---- audio: Opus -> AudioDecoder -> scheduled buffers; starts after setSound(true) (a click is needed before a browser plays sound) ----
  audioPacket(u) {
    const cfg = (u[0] & 1) !== 0, body = u.subarray(9)
    if (cfg) this.audioCfg = u.slice() // sent once at stream start, before the user can press Sound: keep it
    if (!this.soundOn || !('AudioDecoder' in window)) return
    if (cfg) {
      if (this.adec) { try { this.adec.close() } catch (e) { /* closed */ } }
      this.adec = new AudioDecoder({ output: (a) => this.play(a), error: (er) => this.note('Audio decoder: ' + er.message) })
      this.adec.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: 2, description: body.slice() })
      return
    }
    if (!this.adec || this.adec.state !== 'configured') return
    const pts = Number(new DataView(u.buffer, u.byteOffset + 1, 8).getBigUint64(0))
    this.adec.decode(new EncodedAudioChunk({ type: 'key', timestamp: pts, data: body }))
  }

  play(d) {
    try {
      if (!this.ac) return
      const buf = this.ac.createBuffer(d.numberOfChannels, d.numberOfFrames, d.sampleRate)
      for (let c = 0; c < d.numberOfChannels; c++) { const a = new Float32Array(d.numberOfFrames); d.copyTo(a, { planeIndex: c, format: 'f32-planar' }); buf.copyToChannel(a, c) }
      const src = this.ac.createBufferSource(); src.buffer = buf; src.connect(this.ac.destination)
      const now = this.ac.currentTime
      if (this.aNext < now + 0.02 || this.aNext > now + 0.5) this.aNext = now + 0.06 // restart with a small cushion; drop a backlog
      src.start(this.aNext); this.aNext += buf.duration
      window.__audioFrames = (window.__audioFrames || 0) + d.numberOfFrames
    } finally { d.close() }
  }

  setSound(on) {
    this.soundOn = on
    if (on) {
      this.ac = this.ac || new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 48000 })
      this.ac.resume()
      if (this.audioCfg) this.audioPacket(this.audioCfg)
    } else if (this.ac) this.ac.suspend()
  }

  // ---- input ----
  key(code, meta = 0) { this.send({ t: 'key', a: 0, code, meta }); this.send({ t: 'key', a: 1, code, meta }) }

  pos(e) { const r = this.cv.getBoundingClientRect(); return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height } }

  bind() {
    const cv = this.cv
    const on = (t, ev, fn, o) => { t.addEventListener(ev, fn, o); this.off.push(() => t.removeEventListener(ev, fn, o)) }
    let vpTimer = 0
    const later = () => { clearTimeout(vpTimer); vpTimer = setTimeout(() => this.sendViewport(), 500) } // debounced: dragging a window edge is one change
    on(window, 'resize', later)
    if (this.vpEl && typeof ResizeObserver !== 'undefined') { // the stage itself: a desktop window resized, a layout change
      const ro = new ResizeObserver(later)
      ro.observe(this.vpEl)
      this.off.push(() => ro.disconnect())
    }
    this.off.push(() => clearTimeout(vpTimer))
    on(cv, 'pointerdown', (e) => { if (e.button !== 0) return; this.closeMenu(); cv.focus(); this.down = true; cv.setPointerCapture(e.pointerId); this.send({ t: 'touch', a: 0, ...this.pos(e) }); e.preventDefault() })
    on(cv, 'pointermove', (e) => { if (this.down) this.send({ t: 'touch', a: 2, ...this.pos(e) }) })
    const up = (e) => { if (this.down) { this.down = false; this.send({ t: 'touch', a: 1, ...this.pos(e) }) } }
    on(cv, 'pointerup', up); on(cv, 'pointercancel', up)
    on(cv, 'contextmenu', (e) => { e.preventDefault(); if (e.pointerType === 'touch') return; this.openMenu(e.clientX, e.clientY) })
    on(document, 'pointerdown', (e) => { if (this.menu && !this.menu.contains(e.target)) this.closeMenu() })
    on(window, 'blur', () => this.closeMenu())
    on(cv, 'wheel', (e) => { // lines: 3 per step; pixels: 100 per step; positive dy scrolls up
      const k = e.deltaMode === 1 ? 1 / 3 : 1 / 100, p = this.pos(e)
      this.send({ t: 'scroll', x: p.x, y: p.y, dx: -e.deltaX * k, dy: -e.deltaY * k }); e.preventDefault()
    }, { passive: false })
    on(cv, 'keydown', (e) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === 'c' || e.key === 'x')) { this.send({ t: 'clip_get', cut: e.key === 'x' }); return }
      if (e.ctrlKey || e.metaKey || e.altKey) return
      if (KEYS[e.key] !== undefined) { this.send({ t: 'key', a: 0, code: KEYS[e.key] }); e.preventDefault() }
      else if (e.key.length === 1) { this.send({ t: 'text', s: e.key }); e.preventDefault() }
    })
    on(cv, 'keyup', (e) => { if (KEYS[e.key] !== undefined) { this.send({ t: 'key', a: 1, code: KEYS[e.key] }); e.preventDefault() } })
    on(document, 'paste', (e) => {
      if (document.activeElement !== cv) return
      const t = e.clipboardData && e.clipboardData.getData('text')
      if (t) { this.send({ t: 'clip_set', s: t, paste: true }); e.preventDefault() }
    })
  }

  // ---- the page's own context menu (right click): what is offered follows the state of Android ----
  // Android's state comes from `dumpsys input_method` on the bridge (a text field is focused; something is selected), asked for when the menu opens.
  openMenu(x, y) {
    this.closeMenu()
    this.ime = { editor: false, selection: false }
    this.send({ t: 'ime' })
    const m = document.createElement('div')
    m.setAttribute('role', 'menu'); m.setAttribute('aria-label', 'Android')
    m.style.cssText = 'position:fixed;z-index:10000;min-width:200px;padding:6px 0;border-radius:var(--ph-r-control,8px);background:var(--ph-surface,#fff);color:var(--ph-text,#1a1d21);border:1px solid var(--ph-divider,#d9dde2);box-shadow:var(--ph-elev-3,0 12px 32px rgba(16,24,40,.18));font:var(--ph-text-body,16px/1.4 system-ui,sans-serif)'
    const items = [
      ['copy', 'Copy', () => this.send({ t: 'clip_get' })],
      ['cut', 'Cut', () => this.send({ t: 'clip_get', cut: true })],
      ['paste', 'Paste', () => this.pasteToPhone()],
      ['selectall', 'Select all', () => this.key(29, 0x3000)], // A with Ctrl (AMETA_CTRL_ON | AMETA_CTRL_LEFT_ON)
      null,
      ['back', 'Back', () => this.key(4)],
      ['home', 'Home', () => this.key(3)],
      ['recent', 'Recent apps', () => this.key(187)],
    ]
    for (const it of items) {
      if (!it) { const hr = document.createElement('div'); hr.setAttribute('role', 'separator'); hr.style.cssText = 'height:1px;margin:6px 0;background:var(--ph-divider,#d9dde2)'; m.appendChild(hr); continue }
      const b = document.createElement('button')
      b.type = 'button'; b.setAttribute('role', 'menuitem'); b.dataset.item = it[0]; b.textContent = it[1]
      b.style.cssText = 'display:block;width:100%;min-height:40px;padding:8px 16px;text-align:left;font:inherit;color:inherit;background:transparent;border:0;cursor:pointer'
      b.addEventListener('click', () => { if (b.getAttribute('aria-disabled') === 'true') return; this.closeMenu(); it[2](); this.cv.focus() })
      b.addEventListener('mouseenter', () => { if (b.getAttribute('aria-disabled') !== 'true') b.focus() })
      m.appendChild(b)
    }
    m.addEventListener('keydown', (e) => {
      const bs = [...m.querySelectorAll('button:not([aria-disabled="true"])')], i = bs.indexOf(document.activeElement)
      if (e.key === 'Escape') { this.closeMenu(); this.cv.focus(); e.preventDefault() }
      else if (e.key === 'ArrowDown') { (bs[(i + 1) % bs.length] || bs[0]).focus(); e.preventDefault() }
      else if (e.key === 'ArrowUp') { (bs[(i - 1 + bs.length) % bs.length] || bs[0]).focus(); e.preventDefault() }
    })
    document.body.appendChild(m)
    const w = m.offsetWidth, h = m.offsetHeight
    m.style.left = Math.max(4, Math.min(x, window.innerWidth - w - 4)) + 'px'
    m.style.top = Math.max(4, Math.min(y, window.innerHeight - h - 4)) + 'px'
    this.menu = m
    this.updateMenu()
    const first = m.querySelector('button:not([aria-disabled="true"])')
    if (first) first.focus()
  }

  updateMenu() {
    if (!this.menu) return
    const { editor, selection } = this.ime || {}
    const on = { copy: selection, cut: selection, paste: editor, selectall: editor, back: true, home: true, recent: true }
    this.menu.querySelectorAll('button').forEach((b) => {
      const ok = !!on[b.dataset.item]
      b.setAttribute('aria-disabled', String(!ok)); b.style.opacity = ok ? '1' : '0.45'; b.style.cursor = ok ? 'pointer' : 'default'
    })
  }

  closeMenu() { if (this.menu) { this.menu.remove(); this.menu = null } }

  // ---- clipboard ----
  async tryWriteClip() {
    if (!this.phoneClip) return
    try { await navigator.clipboard.writeText(this.phoneClip); this.note('Copied from the phone: ' + this.phoneClip.slice(0, 60)) }
    catch (e) { this.note('The phone copied text. Press "Copy from phone" to put it on this computer.') }
  }
  async copyFromPhone() { if (this.phoneClip) await this.tryWriteClip(); this.send({ t: 'clip_get' }) }
  async pasteToPhone() {
    try { const t = await navigator.clipboard.readText(); if (t) { this.send({ t: 'clip_set', s: t, paste: true }); this.note('Pasted ' + t.length + ' characters.') } }
    catch (e) { this.note('This browser did not allow reading the clipboard. Click the screen and press Ctrl+V instead.') }
  }

  // ---- installing apps (HTTP on the bridge, gated by this stream's session id) ----
  async post(url, body, what) {
    this.note(what + ' ...')
    try {
      const r = await fetch(this.base + url, { method: 'POST', headers: { 'X-Session': this.sid }, body })
      const j = await r.json()
      this.note(what + ': ' + (j.ok ? 'installed. ' : 'failed. ') + (j.detail || ''))
      return j.ok
    } catch (e) { this.note(what + ' failed: ' + e.message); return false }
  }
  installApk(file) { return this.post('apk', file, 'Installing ' + file.name) }
  installFdroid(id) { return this.post('fdroid?id=' + encodeURIComponent(id), '', 'Installing ' + id + ' from F-Droid') }
}
