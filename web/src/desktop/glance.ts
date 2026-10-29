// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The short summaries the desktop shows at a glance, in the Events panel and in the desktop widgets: the calendar around today,
// unread mail and the latest chats. Each loader throws a readable error; `quiet` turns "not set up" into one muted line.

import { apiCall, hubCall } from '../cloud'
import { addDays, toDate, ymd } from '../dates'
import type { CalendarEvent, MailItem, SmsConversation } from '../proto'

const NOT_SET_UP = /not set up|not configured/i
export const quiet = (e: Error, what: string) => (NOT_SET_UP.test(e.message) ? `${what} is not set up on this server.` : `Could not load: ${e.message}`)

/** The events of `days` days from the start of `now`'s day. */
export async function loadEvents(now: Date, days = 2): Promise<CalendarEvent[]> {
  const from = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  return (await hubCall({ op: 'events.list', from: ymd(from), to: ymd(addDays(from, days)) })).events || []
}

/** The day's events, all-day ones first, then by start time. */
export function eventsOn(events: CalendarEvent[], day: Date): CalendarEvent[] {
  const d = ymd(day)
  return events.filter((e) => ymd(toDate(e.start)) === d)
    .sort((a, b) => (a.allday ? -1 : b.allday ? 1 : toDate(a.start).getTime() - toDate(b.start).getTime()))
}

/** The inbox's unread count and its newest unread messages (up to `n`). */
export async function loadUnread(n = 3): Promise<{ unread: number; items: MailItem[] }> {
  const folders = (await hubCall({ op: 'mail.folders' })).folders || []
  const inbox = folders.find((f) => f.role === 'inbox')
  const unread = inbox?.unread || 0
  const items = inbox && unread ? ((await hubCall({ op: 'mail.list', folder: inbox.id, pos: 0 })).mails || []).filter((m) => m.unread).slice(0, n) : []
  return { unread, items }
}

/** The `n` most recent conversations. */
export async function loadChats(n = 3): Promise<SmsConversation[]> {
  const j: { conversations?: SmsConversation[] } = await apiCall('GET', '/sms/conversations')
  return [...(j.conversations || [])].sort((a, b) => b.last_ts - a.last_ts).slice(0, n)
}

/** "now", "5 min", "3 h", or the date, for a Unix time in seconds. */
export function ago(now: Date, ts: number) {
  const m = Math.max(0, Math.round((now.getTime() / 1000 - ts) / 60))
  return m < 1 ? 'now' : m < 60 ? m + ' min' : m < 1440 ? Math.round(m / 60) + ' h' : new Date(ts * 1000).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}
