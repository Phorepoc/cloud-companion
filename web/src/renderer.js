// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The cloud page on a canvas: tiles of the page, layers on top (fixed, sticky, scroll boxes), local scrolling, local
// pinch/ctrl-wheel/button zoom, and the tap -> region -> input frame path. Nothing here talks to the server except
// through `api`.
//
// Geometry: the page is always rendered by the cloud at exactly 360 css px. The visible window is (panX, scrollY) with size
// (360/zoom x 640/zoom) in page units; the canvas shows it scaled by `zoom`. When the server advertises the "zoom" feature the
// view would re-request tiles at the new scale; without it (today) the zoom is purely local scaling of the cached tiles.
import { reactive } from 'vue'
import { state, hooks, api } from './cloud.js'
import { hitTest, layerScreenY, regionScreenRect, VIEW_H } from './hit.js'

const W = 360
export const MIN_ZOOM = 1
export const MAX_ZOOM = 3

export class Renderer {
  constructor(canvas) {
    this.cv = canvas
    this.ctx = canvas.getContext('2d')
    this.dpr = Math.max(1, Math.round(window.devicePixelRatio || 1))
    canvas.width = W * this.dpr
    canvas.height = VIEW_H * this.dpr
    this.tiles = new Map() // layer id -> Map("x:y" -> ImageBitmap)
    this.view = reactive({ scrollY: 0, panX: 0, zoom: 1 })
    this.lscroll = {}
    this.hl = null
    this.raf = 0
    this.reportTimer = 0
    this.zoomTimer = 0
    this.onSelect = null
    this.drag = null
    this.pointers = new Map()
    this.pinch = null
    this.pending = null
  }

  attach() {
    hooks.onTile = (f) => this.addTile(f)
    hooks.onNewPage = () => { this.tiles.clear(); this.view.scrollY = 0; this.view.panX = 0; this.view.zoom = 1; this.lscroll = {}; this.invalidate() }
    hooks.onRedraw = () => this.invalidate()
    hooks.onRescale = (oldZ, newZ) => this.rescaled(newZ)
    const c = this.cv
    c.addEventListener('wheel', (e) => {
      e.preventDefault()
      if (e.ctrlKey) { // trackpad pinch / ctrl + wheel
        const p = this.local(e.clientX, e.clientY)
        this.zoomAt(p.x, p.y, this.view.zoom * Math.exp(-e.deltaY * 0.01))
      } else this.scrollBy(e.clientX, e.clientY, e.deltaY, e.deltaX)
    }, { passive: false })
    c.addEventListener('pointerdown', (e) => {
      c.setPointerCapture(e.pointerId)
      this.pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
      if (this.pointers.size === 2) { // second finger: pinch
        this.drag = null
        this.pinch = { d: this.pointerDistance(), z: this.view.zoom }
      } else if (this.pointers.size === 1) {
        this.drag = { x: e.clientX, y: e.clientY, lx: e.clientX, ly: e.clientY, moved: false }
      }
    })
    c.addEventListener('pointermove', (e) => {
      const pt = this.pointers.get(e.pointerId)
      if (pt) { pt.x = e.clientX; pt.y = e.clientY }
      if (this.pinch && this.pointers.size >= 2) {
        const [a, b] = [...this.pointers.values()]
        const p = this.local((a.x + b.x) / 2, (a.y + b.y) / 2)
        this.zoomAt(p.x, p.y, this.pinch.z * (this.pointerDistance() / this.pinch.d))
        return
      }
      const d = this.drag
      if (!d) return
      if (!d.moved && Math.hypot(e.clientX - d.x, e.clientY - d.y) > 6) d.moved = true
      if (d.moved) {
        this.scrollBy(e.clientX, e.clientY, d.ly - e.clientY, d.lx - e.clientX)
        d.lx = e.clientX
        d.ly = e.clientY
      }
    })
    const up = (e) => {
      this.pointers.delete(e.pointerId)
      if (this.pinch) { if (this.pointers.size < 2) this.pinch = null; this.drag = null; return }
      const d = this.drag
      this.drag = null
      if (d && !d.moved && e.type === 'pointerup') this.tapAt(e.clientX, e.clientY)
    }
    c.addEventListener('pointerup', up)
    c.addEventListener('pointercancel', up)
    this.invalidate()
  }

