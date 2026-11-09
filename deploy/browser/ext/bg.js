// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// Firefox's state for the Browser page, and the page's commands for Firefox, through Cloud Companion's server.
let ws
// "Desktop site" off: these tabs get the phone version of sites (Firefox for Android's user agent, in the requests and to scripts)
const mobile = new Set()
const MOBILE_UA = navigator.userAgent.replace(/\([^)]*rv:([\d.]+)\) Gecko\/\d+/, '(Android 14; Mobile; rv:$1) Gecko/$1')
const send = (m) => { if (ws && ws.readyState === 1) ws.send(JSON.stringify(m)) }
async function tabs() {
  const all = await browser.tabs.query({})
  const marked = new Set()
  for (const x of all) if (x.url && /^https?:/.test(x.url) && (await browser.bookmarks.search({ url: x.url }).catch(() => [])).length) marked.add(x.id)
  send({ t: 'tabs', tabs: all.map((x) => ({ starred: marked.has(x.id), id: x.id, title: x.title, url: x.url, security: security.get(x.id) || (x.url && x.url.startsWith('https:') ? 'secure' : 'none'), active: x.active, loading: x.status === 'loading', mobile: mobile.has(x.id), asleep: !!x.discarded })) })
}
for (const e of ['onCreated', 'onRemoved', 'onActivated', 'onUpdated', 'onMoved']) browser.tabs[e].addListener(tabs)
browser.tabs.onRemoved.addListener((id) => mobile.delete(id))
browser.webRequest.onBeforeSendHeaders.addListener((d) => {
  if (!mobile.has(d.tabId)) return
  for (const h of d.requestHeaders) if (h.name.toLowerCase() === 'user-agent') h.value = MOBILE_UA
  return { requestHeaders: d.requestHeaders }
}, { urls: ['<all_urls>'] }, ['blocking', 'requestHeaders'])
const UA_PAGE = `(() => { const ua = ${JSON.stringify(MOBILE_UA)}, P = window.wrappedJSObject.Navigator.prototype
  const get = (v) => ({ get: exportFunction(() => v, window), configurable: true })
  Reflect.defineProperty(P, 'userAgent', get(ua)); Reflect.defineProperty(P, 'platform', get('Linux armv8l'))
  Reflect.defineProperty(P, 'appVersion', get(ua.slice(8))); Reflect.defineProperty(P, 'maxTouchPoints', get(5)) })()`
