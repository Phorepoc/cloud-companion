// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The app's state that follows the person from browser to browser (the desktop's windows, dock, widgets and notes), kept on
// the server (internal/webui/prefs.go). loadPrefs() reads it all once; setPref() changes it here at once and on the server
// shortly after, so typing does not send a request per key. What an older version kept in this browser is moved up once.
import type { ShellStorage } from '@etesian-design/shell'
import { apiCall } from './cloud'

const cache: Record<string, string> = {}
const timers: Record<string, ReturnType<typeof setTimeout>> = {}
let loaded: Promise<void> | null = null
/** writes reach the server only after it was read: a desktop started offline must not replace the kept one */
let synced = false

/** the localStorage keys of earlier versions, by the pref that replaces them */
const OLD: Record<string, string> = {
  'desktop.windows': 'phecloud-shell',
  'desktop.dock': 'phecloud-shell:dock',
  'desktop.widgets': 'phecloud-desktop-widgets',
  'desktop.notes': 'phecloud-desktop-notes',
}

export function loadPrefs(): Promise<void> {
  loaded ??= (async () => {
    const j = await apiCall('GET', '/phe/prefs')
    Object.assign(cache, j.prefs || {})
    synced = true
    for (const [k, old] of Object.entries(OLD)) {
      let v: string | null = null
      try { v = localStorage.getItem(old) } catch { /* private window */ }
      if (v === null) continue
      if (!(k in cache)) setPref(k, v)
      try { localStorage.removeItem(old) } catch { /* private window */ }
    }
  })().catch((e) => { loaded = null; throw e })
  return loaded
}

export function getPref(key: string): string | null {
  return key in cache ? cache[key] : null
}

export function setPref(key: string, value: string) {
  if (cache[key] === value) return
  cache[key] = value
  if (!synced) return
  clearTimeout(timers[key])
  timers[key] = setTimeout(() => { apiCall('PUT', '/phe/prefs/' + key, { value: cache[key] }).catch(() => { /* kept here; sent with the next change */ }) }, 500)
}

/** for the desktop (XShell): its windows and dock, under its own keys */
const SHELL: Record<string, string> = { 'phecloud-shell': 'desktop.windows', 'phecloud-shell:dock': 'desktop.dock' }
export const shellStorage: ShellStorage = {
  get: (k) => getPref(SHELL[k] ?? k),
  set: (k, v) => setPref(SHELL[k] ?? k, v),
}