  detach() {
    hooks.onTile = hooks.onNewPage = hooks.onRedraw = hooks.onRescale = null
    cancelAnimationFrame(this.raf)
  }

  pointerDistance() {
    const [a, b] = [...this.pointers.values()]
    return Math.max(1, Math.hypot(a.x - b.x, a.y - b.y))
  }

  async addTile(f) {
    if (f.pid !== state.page.pid) return
    const bmp = await createImageBitmap(new Blob([f.data], { type: 'image/' + f.fmt }))
    let m = this.tiles.get(f.layer)
    if (!m) this.tiles.set(f.layer, (m = new Map()))
    m.set(f.x + ':' + f.y, bmp)
    this.invalidate()
  }

  invalidate() {
    if (!this.raf) this.raf = requestAnimationFrame(() => { this.raf = 0; this.draw() })
  }

  // canvas units (360 x 640) from client coordinates
  local(clientX, clientY) {
    const r = this.cv.getBoundingClientRect()
    return { x: ((clientX - r.left) * W) / r.width, y: ((clientY - r.top) * VIEW_H) / r.height }
  }

  viewH() { return VIEW_H / this.view.zoom }
  maxScroll() { return Math.max(0, state.page.h - this.viewH()) }
  maxPan() { return Math.max(0, W - W / this.view.zoom) }
  clampView() {
    this.view.scrollY = Math.max(0, Math.min(this.maxScroll(), this.view.scrollY))
    this.view.panX = Math.max(0, Math.min(this.maxPan(), this.view.panX))
  }

  // zoom about the canvas point (cx, cy): the page point under it stays under it
  zoomAt(cx, cy, z) {
    const pz = (state.page.zoom || 100) / 100 // the server render scale: the tiles are already pz times bigger
    z = Math.max(MIN_ZOOM / pz, Math.min(MAX_ZOOM / pz, z))
    const v = this.view
    const px = cx / v.zoom + v.panX
    const py = cy / v.zoom + v.scrollY
    v.zoom = z
    v.panX = px - cx / z
    v.scrollY = py - cy / z
    this.clampView()
    this.invalidate()
    clearTimeout(this.zoomTimer)
    // a server that renders at the requested scale advertises the "zoom" feature; otherwise the zoom stays local
    this.zoomTimer = setTimeout(() => this.requestScale(), 400)
  }


  // The server advertises "zoom": after the gesture settles, ask it to re-render at the nearest scale of its ladder (the text
  // stays sharp). The point at the middle of the window is kept in place when the new tiles arrive (rescaled()).
  requestScale() {
    if (!state.features.includes('zoom')) return // otherwise the zoom stays local scaling of the cached tiles
    const ladder = [100, 125, 150, 200, 250, 300]
    const pz = state.page.zoom || 100
    const T = (pz / 100) * this.view.zoom // total zoom
    const target = ladder.reduce((b, v) => (Math.abs(v - T * 100) < Math.abs(b - T * 100) ? v : b), 100)
    if (target === pz) return
    const s = pz / 100
    const v = this.view
    this.pending = { T, cssX: (v.panX + W / 2 / v.zoom) / s, cssY: (v.scrollY + VIEW_H / 2 / v.zoom) / s }
    api.zoom(target)
  }

  // the page frame of a re-render at another scale (same page): old tiles are dropped, the view keeps its centre
  rescaled(newZ) {
    this.tiles.clear()
    const s = newZ / 100
    const p = this.pending
    this.pending = null
    const z = Math.max(1 / s, Math.min(3 / s, (p ? p.T : s * this.view.zoom) / s)) // what is left to scale locally
    this.view.zoom = z
    if (p) {
      this.view.panX = p.cssX * s - W / 2 / z
      this.view.scrollY = p.cssY * s - VIEW_H / 2 / z
    }
    this.clampView()
    this.invalidate()
  }
  zoomBy(f) { this.zoomAt(W / 2, VIEW_H / 2, this.view.zoom * f) }
  zoomReset() { this.zoomAt(W / 2, VIEW_H / 2, 100 / (state.page.zoom || 100)) }

  // screen (canvas unit) point -> point in the scrolled page window
  windowPoint(p) { return { x: p.x / this.view.zoom + this.view.panX, y: p.y / this.view.zoom } }