browser.webNavigation.onCommitted.addListener((d) => {
  if (mobile.has(d.tabId)) browser.tabs.executeScript(d.tabId, { code: UA_PAGE, frameId: d.frameId, runAt: 'document_start' }).catch(() => {})
})
// Tabs are never refused: above a number that fits this container's memory, the background tabs used longest ago are unloaded
// (they stay in the list, and load again when picked). The number comes from the memory, or from Settings, Browser.
let memoryMB = 0, override = 0
const autoBudget = () => Math.max(2, Math.min(60, Math.floor((memoryMB - 700) / 150)))
const budget = () => override > 0 ? override : autoBudget()
let fitting
async function fit() {
  if (fitting) return
  fitting = true
  try {
    const all = await browser.tabs.query({})
    const loaded = all.filter((t) => !t.discarded)
    const spare = loaded.filter((t) => !t.active).sort((a, b) => a.lastAccessed - b.lastAccessed)
    const drop = spare.slice(0, Math.max(0, loaded.length - budget()))
    if (drop.length) await browser.tabs.discard(drop.map((t) => t.id)).catch(() => {})
    send({ t: 'mem', memoryMB, auto: autoBudget(), budget: budget(), override, open: all.length, loaded: loaded.length - drop.length })
  } finally { fitting = false }
}
browser.tabs.onActivated.addListener(fit)
browser.tabs.onCreated.addListener(fit)
browser.tabs.onRemoved.addListener(fit)
// how safe each tab's page is, for the lock in the address bar: secure, insecure (http), broken (a certificate Firefox refused
// or one with problems): Firefox's own view of the connection, not just the address
const security = new Map()
browser.tabs.onRemoved.addListener((id) => security.delete(id))
browser.webRequest.onHeadersReceived.addListener(async (d) => {
  if (d.tabId < 0) return
  if (!d.url.startsWith('https:')) { security.set(d.tabId, 'insecure'); return }
  const i = await browser.webRequest.getSecurityInfo(d.requestId, {}).catch(() => null)
  security.set(d.tabId, i && i.state === 'secure' ? 'secure' : 'broken')
  tabs()
}, { urls: ['<all_urls>'], types: ['main_frame'] }, ['blocking'])
browser.webRequest.onErrorOccurred.addListener((d) => {
  if (d.tabId >= 0 && /SEC_ERROR|SSL_ERROR|MOZILLA_PKIX|CERT/i.test(d.error)) { security.set(d.tabId, 'broken'); tabs() }
}, { urls: ['<all_urls>'], types: ['main_frame'] })
// bookmarks and history are Firefox's own, in the profile (a Cloud Companion volume, in its backups); the Browser page shows them
for (const e of ['onCreated', 'onRemoved', 'onChanged']) browser.bookmarks[e].addListener(() => tabs())
const pick = (n) => ({ id: n.id, title: n.title || '', url: n.url })
const nav = (state) => (d) => { if (!d.frameId) send({ t: 'nav', state, tab: d.tabId, url: d.url }) }
browser.webNavigation.onBeforeNavigate.addListener(nav('start'))
browser.webNavigation.onCommitted.addListener(nav('commit'))
browser.webNavigation.onCompleted.addListener(nav('done'))
browser.webNavigation.onErrorOccurred.addListener(nav('error'))
browser.downloads.onCreated.addListener((d) => send({ t: 'download', state: 'start', id: d.id, url: d.url }))
browser.downloads.onChanged.addListener(async (d) => {
  if (!d.state) return
  const [it] = await browser.downloads.search({ id: d.id }).catch(() => [])
  send({ t: 'download', state: d.state.current, id: d.id, name: it ? it.filename.split('/').pop() : '' })
})
// a page's file input: the Browser page asks on the device, the files come back through Cloud Companion
const picks = new Map() // id -> [tab, frame]
browser.runtime.onMessage.addListener((m, sender) => {
  if (!m.pick) return
  picks.set(m.pick.id, [sender.tab.id, sender.frameId])
  send({ t: 'pick', ...m.pick, site: sender.url ? new URL(sender.url).host : '' })
})
let httpBase = '', secret = ''
const active = async () => (await browser.tabs.query({ active: true, currentWindow: true }))[0]
const sendZoom = async (id) => send({ t: 'zoom', factor: await browser.tabs.getZoom(id) })
browser.tabs.onZoomChange.addListener((z) => send({ t: 'zoom', factor: z.newZoomFactor }))
browser.tabs.onActivated.addListener((i) => sendZoom(i.tabId))
const cmds = {
  async newTab(m) {
    if (!m.mobile) return void await browser.tabs.create(m.url ? { url: m.url } : {})
    const t = await browser.tabs.create({}) // marked before its first request goes out
    mobile.add(t.id)
    if (m.url) await browser.tabs.update(t.id, { url: m.url })
    tabs()
  },
  async site(m) {
    const id = m.id ?? (await active()).id
    if (m.mobile) mobile.add(id); else mobile.delete(id)
    tabs()
    await browser.tabs.reload(id, { bypassCache: true })
  },
  async close(m) { await browser.tabs.remove(m.id) },
  async pick(m) { await browser.tabs.update(m.id, { active: true }) },
  async go(m) { await browser.tabs.update((await active()).id, { url: m.url }) },
  async back() { await browser.tabs.goBack((await active()).id) },
  async forward() { await browser.tabs.goForward((await active()).id) },
  async reload() { await browser.tabs.reload((await active()).id) },
  async files(m) {
    const at = picks.get(m.id)
    picks.delete(m.id)
    send({ t: 'picked', id: m.id })
    if (!at) return
    const files = []
    for (const f of m.files || []) {
      const r = await fetch(`${httpBase}/phe/browser/file/${f.tok}?s=${encodeURIComponent(secret)}`)
      if (r.ok) files.push({ name: f.name, type: f.type, data: await r.arrayBuffer() })
    }
    await browser.tabs.sendMessage(at[0], { files, id: m.id }, { frameId: at[1] })
  },
  async bookmark(m) {
    const have = await browser.bookmarks.search({ url: m.url })
    if (m.on && !have.length) await browser.bookmarks.create({ title: m.title || m.url, url: m.url })
    if (!m.on) for (const b of have) await browser.bookmarks.remove(b.id)
  },
  async marks(m) {
    const all = (await browser.bookmarks.search(m.q ? { query: m.q } : {})).filter((b) => b.url)
    send({ t: 'marks', list: all.reverse().slice(0, 500).map(pick) })
  },
  async unmark(m) { await browser.bookmarks.remove(m.id); await cmds.marks(m) },
  async history(m) {
    const all = await browser.history.search({ text: m.q || '', startTime: 0, maxResults: 300 })
    send({ t: 'history', list: all.map((h) => ({ url: h.url, title: h.title || '', at: h.lastVisitTime })) })
  },
  async forget(m) { if (m.url) await browser.history.deleteUrl({ url: m.url }); else await browser.history.deleteAll(); await cmds.history(m) },
  async budget(m) { override = Math.max(0, Number(m.n) || 0); await fit() },
  async zoom(m) { await browser.tabs.setZoom((await active()).id, m.factor) },
}
async function dial() {
  const c = await browser.storage.managed.get(['url', 'secret', 'memoryMB']).catch(() => ({}))
  memoryMB = Number(c.memoryMB) || 2048
  secret = c.secret || ''
  httpBase = c.url ? c.url.replace(/^ws/, 'http').replace(/\/phe\/browser\/ext$/, '') : ''
  if (!c.url) return setTimeout(dial, 3000)
  ws = new WebSocket(c.url + '?s=' + encodeURIComponent(c.secret))
  ws.onopen = async () => { tabs(); fit(); const a = await active(); if (a) sendZoom(a.id) }
  ws.onmessage = async (e) => {
    const m = JSON.parse(e.data)
    try { await cmds[m.c](m) } catch (err) { send({ t: 'error', c: m.c, error: String(err) }) }
  }
  ws.onclose = () => setTimeout(dial, 1000)
}
dial()
