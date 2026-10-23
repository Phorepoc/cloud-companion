// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The app's single connection to the cloud and the state of the current page. Components read `state`; the canvas view
// registers `onTile`/`onRedraw` hooks.
import { reactive, shallowRef } from 'vue'
import { PheClient } from './pheclient.js'
import { idbGet, idbSet, idbDel } from './idb.js'
import { brand } from './brand.js'

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
  dialogs: [],              // pending JS/passkey/cert dialogs
  downloadNote: '',
  deviceList: [],
  adminError: '',
  tabs: [],                 // the views of this connection: [{v, title, url}]
  activeView: 1,
  sessions: [],             // the connected devices and their tabs (for the "other devices" list)
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
    case 'hubres': {
      const p = hubPending.get(f.id)
      if (p) {
        hubPending.delete(f.id)
        if (f.ok) p.resolve(f)
        else p.reject(new Error(f.err || 'The hub did not accept that.'))
      }
      break
    }
    case 'sessionlist':
      state.sessions = f.list || []
      break
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
      document.title = f.title ? f.title + ' - ' + brand.name : brand.name
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
  window.__phe = Object.assign(window.__phe || {}, { state, forget })
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
  sessions: () => client && client.send({ t: 'sessions' }),
  remote: (msg) => client && client.send({ t: 'remote', ...msg }),
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
window.__phe = Object.assign(window.__phe || {}, { state, hubCall: (m) => hubCall(m) })

// ---- the personal hub (contacts, calendar): one request, one answer, matched by id ----
const hubPending = new Map()
let hubSeq = 0

export async function hubCall(msg) {
  if (state.status !== 'online') await connect()
  return new Promise((resolve, reject) => {
    if (!client) return reject(new Error('Not connected to the server.'))
    const id = ++hubSeq
    hubPending.set(id, { resolve, reject })
    client.send({ t: 'hub', id, ...msg })
    setTimeout(() => { if (hubPending.delete(id)) reject(new Error('The server did not answer. Try again.')) }, 25000)
  })
}

// open a page in a new tab of this browser (for "Open here" on another device's tab)
export async function openInNewTab(url) {
  const before = { n: state.tabs.length, v: state.activeView }
  api.tab('new')
  for (let i = 0; i < 80 && state.tabs.length <= before.n && state.activeView === before.v; i++) await new Promise((r) => setTimeout(r, 50))
  api.open(url)
}

// ---- owner sign-in: passkeys (WebAuthn) and the recovery code (server: internal/webui/auth.go) ----
const b64u = {
  dec: (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0)).buffer,
  enc: (b) => btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''),
}

