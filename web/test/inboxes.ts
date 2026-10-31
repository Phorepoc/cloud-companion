// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Unit test (plain node, no server): the mail widgets, Events and the menu bar read every inbox, the hub's own and each outside
// account's "<account> / INBOX" folder (regression 2026-10-31: only the hub's own inbox was read, so other accounts' mail never showed).
//   node --experimental-strip-types test/inboxes.ts      (from web/)
import assert from 'node:assert/strict'
import { pickInboxes, mergeNewest } from '../src/desktop/inboxes.ts'

const boxes = pickInboxes([
  { id: '1', name: 'Inbox', role: 'inbox', unread: 2 },
  { id: '2', name: 'Work / INBOX', unread: 5 },
  { id: '3', name: 'Work / Sent' },
  { id: '4', name: 'Gmail / Inbox', unread: 1 },
  { id: '5', name: 'Archive', role: 'archive' },
])
assert.deepEqual(boxes.map((b) => b.account), ['Inbox', 'Work', 'Gmail'], 'the hub inbox and each account\'s INBOX, nothing else')
assert.equal(boxes.reduce((t, b) => t + b.unread, 0), 8)
const merged = mergeNewest([[{ date: '2026-10-30T10:00:00Z', s: 'a' }], [{ date: '2026-10-31T09:00:00Z', s: 'b' }, { date: '2026-10-29T09:00:00Z', s: 'c' }]], 2)
assert.deepEqual(merged.map((m) => m.s), ['b', 'a'], 'newest first, at most n')
console.log('inboxes: all checks passed')
