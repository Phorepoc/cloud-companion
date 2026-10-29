// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Layer geometry and hit-testing: the same rules as the C core (phc_hit_test): the topmost layer under the point wins,
// then the smallest tap region inside it. Fixed layers are tapped in layer coordinates, scroll layers in content
// coordinates, page and sticky points in document coordinates.
import type { Layer, Region } from './proto'

export const VIEW_H = 640

/** Local scroll of the scroll layers, by layer id. */
export type LocalScroll = Record<number, { x: number; y: number }>
export interface Rect { x: number; y: number; w: number; h: number }

// screen y of a layer's top edge for the page scroll `scroll`
export function layerScreenY(l: Layer, scroll: number, viewH = VIEW_H) {
  if (l.kind === 'fixed') return l.y
  let y = l.y - scroll
  if (l.kind === 'scroll') return y
  if (l.top != null) { if (y < l.top) y = l.top }
  else if (l.bottom != null) { if (y + l.h > viewH - l.bottom) y = viewH - l.bottom - l.h }
  return y
}

// lscroll: {layerId: {x, y}} local scroll of scroll layers
export function hitTest(layers: Layer[], regions: Region[], scroll: number, lscroll: LocalScroll, sx: number, sy: number, viewH = VIEW_H) {
  let top: Layer | null = null
  for (const l of layers) {
    const y = layerScreenY(l, scroll, viewH)
    if (sx < l.x || sx >= l.x + l.w || sy < y || sy >= y + l.h) continue
    if (!top || l.z > top.z || (l.z === top.z && l.id > top.id)) top = l
  }
  let layer = 0, lx = sx, ly = sy + scroll
  if (top) {
    layer = top.id
    lx = sx - top.x
    ly = sy - layerScreenY(top, scroll, viewH)
    if (top.kind === 'scroll') {
      const s = lscroll[top.id] || { x: top.sx || 0, y: top.sy || 0 }
      lx += s.x; ly += s.y
    }
  }
  let best: Region | null = null
  for (const r of regions) {
    if (r.layer !== layer) continue
    if (lx < r.x || lx >= r.x + r.w || ly < r.y || ly >= r.y + r.h) continue
    if (!best || r.w * r.h < best.w * best.h) best = r
  }
  const tap = top && top.kind !== 'sticky' ? { layer, x: lx, y: ly } : { layer: 0, x: sx, y: sy + scroll }
  return { layer, region: best, tap }
}

// screen rectangle of a region (for the keyboard overlay and the tap highlight)
export function regionScreenRect(layers: Layer[], r: Region, scroll: number, lscroll: LocalScroll, viewH = VIEW_H): Rect | null {
  if (r.layer === 0) return { x: r.x, y: r.y - scroll, w: r.w, h: r.h }
  const l = layers.find((q) => q.id === r.layer)
  if (!l) return null
  const sy = layerScreenY(l, scroll, viewH)
  if (l.kind === 'scroll') {
    const s = lscroll[l.id] || { x: l.sx || 0, y: l.sy || 0 }
    return { x: l.x + r.x - s.x, y: sy + r.y - s.y, w: r.w, h: r.h }
  }
  return { x: l.x + r.x, y: sy + r.y, w: r.w, h: r.h }
}
