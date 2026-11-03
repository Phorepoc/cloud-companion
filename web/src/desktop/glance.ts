// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The short summaries the desktop shows at a glance, in the Events panel and in the desktop widgets: the calendar around today,
// unread mail and the latest chats. Each loader throws a readable error; `quiet` turns "not set up" into one muted line.

import { apiCall, hubCall } from '../cloud'
import { addDays, toDate, ymd } from '../dates'
import type { CalendarEvent, Contact, MailItem, SmsConversation } from '../proto'
import { pickInboxes, mergeNewest } from './inboxes'

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

const inboxes = async () => pickInboxes((await hubCall({ op: 'mail.folders' })).folders || [])

/** The accounts with an inbox, for the mail widgets' setting. */
export const mailAccounts = async () => (await inboxes()).map((i) => i.account)

/** The unread count and the newest messages, read or not (up to `n`, as the Belle Mail widget lists them), of one account's inbox,
 *  or of every inbox merged newest first when `account` is ''. */
export async function loadMail(n = 20, account = ''): Promise<{ unread: number; items: MailItem[] }> {
  const boxes = (await inboxes()).filter((i) => !account || i.account === account)
  const lists = await Promise.all(boxes.map(async (b) => ((await hubCall({ op: 'mail.list', folder: b.id, pos: 0 })).mails || []).slice(0, n)))
  const items = mergeNewest(lists, n)
  return { unread: boxes.reduce((t, b) => t + b.unread, 0), items }
}

/** Every inbox's unread count and the newest unread messages (up to `n`), for Events and the menu bar. */
export async function loadUnread(n = 3): Promise<{ unread: number; items: MailItem[] }> {
  const all = await loadMail(40)
  return { unread: all.unread, items: all.items.filter((m) => m.unread).slice(0, n) }
}

/** Contacts, by name (people first: entries without a name are left out). */
export async function loadContacts(): Promise<Contact[]> {
  return ((await hubCall({ op: 'contacts.list' })).contacts || []).filter((c) => c.name).sort((a, b) => a.name.localeCompare(b.name))
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
