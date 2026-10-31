// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The six snap layouts of the Etesian desktop (etesian-design explorations/desktop2/geometry.ts LAYOUTS; Plasma's etesian_snaplayouts
// uses the same): zones as fractions of the desktop area.
import type { Rect } from './types'

export interface SnapLayout { label: string; zones: Rect[] }
export const LAYOUTS: SnapLayout[] = [
  { label: '1/2 + 1/2', zones: [{ x: 0, y: 0, w: 0.5, h: 1 }, { x: 0.5, y: 0, w: 0.5, h: 1 }] },
  { label: 'Three columns', zones: [{ x: 0, y: 0, w: 1 / 3, h: 1 }, { x: 1 / 3, y: 0, w: 1 / 3, h: 1 }, { x: 2 / 3, y: 0, w: 1 / 3, h: 1 }] },
  { label: '2/3 + 1/3', zones: [{ x: 0, y: 0, w: 2 / 3, h: 1 }, { x: 2 / 3, y: 0, w: 1 / 3, h: 1 }] },
  { label: 'Focus centre', zones: [{ x: 0, y: 0, w: 0.25, h: 1 }, { x: 0.25, y: 0, w: 0.5, h: 1 }, { x: 0.75, y: 0, w: 0.25, h: 1 }] },
  { label: 'Main + stack', zones: [{ x: 0, y: 0, w: 0.5, h: 1 }, { x: 0.5, y: 0, w: 0.5, h: 0.5 }, { x: 0.5, y: 0.5, w: 0.5, h: 0.5 }] },
  { label: 'Quarters', zones: [{ x: 0, y: 0, w: 0.5, h: 0.5 }, { x: 0.5, y: 0, w: 0.5, h: 0.5 }, { x: 0, y: 0.5, w: 0.5, h: 0.5 }, { x: 0.5, y: 0.5, w: 0.5, h: 0.5 }] },
]

/** A zone in pixels inside a w × h area, with `gap` round and between the zones (geometry.ts zoneRects). */
export function zoneRect(layout: number, zone: number, w: number, h: number, gap: number): Rect | null {
  const z = LAYOUTS[layout]?.zones[zone]
  if (!z) return null
  const x0 = Math.round(z.x * w), x1 = Math.round((z.x + z.w) * w), y0 = Math.round(z.y * h), y1 = Math.round((z.y + z.h) * h)
  const l = x0 + (z.x === 0 ? gap : gap / 2), r = x1 - (z.x + z.w >= 0.999 ? gap : gap / 2)
  const t = y0 + (z.y === 0 ? gap : gap / 2), b = y1 - (z.y + z.h >= 0.999 ? gap : gap / 2)
  return { x: l, y: t, w: r - l, h: b - t }
}
