// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Desktop widgets (after the Etesian Belle widgets): what kinds there are, their size on the 90 px grid, and where they are.
// The arrangement is kept on this device (localStorage), like the windows.

export type WidgetKind = 'clock-digital' | 'clock-analog' | 'clock-flip' | 'clock-word' | 'clock-gear' | 'clock-lcd' | 'weather' | 'mail' | 'mail-stack' | 'agenda' | 'month' | 'today'
  | 'chats' | 'contacts' | 'contact' | 'notes' | 'shortcuts' | 'feeds' | 'web' | 'devices' | 'music' | 'music-large'

/** A placed widget: its kind and its top-left grid cell. */
/** `opt` is the widget's own setting: the contact's name for a contact, the page for a web page widget. */
export interface Widget { id: string; kind: WidgetKind; x: number; y: number; opt?: string }

export const CELL = 90 // px per grid cell
export const MARGIN = 24 // px from the top-left of the desktop area to cell 0,0

export interface WidgetInfo { label: string; group: string; w: number; h: number }
/** The kinds, in the order the Add widgets sheet lists them; sizes in cells. */
export const KINDS: Record<WidgetKind, WidgetInfo> = {
  'clock-digital': { label: 'Big clock', group: 'Clocks', w: 4, h: 2 },
  'clock-analog': { label: 'Analog clock', group: 'Clocks', w: 2, h: 2 },
  'clock-flip': { label: 'Flip clock', group: 'Clocks', w: 4, h: 2 },
  'clock-word': { label: 'Clock in words', group: 'Clocks', w: 4, h: 2 },
  'clock-gear': { label: 'Gear clock', group: 'Clocks', w: 3, h: 3 },
  'clock-lcd': { label: 'LCD clock', group: 'Clocks', w: 4, h: 2 },
  weather: { label: 'Weather', group: 'Weather', w: 4, h: 2 },
  mail: { label: 'Mail', group: 'Mail', w: 4, h: 3 },
  'mail-stack': { label: 'Latest mail, one at a time', group: 'Mail', w: 4, h: 2 },
  agenda: { label: 'Agenda', group: 'Calendar', w: 4, h: 3 },
  month: { label: 'Month', group: 'Calendar', w: 3, h: 3 },
  today: { label: 'Today', group: 'Calendar', w: 4, h: 1 },
  chats: { label: 'Latest chats', group: 'Chats', w: 4, h: 2 },
  contacts: { label: 'Contacts', group: 'Contacts', w: 4, h: 1 },
  contact: { label: 'One contact', group: 'Contacts', w: 2, h: 2 },
  notes: { label: 'Notes', group: 'Notes', w: 4, h: 3 },
  shortcuts: { label: 'Shortcuts', group: 'Shortcuts', w: 4, h: 1 },
  web: { label: 'Web page', group: 'Shortcuts', w: 2, h: 2 },
  feeds: { label: 'Feeds', group: 'Feeds', w: 4, h: 3 },
  devices: { label: 'Devices', group: 'Devices', w: 4, h: 2 },
  music: { label: 'Music', group: 'Music', w: 4, h: 2 },
  'music-large': { label: 'Music, large', group: 'Music', w: 4, h: 3 },
}

const KEY = 'phecloud-desktop-widgets'
/** Shown until the user changes anything: the time, unread mail and the day's calendar. */
const DEFAULT: Widget[] = [
  { id: 'w1', kind: 'clock-digital', x: 0, y: 0 },
  { id: 'w2', kind: 'mail', x: 0, y: 2 },
  { id: 'w3', kind: 'agenda', x: 4, y: 0 },
]

export function loadWidgets(): Widget[] {
  try {
    const saved = localStorage.getItem(KEY)
    if (saved === null) return DEFAULT.map((w) => ({ ...w }))
    const list = JSON.parse(saved)
    return Array.isArray(list) ? list.filter((w: Widget) => w && w.id && w.kind in KINDS && Number.isInteger(w.x) && Number.isInteger(w.y)) : []
  } catch (e) { return [] }
}
export function saveWidgets(list: Widget[]) {
  try { localStorage.setItem(KEY, JSON.stringify(list)) } catch (e) { /* private mode */ }
}

/** How many cells fit in an area of w x h px. */
export const gridSize = (w: number, h: number) => ({ cols: Math.max(1, Math.floor((w - MARGIN) / CELL)), rows: Math.max(1, Math.floor((h - MARGIN) / CELL)) })

const overlaps = (a: Widget, b: Widget) => {
  const A = KINDS[a.kind], B = KINDS[b.kind]
  return a.x < b.x + B.w && b.x < a.x + A.w && a.y < b.y + B.h && b.y < a.y + A.h
}

/** Whether `w` fits inside the grid without covering another widget. */
export function fits(w: Widget, list: Widget[], cols: number, rows: number) {
  const k = KINDS[w.kind]
  return w.x >= 0 && w.y >= 0 && w.x + k.w <= cols && w.y + k.h <= rows && !list.some((o) => o.id !== w.id && overlaps(w, o))
}

/** The first free spot for a new widget, column by column from the top left (null when the desktop is full). */
export function freeSpot(kind: WidgetKind, list: Widget[], cols: number, rows: number): { x: number; y: number } | null {
  for (let x = 0; x < cols; x++) for (let y = 0; y < rows; y++) if (fits({ id: '', kind, x, y }, list, cols, rows)) return { x, y }
  return null
}
