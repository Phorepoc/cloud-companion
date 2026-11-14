// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Toasts, as STYLE.md section 5 describes them: only for the result of something the person just did, 4 seconds, an optional
// "Undo". Errors are never toasts (they are inline banners). Rendered by components/ToastHost.vue.
import { reactive } from 'vue'

export interface Toast { id: number; message: string; undo: (() => void) | null; onExpire: (() => void) | null; ms: number; done: boolean }
export interface ToastOptions { undo?: () => void; onExpire?: () => void; ms?: number; resume?: [string, unknown] }

export const toasts = reactive<Toast[]>([])
let next = 1

// toast('Contact saved'), toast('Contact deleted', { undo: () => restore(), onExpire: () => reallyDelete() })
// With `onExpire` the action is deferred: it runs when the toast ends, unless Undo was pressed first.
export function toast(message: string, opts: ToastOptions = {}) {
  const t: Toast = { id: next++, message, undo: opts.undo || null, onExpire: opts.onExpire || null, ms: opts.ms || 4000, done: false }
  toasts.push(t)
  if (opts.resume) keepAction(t.id, opts.resume)
  return t.id
}

export function closeToast(id: number, undone = false) {
  const i = toasts.findIndex((t) => t.id === id)
  if (i < 0) return
  const t = toasts[i]
  toasts.splice(i, 1)
  if (t.done) return
  t.done = true
  dropAction(id)
  if (undone) { if (t.undo) t.undo() } else if (t.onExpire) t.onExpire()
}

// Leaving or reloading the page mid-toast: the deferred action (a delete) still happens, as the toast promised; before, it was
// dropped with the page and the item came back. A signed request can't be made while the page goes, so a toast that names its
// action ({ resume: ['feeds.remove', url] }) keeps it in this tab's session storage, and the app does it when it opens again.
const PENDING = 'phe-pending-actions'
type Pending = [string, unknown]
const handlers: Record<string, (data: never) => Promise<unknown>> = {}
const kept = new Map<number, Pending>()
function save() { try { sessionStorage.setItem(PENDING, JSON.stringify([...kept.values()])) } catch { /* no storage: as before */ } }
/** What a kept action does; registered by the module that owns it, and run for whatever a closed page left. */
export function onResume<T>(kind: string, run: (data: T) => Promise<unknown>) {
  handlers[kind] = run as (data: never) => Promise<unknown>
  let left: Pending[] = []
  try { left = JSON.parse(sessionStorage.getItem(PENDING) || '[]') } catch { /* nothing */ }
  const mine = left.filter(([k]) => k === kind)
  if (!mine.length) return
  try { sessionStorage.setItem(PENDING, JSON.stringify(left.filter(([k]) => k !== kind))) } catch { /* nothing */ }
  for (const [, d] of mine) run(d as T).catch(() => { /* gone already, or refused: the list shows what is there */ })
}
export function keepAction(id: number, p: Pending) { kept.set(id, p); save() }
export function dropAction(id: number) { if (kept.delete(id)) save() }