  scrollBy(clientX, clientY, dy, dx) {
    const p = this.local(clientX, clientY)
    const wp = this.windowPoint(p)
    const z = this.view.zoom
    const h = hitTest(state.layers, state.regions, this.view.scrollY, this.lscroll, wp.x, wp.y, this.viewH())
    const l = state.layers.find((q) => q.id === h.layer && q.kind === 'scroll')
    if (l) { // a scroll box under the pointer scrolls first; the page takes over at the box's end
      const s = this.lscroll[l.id] || (this.lscroll[l.id] = { x: l.sx || 0, y: l.sy || 0 })
      const nx = Math.max(0, Math.min(Math.max(0, (l.cw || l.w) - l.w), s.x + dx / z))
      const ny = Math.max(0, Math.min(Math.max(0, (l.ch || l.h) - l.h), s.y + dy / z))
      const moved = nx !== s.x || ny !== s.y
      s.x = nx
      s.y = ny
      if (moved) { this.invalidate(); return }
    }
    const y0 = this.view.scrollY
    const x0 = this.view.panX
    this.view.scrollY += dy / z
    this.view.panX += dx / z
    this.clampView()
    if (this.view.scrollY !== y0 || this.view.panX !== x0) {
      this.invalidate()
      if (this.view.scrollY !== y0) {
        clearTimeout(this.reportTimer)
        this.reportTimer = setTimeout(() => api.scroll(Math.round(this.view.scrollY)), 400)
      }
    }
  }

  tapAt(clientX, clientY) {
    const p = this.local(clientX, clientY)
    const wp = this.windowPoint(p)
    const h = hitTest(state.layers, state.regions, this.view.scrollY, this.lscroll, wp.x, wp.y, this.viewH())
    if (h.region && h.region.kind === 'select' && h.region.opts && h.region.opts.length && this.onSelect) {
      this.onSelect(h.region)
      return
    }
    if (h.region) {
      this.hl = { r: h.region, t: performance.now() }
      this.invalidate()
    }
    api.tap(h.tap.layer, Math.round(h.tap.x), Math.round(h.tap.y), Math.round(this.view.scrollY))
  }

  // a region's rectangle in canvas units (zoom and pan applied)
  screenRect(r) {
    const w = regionScreenRect(state.layers, r, this.view.scrollY, this.lscroll, this.viewH())
    if (!w) return null
    const z = this.view.zoom
    return { x: (w.x - this.view.panX) * z, y: w.y * z, w: w.w * z, h: w.h * z }
  }

  draw() {
    const ctx = this.ctx
    const { zoom: z, panX, scrollY: sy } = this.view
    ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0)
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, W, VIEW_H)
    // page units from here on: x is page x, y is relative to the scroll position
    ctx.setTransform(this.dpr * z, 0, 0, this.dpr * z, -panX * z * this.dpr, 0)
    ctx.imageSmoothingEnabled = true
    const vh = this.viewH()
    const page = this.tiles.get(0)
    if (page) {
      for (const [k, bmp] of page) {
        const [x, y] = k.split(':').map(Number)
        if (y + bmp.height < sy || y > sy + vh) continue
        ctx.drawImage(bmp, x, y - sy)
      }
    }
    const layers = [...state.layers].sort((a, b) => a.z - b.z || a.id - b.id)
    for (const l of layers) {
      const m = this.tiles.get(l.id)
      if (!m) continue
      const ly = layerScreenY(l, sy, vh)
      ctx.save()
      ctx.beginPath()
      ctx.rect(l.x, ly, l.w, l.h)
      ctx.clip()
      const s = l.kind === 'scroll' ? this.lscroll[l.id] || { x: l.sx || 0, y: l.sy || 0 } : { x: 0, y: 0 }
      for (const [k, bmp] of m) {
        const [x, y] = k.split(':').map(Number)
        ctx.drawImage(bmp, l.x + x - s.x, ly + y - s.y)
      }
      ctx.restore()
    }
    if (this.hl) { // short tap highlight: the local feedback that hides the server round trip
      const age = performance.now() - this.hl.t
      if (age < 350) {
        const r = regionScreenRect(state.layers, this.hl.r, sy, this.lscroll, vh)
        if (r) {
          ctx.fillStyle = 'rgba(30,120,255,' + (0.3 * (1 - age / 350)).toFixed(3) + ')'
          ctx.fillRect(r.x, r.y, r.w, r.h)
        }
        this.invalidate()
      } else this.hl = null
    }
  }
}
