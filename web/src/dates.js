// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Date helpers for the calendar. Event times are ISO 8601 strings: "2026-10-24" (all day), "2026-10-24T09:00:00" (floating local
// time) or with a trailing Z (UTC). The inputs of the form work in local date ("YYYY-MM-DD") and time ("HH:MM").
export const pad = (n) => String(n).padStart(2, '0')
export const ymd = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
export const hm = (d) => `${pad(d.getHours())}:${pad(d.getMinutes())}`

// an ISO string as a local Date (UTC values are converted, floating and date-only values are taken as local)
export function toDate(iso) {
  if (!iso) return null
  if (iso.length === 10) { const [y, m, d] = iso.split('-').map(Number); return new Date(y, m - 1, d) }
  return new Date(iso)
}

export function addDays(date, n) { const d = new Date(date); d.setDate(d.getDate() + n); return d }

// the form fields of an event
export function toFields(ev) {
  const s = toDate(ev.start)
  let e = toDate(ev.end || ev.start)
  if (ev.allday) { // the calendar stores an exclusive end date; the form shows the last day
    e = addDays(e, -1)
    if (e < s) e = s
    return { allday: true, startDate: ymd(s), startTime: '09:00', endDate: ymd(e), endTime: '10:00', }
  }
  return { allday: false, startDate: ymd(s), startTime: hm(s), endDate: ymd(e), endTime: hm(e) }
}

// the ISO strings for the form fields
export function fromFields(f) {
  if (f.allday) return { start: f.startDate, end: ymd(addDays(toDate(f.endDate || f.startDate), 1)) }
  const mk = (d, t) => `${d}T${t || '00:00'}:00` // local time of the browser; the zone goes with the event (TZID)
  return { start: mk(f.startDate, f.startTime), end: mk(f.endDate || f.startDate, f.endTime || f.startTime) }
}

export const monthLabel = (d) => d.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
export const dayLabel = (d) => d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
export const timeLabel = (d) => d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })

export const browserZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || ''

// Monday-first month grid: 6 weeks of days that contain `month` (a Date on the 1st)
export function gridDays(month) {
  const first = new Date(month.getFullYear(), month.getMonth(), 1)
  const lead = (first.getDay() + 6) % 7
  const start = addDays(first, -lead)
  return Array.from({ length: 42 }, (_, i) => addDays(start, i))
}
