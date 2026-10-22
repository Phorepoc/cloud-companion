// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The cloud page on a canvas: tiles of the page, layers on top (fixed, sticky, scroll boxes), local scrolling, and the
// tap -> region -> input frame path. Nothing here talks to the server except through `api`.
import { reactive } from 'vue'
import { state, hooks, api } from './cloud.js'
import { hitTest, layerScreenY, regionScreenRect, VIEW_H } from './hit.js'

const W = 360

export class Renderer {
  constructor(canvas) {
    this.cv = canvas
    this.ctx = canvas.getContext('2d')
    const dpr = Math.max(1, Math.round(window.devicePixelRatio || 1))
    canvas.width = W * dpr
    canvas.height = VIEW_H * dpr
    this.ctx.scale(dpr, dpr)
    this.tiles = new Map() // layer id -> Map("x:y" -> ImageBitmap)
    this.view = reactive({ scrollY: 0 })
    this.lscroll = {}
    this.hl = null
    this.raf = 0
    this.reportTimer = 0
    this.onSelect = null
    this.drag = null
  }

  attach() {
    hooks.onTile = (f) => this.addTile(f)
    hooks.onNewPage = () => { this.tiles.clear(); this.view.scrollY = 0; this.lscroll = {}; this.invalidate() }
    hooks.onRedraw = () => this.invalidate()
    const c = this.cv
    c.addEventListener('wheel', (e) => { e.preventDefault(); this.scrollBy(e.clientX, e.clientY, e.deltaY, e.deltaX) }, { passive: false })
    c.addEventListener('pointerdown', (e) => {
      c.setPointerCapture(e.pointerId)
      this.drag = { x: e.clientX, y: e.clientY, lx: e.clientX, ly: e.clientY, moved: false }
    })
    c.addEventListener('pointermove', (e) => {
      const d = this.drag
      if (!d) return
      if (!d.moved && Math.hypot(e.clientX - d.x, e.clientY - d.y) > 6) d.moved = true
      if (d.moved) {
        this.scrollBy(e.clientX, e.clientY, d.ly - e.clientY, d.lx - e.clientX)
        d.lx = e.clientX
        d.ly = e.clientY
      }
    })
    c.addEventListener('pointerup', (e) => {
      const d = this.drag
      this.drag = null
      if (d && !d.moved) this.tapAt(e.clientX, e.clientY)
    })
    this.invalidate()
  }

  detach() {
    hooks.onTile = hooks.onNewPage = hooks.onRedraw = null
    cancelAnimationFrame(this.raf)
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

  local(clientX, clientY) {
    const r = this.cv.getBoundingClientRect()
    return { x: ((clientX - r.left) * W) / r.width, y: ((clientY - r.top) * VIEW_H) / r.height }
  }

  maxScroll() { return Math.max(0, state.page.h - VIEW_H) }

  scrollBy(clientX, clientY, dy, dx) {
    const p = this.local(clientX, clientY)
    const h = hitTest(state.layers, state.regions, this.view.scrollY, this.lscroll, p.x, p.y)
    const l = state.layers.find((q) => q.id === h.layer && q.kind === 'scroll')
    if (l) { // a scroll box under the pointer scrolls first; the page takes over at the box's end
      const s = this.lscroll[l.id] || (this.lscroll[l.id] = { x: l.sx || 0, y: l.sy || 0 })
      const nx = Math.max(0, Math.min(Math.max(0, (l.cw || l.w) - l.w), s.x + dx))
      const ny = Math.max(0, Math.min(Math.max(0, (l.ch || l.h) - l.h), s.y + dy))
      const moved = nx !== s.x || ny !== s.y
      s.x = nx
      s.y = ny
      if (moved) { this.invalidate(); return }
    }
    const y = Math.max(0, Math.min(this.maxScroll(), this.view.scrollY + dy))
    if (y !== this.view.scrollY) {
      this.view.scrollY = y
      this.invalidate()
      clearTimeout(this.reportTimer)
      this.reportTimer = setTimeout(() => api.scroll(Math.round(this.view.scrollY)), 400)
    }
  }

  tapAt(clientX, clientY) {
    const p = this.local(clientX, clientY)
    const h = hitTest(state.layers, state.regions, this.view.scrollY, this.lscroll, p.x, p.y)
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

  screenRect(r) { return regionScreenRect(state.layers, r, this.view.scrollY, this.lscroll) }

  draw() {
    const ctx = this.ctx
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, W, VIEW_H)
    const sy = this.view.scrollY
    const page = this.tiles.get(0)
    if (page) {
      for (const [k, bmp] of page) {
        const [x, y] = k.split(':').map(Number)
        if (y + bmp.height < sy || y > sy + VIEW_H) continue
        ctx.drawImage(bmp, x, y - sy)
      }
    }
    const layers = [...state.layers].sort((a, b) => a.z - b.z || a.id - b.id)
    for (const l of layers) {
      const m = this.tiles.get(l.id)
      if (!m) continue
      const ly = layerScreenY(l, sy)
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
        const r = this.screenRect(this.hl.r)
        if (r) {
          ctx.fillStyle = 'rgba(30,120,255,' + (0.3 * (1 - age / 350)).toFixed(3) + ')'
          ctx.fillRect(r.x, r.y, r.w, r.h)
        }
        this.invalidate()
      } else this.hl = null
    }
  }
}
