// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The app's single connection to the cloud and the state of the current page. Components read `state`; the canvas view
// registers `onTile`/`onRedraw` hooks.
import { reactive, shallowRef, watch } from 'vue'
import { onResume } from './toast'
import { PheClient, type Creds } from './pheclient'
import { idbGet, idbSet, idbDel } from './idb'
import { brand } from './brand'
import type { DeviceInfo, Dialog, Focus, HubRequest, HubResFrame, Layer, PageInfo, PairCodeInfo, Region, ServerFrame, SessionInfo, TabInfo, TileFrame, WelcomeFrame } from './proto'

export interface AppState {
  status: 'offline' | 'connecting' | 'online'
  error: string
  dev: string
  admin: boolean
  page: PageInfo
  layers: Layer[]
  regions: Region[]
  focus: Focus
  loading: boolean
  dialogs: Dialog[]
  downloadNote: string
  deviceList: DeviceInfo[]
  adminError: string
  tabs: TabInfo[]
  activeView: number
  sessions: SessionInfo[]
  sent: { url: string; title: string; from: string }[] // pages other devices sent here, not answered yet
  features: string[]
  settings: { q: number; gray: boolean }
  viewport: { w: number; h: number; phone: boolean } // the size pages are laid out at in the cloud (the window's, or the phone's 360 x 640)
}

export const state = reactive<AppState>({
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
  sent: [],
  sessions: [],             // the connected devices and their tabs (for the "other devices" list)
  features: [],             // optional server features from the welcome frame (zoom, tabs)
  settings: { q: 70, gray: false },
  viewport: { w: 360, h: 640, phone: true }, // the size the cloud lays pages out at: the browser window's, or the phone's 360 x 640
})

/** What the canvas view (renderer.ts) listens to while it is shown. */
export const hooks: {
  onTile: ((f: TileFrame) => void) | null
  onNewPage: ((pid: number) => void) | null
  onRedraw: (() => void) | null
  onRescale: ((oldZoom: number, newZoom: number) => void) | null
} = { onTile: null, onNewPage: null, onRedraw: null, onRescale: null }
export const creds = shallowRef<Creds | null>(null)
let client: PheClient | null = null

export function wsUrl() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return proto + '//' + location.host + location.pathname.replace(/[^/]*$/, '') + 'ws'
}

export async function loadCreds() {
  creds.value = (await idbGet<Creds>('creds')) || null
  try { Object.assign(state.settings, JSON.parse(localStorage.getItem('phecloud-settings') || '{}')) } catch (e) { /* ignore */ }
  return creds.value
}

// view (tab) switching: the page state of the shown view is dropped; the server re-sends the new view in full
function showView(v: number) {
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

function onFrame(f: ServerFrame) {
  if (VIEW_FRAMES.includes(f.t) && 'v' in f && f.v && f.v !== state.activeView) return // another view's frame
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
      if (f.msg === REVOKED) { removed(); break }
      state.error = f.msg
      state.loading = false
      if (/admin only/.test(f.msg)) state.adminError = f.msg
      break
    case 'pong':
      break
    case 'sent':
      state.sent.push({ url: f.url, title: f.title || '', from: f.from })
      break
  }
}

async function finishConnect(w: WelcomeFrame) {
  state.dev = w.dev
  state.admin = !!w.admin
  state.features = w.features || []
  state.status = 'online'
  state.error = ''
  const c = client!
  c.onframe = onFrame
  c.onclose = () => { state.status = 'offline'; reconnectSoon() }
  retry = 0
  if (state.settings.q !== 70 || state.settings.gray) c.settings(state.settings.q, state.settings.gray)
  window.__phe = Object.assign(window.__phe || {}, { state, forget })
}

// The server went away (restarted for a backup or an update, or the network dropped): connect again, after 1 s, then less
// often up to every 30 s, until it answers. A device that was forgotten does not.
let retry = 0
let retryTimer: ReturnType<typeof setTimeout> | undefined
function reconnectSoon() {
  clearTimeout(retryTimer)
  if (!creds.value) return
  const wait = Math.min(30000, 1000 * 2 ** retry++)
  retryTimer = setTimeout(() => {
    if (state.status !== 'offline' || !creds.value) return
    connect().catch(() => reconnectSoon())
  }, wait)
}

// At start: a paired device connects whatever page it opens on (a reload on Settings stayed offline), and keeps trying while
// the server is not there.
export function connectAtStart() {
  if (creds.value) connect().catch(() => reconnectSoon())
}