async function authCall(path, body) {
  const r = await fetch(path, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  return j
}

export const passkeysSupported = () => !!(window.PublicKeyCredential && navigator.credentials)

export function authStatus() { return authCall('/passkey/status') }

// the device a sign-in produced is used like a paired one
async function adopt(w) {
  const key = Uint8Array.from(w.key.match(/../g).map((h) => parseInt(h, 16)))
  creds.value = { dev: w.dev, key }
  await idbSet('creds', creds.value)
  await connect()
  return w.recovery || ''
}

function credJSON(c) {
  if (c.toJSON) return c.toJSON()
  const r = c.response
  const out = { id: c.id, rawId: b64u.enc(c.rawId), type: c.type, clientExtensionResults: c.getClientExtensionResults() }
  out.response = r.attestationObject
    ? { attestationObject: b64u.enc(r.attestationObject), clientDataJSON: b64u.enc(r.clientDataJSON), transports: r.getTransports ? r.getTransports() : [] }
    : { authenticatorData: b64u.enc(r.authenticatorData), clientDataJSON: b64u.enc(r.clientDataJSON), signature: b64u.enc(r.signature), userHandle: r.userHandle ? b64u.enc(r.userHandle) : '' }
  return out
}

async function finish(path, sid, name, cred) {
  const r = await fetch(`${path}?sid=${sid}&name=${encodeURIComponent(name)}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(credJSON(cred)) })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  return j
}

// register a passkey with an admin pairing code; returns the recovery code (first passkey only, else '')
export async function passkeyRegister(code, name) {
  const { sid, options } = await authCall('/passkey/register/begin', { code })
  const pk = options.publicKey
  pk.challenge = b64u.dec(pk.challenge)
  pk.user.id = b64u.dec(pk.user.id)
  ;(pk.excludeCredentials || []).forEach((c) => { c.id = b64u.dec(c.id) })
  const cred = await navigator.credentials.create({ publicKey: pk })
  return adopt(await finish('/passkey/register/finish', sid, name, cred))
}

export async function passkeyLogin(name) {
  const { sid, options } = await authCall('/passkey/login/begin', {})
  const pk = options.publicKey
  pk.challenge = b64u.dec(pk.challenge)
  ;(pk.allowCredentials || []).forEach((c) => { c.id = b64u.dec(c.id) })
  const cred = await navigator.credentials.get({ publicKey: pk })
  await adopt(await finish('/passkey/login/finish', sid, name, cred))
}

// sign in with the recovery code; returns the NEW recovery code (the old one is used up)
export async function recoverLogin(code, name) {
  return adopt(await authCall('/passkey/recover', { code, name }))
}

// ---- Settings -> Security: calls signed with this browser's device key (an admin device) ----
async function signed(method, path, body) {
  if (!creds.value || !crypto.subtle) throw new Error('This needs a secure page (https or localhost) and a signed-in browser.')
  const ts = String(Math.floor(Date.now() / 1000))
  const k = await crypto.subtle.importKey('raw', creds.value.key, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const sig = new Uint8Array(await crypto.subtle.sign('HMAC', k, new TextEncoder().encode(`${creds.value.dev}.${ts}.${method} ${path}`)))
  const hdr = { 'X-Phe-Auth': `${creds.value.dev}.${ts}.${Array.from(sig, (b) => b.toString(16).padStart(2, '0')).join('')}` }
  return { headers: hdr, body }
}

async function signedJSON(method, path, body) {
  const s = await signed(method, path)
  const r = await fetch(path, { method, headers: { ...s.headers, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  return j
}

export const passkeyList = () => signedJSON('GET', '/passkey/list')
export const passkeyRemove = (id) => signedJSON('POST', '/passkey/remove', { id })
export const recoveryRegenerate = () => signedJSON('POST', '/passkey/recovery', {})

// add a passkey while signed in as an admin: no pairing code
export async function passkeyAdd(name) {
  let s = await signed('POST', '/passkey/register/begin')
  let r = await fetch('/passkey/register/begin', { method: 'POST', headers: { ...s.headers, 'Content-Type': 'application/json' }, body: '{}' })
  let j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  const pk = j.options.publicKey
  pk.challenge = b64u.dec(pk.challenge)
  pk.user.id = b64u.dec(pk.user.id)
  ;(pk.excludeCredentials || []).forEach((c) => { c.id = b64u.dec(c.id) })
  const cred = await navigator.credentials.create({ publicKey: pk })
  const path = '/passkey/register/finish'
  s = await signed('POST', path)
  r = await fetch(`${path}?sid=${j.sid}&name=${encodeURIComponent(name)}`, { method: 'POST', headers: { ...s.headers, 'Content-Type': 'application/json' }, body: JSON.stringify(credJSON(cred)) })
  const f = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(f.error || 'The server did not accept that.')
  return f.recovery || ''
}

// passkeys need a secure origin and a domain name (not an IP address)
export function passkeyBlocker() {
  const h = location.hostname
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(h) || h.includes(':')) return 'You opened this page by an IP address. Passkeys only work with a domain name (for example cloud.example.org or localhost), because a passkey belongs to the name of the site. Open the page by its name to use passkeys.'
  if (!window.isSecureContext) return 'This page is not secure (http). Passkeys need https, or the address localhost.'
  if (!window.PublicKeyCredential) return 'This browser does not support passkeys.'
  return ''
}

// the Connections API (admin devices; internal/webui/connections.go)
export const apiCall = (method, path, body) => signedJSON(method, path, body)
