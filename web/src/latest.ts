// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Only the newest reply counts. A page that loads on a tap (a folder, a month, a conversation) can be tapped again before the
// first answer is back; the slower answer then landed last and showed the wrong folder's list under the new folder's name. Each
// load takes a ticket, and its reply is used only while that ticket is still the newest.
//
//   const list = latest()
//   const t = list.take()
//   const r = await fetchIt()
//   if (!list.is(t)) return // a newer load has started: this reply is out of date
export function latest() {
  let n = 0
  return {
    take: () => ++n,
    now: () => n, // the newest ticket, without starting a new load (more of the same list: its reply still needs the check)
    is: (t: number) => t === n,
    drop: () => { n++ }, // what is in flight is out of date (the page moved on without loading)
  }
}
