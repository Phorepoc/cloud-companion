// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The Browser page's connection to Firefox: the picture and input through Neko's client, the tabs and navigation through
// our own extension (deploy/browser/ext). Anything the user does shows at once in `busy`, before the answer comes.
import { reactive } from 'vue'
import { NekoClient, mount } from '@m1k1o/neko'
import { apiCall } from './cloud'

export interface FxTab { id: number; title: string; url: string; active: boolean; loading: boolean; mobile?: boolean; starred?: boolean; security?: 'secure' | 'insecure' | 'broken' | 'none' }
export const engine = reactive({
  status: 'idle' as 'idle' | 'insecure' | 'unconfigured' | 'connecting' | 'live' | 'failed',
  up: false, tabs: [] as FxTab[], busy: false, zoom: 1, note: '', saved: '',
  marks: [] as { id: string; title: string; url: string }[],
  history: [] as { url: string; title: string; at: number }[],
  passkey: null as null | { id: string; kind: 'create' | 'get'; origin: string; site: string; options: Record<string, unknown> },
  pick: null as null | { id: string; accept: string; multiple: boolean; site: string }, // a page asks for files
})
;(window as any).__pheBrowser = engine // tests read the tab list from here
let client: NekoClient | null = null, ws: WebSocket | null = null, unmount: (() => void) | null = null
let stopped = true, watchdog: ReturnType<typeof setTimeout> | undefined, busyTimer: ReturnType<typeof setTimeout> | undefined
let box: HTMLElement | null = null
let pending = '' // a page to open once Firefox's extension is there (one sent from another device)

export const activeTab = () => engine.tabs.find((t) => t.active)
export function command(m: Record<string, unknown>) {
  if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(m))
}
/** shows at once that something is happening; the answer, or five seconds, ends it */
export function act(m: Record<string, unknown>) {
  engine.busy = true
  clearTimeout(busyTimer); busyTimer = setTimeout(() => { engine.busy = false }, 5000)
  command(m)
}
/** opens a page in a new tab of Firefox: now, or as soon as the Browser page is connected */
export function openPage(url: string) {
  if (ws?.readyState === WebSocket.OPEN && engine.up) act({ c: 'newTab', url })
  else pending = url
}
function flush() { if (pending && ws?.readyState === WebSocket.OPEN && engine.up) { const u = pending; pending = ''; act({ c: 'newTab', url: u }) } }
/** keys go to the page again (after the address field had them) */
export function focusPage() { box?.querySelector('textarea')?.focus({ preventScroll: true }) }

function onBridge(e: MessageEvent) {
  const m = JSON.parse(e.data)
  if (m.t === 'tabs') { engine.tabs = m.tabs; if (!m.tabs.some((t: FxTab) => t.loading)) engine.busy = false }
  else if (m.t === 'engine') { engine.up = m.up; flush() }
  else if (m.t === 'zoom') engine.zoom = m.factor
  else if (m.t === 'marks') engine.marks = m.list
  else if (m.t === 'history') engine.history = m.list
  else if (m.t === 'passkey') engine.passkey = { id: m.id, kind: m.kind, origin: m.origin, site: m.site, options: m.options }
  else if (m.t === 'passkeyDone') { if (engine.passkey?.id === m.id) engine.passkey = null }
  else if (m.t === 'pick') engine.pick = { id: m.id, accept: m.accept, multiple: !!m.multiple, site: m.site || '' }
  else if (m.t === 'picked') { if (engine.pick?.id === m.id) engine.pick = null }
  else if (m.t === 'nav') { if (m.state === 'start') engine.busy = true; else if (m.state !== 'commit') engine.busy = false }
  else if (m.t === 'download') {
    engine.saved = m.state === 'complete' ? m.name || '' : ''
    engine.note = m.state === 'complete' ? `Saved in Files, Downloads${m.name ? ': ' + m.name : ''}` : m.state === 'interrupted' ? 'Download failed' : 'Downloading…'
  }
}
function openBridge(ticket: string) {
  ws?.close()
  ws = new WebSocket(`${location.origin.replace(/^http/, 'ws')}/phe/browser/bridge?t=${encodeURIComponent(ticket)}`)
  ws.onmessage = onBridge
  ws.onclose = () => { if (!stopped) setTimeout(() => void refreshBridge(), 1000) }
}
async function refreshBridge() {
  try { const j = await apiCall('GET', '/phe/browser/ticket'); if (j.configured && !stopped) openBridge(j.bridge) }
  catch { if (!stopped) setTimeout(() => void refreshBridge(), 3000) }
}
const videoLive = () => { const v = box?.querySelector('video'); return !!v && v.videoWidth > 0 && v.currentTime > 0 }

/** the largest screen of the browser's that fits the space; what is left stays as a thin band */
export function fitScreen(w: number, h: number) {
  const list = (client?.state as any)?.screen?.configurations as { width: number; height: number; rate: number }[] | undefined
  if (!client?.controlling || !list?.length) return
  const fits = list.filter((c) => c.width <= w && c.height <= h)
  const pick = (fits.length ? fits : list).reduce((b, c) => (fits.length ? c.width * c.height > b.width * b.height : c.width * c.height < b.width * b.height) ? c : b)
  const cur = (client.state as any).screen?.size
  if (cur?.width !== pick.width || cur?.height !== pick.height) client.setScreenSize(pick.width, pick.height, pick.rate)
}

export async function start(el: HTMLElement) {
  stopped = false; box = el
  if (!window.isSecureContext) { engine.status = 'insecure'; return } // this device's requests can't be signed on plain http
  engine.status = 'connecting'
  // the first try can come before this device's key is ready or while Firefox still starts: keep trying as long as the page is open
  const retry = () => { engine.status = 'failed'; client = null; watchdog = setTimeout(() => { if (!stopped) void start(el) }, 3000) }
  let j: any
  try { j = await apiCall('GET', '/phe/browser/ticket') }
  catch { if (!stopped) retry(); return }
  if (stopped) return
  if (!j.configured) { engine.status = 'unconfigured'; return }
  openBridge(j.bridge)
  client = new NekoClient({ autoplay: true } as any)
  client.setUrl(`${location.origin}/phe/browser/neko/?token=${encodeURIComponent(j.token)}`)
  try { await client.authenticate() }
  catch { if (!stopped) retry(); return }
  if (stopped) return
  client.connect()
  unmount = mount(client, el) as unknown as () => void
  client.store.subscribe((s: any) => s.connection.status, (st: string) => { if (st === 'connected') client?.request() })
  // no picture for ten seconds is never left as a black box: it says so meanwhile, then tries again
  let since = Date.now()
  const check = () => {
    if (stopped) return
    if (videoLive()) { engine.status = 'live'; since = Date.now() }
    else if (Date.now() - since > 10000) { stop(); void start(el); return }
    else if (engine.status === 'live') engine.status = 'connecting'
    watchdog = setTimeout(check, 250)
  }
  check()
}
export function stop() {
  stopped = true
  clearTimeout(watchdog); clearTimeout(busyTimer)
  ws?.close(); ws = null
  unmount?.(); unmount = null
  client?.disconnect(); client = null
  engine.status = 'idle'
}
