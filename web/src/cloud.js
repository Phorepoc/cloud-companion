// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The app's single connection to the cloud and the state of the current page. Components read `state`; the canvas view
// registers `onTile`/`onRedraw` hooks.
import { reactive, shallowRef } from 'vue'
import { PheClient } from './pheclient.js'
import { idbGet, idbSet, idbDel } from './idb.js'

export const state = reactive({
  status: 'offline',        // offline | connecting | online
  error: '',
  dev: '',
  admin: false,
  page: { pid: 0, zoom: 100, url: '', title: '', w: 360, h: 640, back: false, fwd: false, trunc: false },
  layers: [],
  regions: [],
  focus: { kind: 'none', id: 0, type: '', value: '' },
  loading: false,
  dialogs: [],              // pending JS/auth/cert dialogs
  downloadNote: '',
  deviceList: [],
  adminError: '',
  tabs: [],                 // the views of this connection: [{v, title, url}]
  activeView: 1,
  features: [],             // optional server features from the welcome frame (zoom, tabs)
  settings: { q: 70, gray: false },
})

export const hooks = { onTile: null, onNewPage: null, onRedraw: null, onRescale: null }
export const creds = shallowRef(null)
let client = null

export function wsUrl() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return proto + '//' + location.host + location.pathname.replace(/[^/]*$/, '') + 'ws'
}

export async function loadCreds() {
  creds.value = (await idbGet('creds')) || null
  try { Object.assign(state.settings, JSON.parse(localStorage.getItem('phecloud-settings') || '{}')) } catch (e) { /* ignore */ }
  return creds.value
}

// view (tab) switching: the page state of the shown view is dropped; the server re-sends the new view in full
function showView(v) {
  state.activeView = v
  if (client) client.view = v
  state.page.pid = 0
  state.page.zoom = 100
  state.page.title = ''
  state.page.url = ''
  state.layers = []
  state.regions = []
  state.focus = { kind: 'none', id: 0, type: '', value: '' }
  state.loading = false
  if (hooks.onNewPage) hooks.onNewPage(0)
}

const VIEW_FRAMES = ['page', 'tile', 'layers', 'regions', 'focus', 'done']

function onFrame(f) {
  if (VIEW_FRAMES.includes(f.t) && f.v && f.v !== state.activeView) return // another view's frame
  switch (f.t) {
    case 'tabs':
      state.tabs = f.list || []
      if (f.active && f.active !== state.activeView) showView(f.active)
      break
    case 'page': {
      const newPage = f.pid !== state.page.pid
      const oldZ = state.page.zoom || 100
      const newZ = f.zoom || 100
      Object.assign(state.page, { pid: f.pid, zoom: newZ, url: f.url, title: f.title, w: f.w, h: f.h, back: !!f.back, fwd: !!f.fwd, trunc: !!f.trunc })
      if (newPage && hooks.onNewPage) hooks.onNewPage(f.pid)
      else if (f.reset && hooks.onRescale) hooks.onRescale(oldZ, newZ) // same page at another render scale
      document.title = f.title ? f.title + ' - Phecloud' : 'Phecloud'
      state.loading = true
      break
    }
    case 'tile':
      if (hooks.onTile) hooks.onTile(f)
      break
    case 'layers':
      state.layers = f.list || []
      if (hooks.onRedraw) hooks.onRedraw()
      break
    case 'regions':
      state.regions = f.list || []
      break
    case 'focus':
      Object.assign(state.focus, { kind: f.kind, id: f.id || 0, type: f.type || '', value: f.value || '', start: f.start, end: f.end, im: f.im || '', enter: f.enter || '' })
      break
    case 'done':
      if (!f.more) state.loading = false
      if (hooks.onRedraw) hooks.onRedraw()
      break
    case 'dialog':
      state.dialogs.push({ id: f.id, kind: f.kind, message: f.msg, def: f.def || '' })
      break
    case 'download':
      state.downloadNote = 'Download refused by the cloud: ' + (f.name || f.url)
      break
    case 'devicelist':
      state.deviceList = f.list || []
      state.adminError = ''
      break
    case 'error':
      state.error = f.msg
      state.loading = false
      if (/admin only/.test(f.msg)) state.adminError = f.msg
      break
    case 'pong':
      break
  }
}

async function finishConnect(w) {
  state.dev = w.dev
  state.admin = !!w.admin
  state.features = w.features || []
  state.status = 'online'
  state.error = ''
  client.onframe = onFrame
  client.onclose = () => { state.status = 'offline' }
  if (state.settings.q !== 70 || state.settings.gray) client.settings(state.settings.q, state.settings.gray)
  window.__phe = Object.assign(window.__phe || {}, { state })
}

// Pair with a code; stores the device key in IndexedDB.
export async function pair(code, name) {
  state.status = 'connecting'
  state.error = ''
  client = new PheClient()
  try {
    const w = await client.connect(wsUrl(), { code: code.trim().toUpperCase(), name })
    creds.value = { dev: w.dev, key: Uint8Array.from(w.key) }
    await idbSet('creds', creds.value)
    await finishConnect(w)
  } catch (e) {
    state.status = 'offline'
    state.error = e.message
    throw e
  }
}

export async function connect() {
  if (state.status === 'online' || state.status === 'connecting') return
  if (!creds.value) throw new Error('not paired')
  state.status = 'connecting'
  client = new PheClient()
  try {
    const w = await client.connect(wsUrl(), { creds: creds.value })
    await finishConnect(w)
  } catch (e) {
    state.status = 'offline'
    state.error = e.message
    throw e
  }
}

export async function forget() {
  if (client) client.close()
  client = null
  await idbDel('creds')
  creds.value = null
  state.status = 'offline'
  state.dev = ''
  state.admin = false
}

export const api = {
  open: (u) => client && client.open(u),
  navigate: (u) => client && client.navigate(u),
  action: (a) => client && client.action(a),
  tap: (l, x, y, sy) => client && client.tap(l, x, y, sy),
  scroll: (y) => client && client.scroll(y),
  text: (id, t, r) => client && client.text(id, t, r),
  key: (k) => client && client.key(k),
  select: (id, i) => client && client.select(id, i),
  zoom: (pct) => client && client.zoom(pct),
  tab: (action, v) => client && client.tab(action, v),
  settings(q, gray) {
    state.settings.q = q
    state.settings.gray = gray
    localStorage.setItem('phecloud-settings', JSON.stringify(state.settings))
    client && client.settings(q, gray)
  },
  devices: () => client && client.devices(),
  revoke: (id) => client && client.revoke(id),
  answer(d, ok, text, pass) {
    if (client) client.dialogReply(d.id, ok, text, pass)
    state.dialogs = state.dialogs.filter((x) => x.id !== d.id)
  },
}

// test/debug hook (the Playwright smoke test reads it)
window.__phe = Object.assign(window.__phe || {}, { state })