// Pair with a code; stores the device key in IndexedDB.
export async function pair(code: string, name: string) {
  state.status = 'connecting'
  state.error = ''
  client = new PheClient()
  try {
    const w = await client.connect(wsUrl(), { code: code.trim().toUpperCase(), name })
    creds.value = { dev: w.dev, key: Uint8Array.from(w.key ?? []) }
    await idbSet('creds', creds.value)
    await finishConnect(w)
  } catch (e) {
    state.status = 'offline'
    state.error = (e as Error).message
    throw e
  }
}

// the connection being opened: callers that arrive meanwhile wait for it (a hub request sent before the server has signed
// the connection in is refused as coming from an unknown device)
let connecting: Promise<void> | null = null

export async function connect() {
  if (state.status === 'online') return
  if (connecting) return connecting
  if (state.status === 'connecting') return // pairing (pair() below) is opening it
  if (!creds.value) throw new Error('not paired')
  const c = creds.value
  connecting = (async () => {
    state.status = 'connecting'
    client = new PheClient()
    try {
      const w = await client.connect(wsUrl(), { creds: c })
      await finishConnect(w)
    } catch (e) {
      state.status = 'offline'
      state.error = (e as Error).message
      if (state.error === REVOKED) { removed(); return } // retrying would only count as failed sign-ins against this address
      throw e
    }
  })().finally(() => { connecting = null })
  return connecting
}

// The server's answer to a device it removed (internal/proto ErrRevoked), at sign-in or while connected.
const REVOKED = 'unknown or revoked device'

// This device was removed from the server (signed out from another device): its key is useless, so it is forgotten and the app
// goes back to pairing (App.vue), saying why.
function removed() {
  clearTimeout(retryTimer)
  state.error = 'This browser was signed out from another device. Pair it again to keep using it.' // before the pairing page opens
  forget()
}

// what Mail, Contacts and Calendar say when the server has no mail service (the hub) switched on
export const NO_HUB = "Mail, contacts and calendar isn't set up on this server. An admin can turn it on in Settings → Features."

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
  open: (u: string) => client && client.open(u, state.viewport.w, state.viewport.h, ''),
  viewport: (w: number, h: number) => client && client.viewport(w, h, ''),
  navigate: (u: string) => client && client.navigate(u),
  action: (a: string) => client && client.action(a),
  tap: (l: number, x: number, y: number, sy: number) => client && client.tap(l, x, y, sy),
  scroll: (y: number) => client && client.scroll(y),
  text: (id: number, t: string, r?: boolean) => client && client.text(id, t, r),
  key: (k: string) => client && client.key(k),
  select: (id: number, i: number) => client && client.select(id, i),
  zoom: (pct: number) => client && client.zoom(pct),
  tab: (action: string, v?: number) => client && client.tab(action, v),
  sessions: () => client && client.send({ t: 'sessions' }),
  remote: (msg: { sid: number; action: 'close' | 'open' | 'send'; view?: number; url?: string; title?: string }) => client && client.send({ t: 'remote', ...msg }),
  settings(q: number, gray: boolean) {
    state.settings.q = q
    state.settings.gray = gray
    localStorage.setItem('phecloud-settings', JSON.stringify(state.settings))
    client && client.settings(q, gray)
  },
  devices: () => client && client.devices(),
  revoke: (id: string) => client && client.revoke(id),
  answer(d: Dialog, ok: boolean, text?: string, pass?: string) {
    if (client) client.dialogReply(d.id, ok, text, pass)
    state.dialogs = state.dialogs.filter((x) => x.id !== d.id)
  },
}

// test/debug hook (the Playwright smoke test reads it)
window.__phe = Object.assign(window.__phe || {}, { state, api, hubCall: (m: HubRequest) => hubCall(m), apiCall: (m: string, p: string, b?: unknown) => apiCall(m, p, b) })

// ---- the personal hub (contacts, calendar): one request, one answer, matched by id ----
const hubPending = new Map<number, { resolve: (f: HubResFrame) => void; reject: (e: Error) => void }>()
let hubSeq = 0

export async function hubCall(msg: HubRequest) {
  if (state.status !== 'online') await connect()
  return new Promise<HubResFrame>((resolve, reject) => {
    if (!client) return reject(new Error('Not connected to the server.'))
    const id = ++hubSeq
    hubPending.set(id, { resolve, reject })
    client.send({ ...msg, t: 'hub', id })
    setTimeout(() => { if (hubPending.delete(id)) reject(new Error('The server did not answer. Try again.')) }, 25000)
  })
}

