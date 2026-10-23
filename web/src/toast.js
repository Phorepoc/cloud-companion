// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Toasts, as STYLE.md section 5 describes them: only for the result of something the person just did, 4 seconds, an optional
// "Undo". Errors are never toasts (they are inline banners). Rendered by components/ToastHost.vue.
import { reactive } from 'vue'

export const toasts = reactive([])
let next = 1

// toast('Contact saved'), toast('Contact deleted', { undo: () => restore(), onExpire: () => reallyDelete() })
// With `onExpire` the action is deferred: it runs when the toast ends, unless Undo was pressed first.
export function toast(message, opts = {}) {
  const t = { id: next++, message, undo: opts.undo || null, onExpire: opts.onExpire || null, ms: opts.ms || 4000, done: false }
  toasts.push(t)
  return t.id
}

export function closeToast(id, undone = false) {
  const i = toasts.findIndex((t) => t.id === id)
  if (i < 0) return
  const t = toasts[i]
  toasts.splice(i, 1)
  if (t.done) return
  t.done = true
  if (undone) { if (t.undo) t.undo() } else if (t.onExpire) t.onExpire()
}
