// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Which hub folders are inboxes: the hub's own (role inbox) and each outside account's, which the connectors import into hub folders
// called "<account> / INBOX" (internal/connectors/mail.go). No imports, so web/test/inboxes.ts can test it with plain node.

export interface Inbox { account: string; id: string; unread: number }

export function pickInboxes(folders: { id: string; name: string; role?: string; unread?: number }[]): Inbox[] {
  const out: Inbox[] = []
  for (const f of folders) {
    const outside = /^(.+) \/ INBOX$/i.exec(f.name)
    if (f.role === 'inbox') out.push({ account: 'Inbox', id: f.id, unread: f.unread || 0 })
    else if (outside) out.push({ account: outside[1], id: f.id, unread: f.unread || 0 })
  }
  return out
}

/** Several inboxes' newest mails as one list, newest first, at most n. */
export const mergeNewest = <T extends { date: string }>(lists: T[][], n: number) =>
  lists.flat().sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()).slice(0, n)