// open a page in a new tab of this browser (for "Open here" on another device's tab)
export async function openInNewTab(url: string) {
  if (!state.page.url || state.page.url === 'about:blank') { api.open(url); return } // a blank tab is used, not left behind
  const before = { n: state.tabs.length, v: state.activeView }
  api.tab('new')
  for (let i = 0; i < 80 && state.tabs.length <= before.n && state.activeView === before.v; i++) await new Promise((r) => setTimeout(r, 50))
  api.open(url)
}

// ---- owner sign-in: passkeys (WebAuthn) and the recovery code (server: internal/webui/auth.go) ----
const b64u = {
  dec: (s: string) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0)).buffer,
  enc: (b: ArrayBuffer) => btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''),
}

async function authCall(path: string, body?: unknown): Promise<any> {
  const r = await fetch(path, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  return j
}

export const passkeysSupported = () => !!(window.PublicKeyCredential && navigator.credentials)

export function authStatus() { return authCall('/passkey/status') }

// the device a sign-in produced is used like a paired one
async function adopt(w: { dev: string; key: string; recovery?: string }) {
  const key = Uint8Array.from((w.key.match(/../g) ?? []).map((h) => parseInt(h, 16)))
  creds.value = { dev: w.dev, key }
  await idbSet('creds', creds.value)
  await connect()
  return w.recovery || ''
}

// a WebAuthn credential as the server's JSON (PublicKeyCredential.toJSON where the browser has it)
function credJSON(c: PublicKeyCredential) {
  if ('toJSON' in c && typeof c.toJSON === 'function') return c.toJSON()
  const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse
  const out: Record<string, unknown> = { id: c.id, rawId: b64u.enc(c.rawId), type: c.type, clientExtensionResults: c.getClientExtensionResults() }
  out.response = r.attestationObject
    ? { attestationObject: b64u.enc(r.attestationObject), clientDataJSON: b64u.enc(r.clientDataJSON), transports: r.getTransports ? r.getTransports() : [] }
    : { authenticatorData: b64u.enc(r.authenticatorData), clientDataJSON: b64u.enc(r.clientDataJSON), signature: b64u.enc(r.signature), userHandle: r.userHandle ? b64u.enc(r.userHandle) : '' }
  return out
}

async function finish(path: string, sid: string, name: string, cred: Credential | null) {
  if (!cred) throw new Error('No passkey was made.')
  const r = await fetch(`${path}?sid=${sid}&name=${encodeURIComponent(name)}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(credJSON(cred as PublicKeyCredential)) })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  return j
}

// register a passkey with an admin pairing code; returns the recovery code (first passkey only, else '')
export async function passkeyRegister(code: string, name: string) {
  const { sid, options } = await authCall('/passkey/register/begin', { code })
  const pk = options.publicKey
  pk.challenge = b64u.dec(pk.challenge)
  pk.user.id = b64u.dec(pk.user.id)
  ;(pk.excludeCredentials || []).forEach((c: { id: string | ArrayBuffer }) => { c.id = b64u.dec(c.id as string) })
  const cred = await navigator.credentials.create({ publicKey: pk })
  return adopt(await finish('/passkey/register/finish', sid, name, cred))
}

export async function passkeyLogin(name: string) {
  const { sid, options } = await authCall('/passkey/login/begin', {})
  const pk = options.publicKey
  pk.challenge = b64u.dec(pk.challenge)
  ;(pk.allowCredentials || []).forEach((c: { id: string | ArrayBuffer }) => { c.id = b64u.dec(c.id as string) })
  const cred = await navigator.credentials.get({ publicKey: pk })
  await adopt(await finish('/passkey/login/finish', sid, name, cred))
}

// sign in with the recovery code; returns the NEW recovery code (the old one is used up)
export async function recoverLogin(code: string, name: string) {
  return adopt(await authCall('/passkey/recover', { code, name }))
}

// ---- Settings -> Security: calls signed with this browser's device key (an admin device) ----
// The X-Phe-Auth header (v2, internal/webui/auth.go): good for this one request, its address with the query, and its body
// (the text sent, or "u" for a multipart upload). A header seen on the network cannot be sent again.
async function signed(method: string, target: string, body: string | 'u' = '') {
  if (!creds.value || !crypto.subtle) throw new Error('This needs a secure page (https or localhost) and a signed-in browser.')
  const hex = (b: ArrayBuffer) => Array.from(new Uint8Array(b), (x) => x.toString(16).padStart(2, '0')).join('')
  const ts = String(Math.floor(Date.now() / 1000))
  const nonce = hex(crypto.getRandomValues(new Uint8Array(12)).buffer)
  const bh = body === 'u' ? 'u' : hex(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(body)))
  const k = await crypto.subtle.importKey('raw', creds.value.key as BufferSource, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const u = new URL(target, location.href) // as fetch will send it: the browser escapes some characters itself (an apostrophe in a query)
  const msg = `v2.${creds.value.dev}.${ts}.${nonce}.${bh}.${method} ${u.pathname}${u.search}`
  const sig = hex(await crypto.subtle.sign('HMAC', k, new TextEncoder().encode(msg)))
  return { headers: { 'X-Phe-Auth': `v2.${creds.value.dev}.${ts}.${nonce}.${bh}.${sig}` } }
}

async function signedJSON(method: string, path: string, body?: unknown): Promise<any> {
  const text = body === undefined ? '' : JSON.stringify(body)
  const s = await signed(method, path, text)
  // keepalive: a change sent as the page goes away (a delete whose Undo toast was still up) still reaches the server
  const r = await fetch(path, { method, headers: { ...s.headers, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : text, keepalive: method !== 'GET' && text.length < 60000 })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  return j
}

/** Settings, Pair a new device: a single-use code (admin devices only). */
export const pairCodeNew = (admin: boolean): Promise<PairCodeInfo> => signedJSON('POST', '/phe/pair/code', { admin })
export const passkeyList = () => signedJSON('GET', '/passkey/list')
export const passkeyRemove = (id: string) => signedJSON('POST', '/passkey/remove', { id })
export const recoveryRegenerate = () => signedJSON('POST', '/passkey/recovery', {})

// add a passkey while signed in as an admin: no pairing code
export async function passkeyAdd(name: string) {
  let s = await signed('POST', '/passkey/register/begin', '{}')
  let r = await fetch('/passkey/register/begin', { method: 'POST', headers: { ...s.headers, 'Content-Type': 'application/json' }, body: '{}' })
  let j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  const pk = j.options.publicKey
  pk.challenge = b64u.dec(pk.challenge)
  pk.user.id = b64u.dec(pk.user.id)
  ;(pk.excludeCredentials || []).forEach((c: { id: string | ArrayBuffer }) => { c.id = b64u.dec(c.id as string) })
  const cred = await navigator.credentials.create({ publicKey: pk })
  if (!cred) throw new Error('No passkey was made.')
  const path = `/passkey/register/finish?sid=${j.sid}&name=${encodeURIComponent(name)}`
  const text = JSON.stringify(credJSON(cred as PublicKeyCredential))
  s = await signed('POST', path, text)
  r = await fetch(path, { method: 'POST', headers: { ...s.headers, 'Content-Type': 'application/json' }, body: text })
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

// where the browser's tabs are, kept on this device (each device has its own screen)
export const browserPrefs = reactive({
  tabsAt: ((() => { try { return localStorage.getItem('phecloud-browser-tabs') } catch { return null } })() || 'top') as 'top' | 'side',
})
export function setBrowserPref<K extends keyof typeof browserPrefs>(k: K, v: (typeof browserPrefs)[K]) {
  browserPrefs[k] = v
  try { localStorage.setItem('phecloud-browser-tabs', String(v ?? '')) } catch { /* private window */ }
}

// the Connections API (admin devices; internal/webui/connections.go)
export const apiCall = (method: string, path: string, body?: unknown) => signedJSON(method, path, body)

// a signed multipart upload (the Catalogue page: installation files; internal/webui/catalogue.go)
export async function apiUpload(path: string, form: FormData) {
  const s = await signed('POST', path, 'u')
  const r = await fetch(path, { method: 'POST', headers: s.headers, body: form })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(j.error || 'The server did not accept that.')
  return j
}

// a delete or move whose Undo toast was still up when the page was closed or reloaded (toast.ts): done now
onResume<HubRequest>('hub.call', (r) => new Promise((ok) => { const stop = watch(() => state.status === 'online', (on) => { if (on) { queueMicrotask(() => stop()); ok(hubCall(r)) } }, { immediate: true }) }))
