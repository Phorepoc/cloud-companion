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
  send({ t: 'tabs', tabs: all.map((x) => ({ id: x.id, title: x.title, url: x.url, active: x.active, loading: x.status === 'loading', mobile: mobile.has(x.id), asleep: !!x.discarded })) })
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
  async budget(m) { override = Math.max(0, Number(m.n) || 0); await fit() },
  async zoom(m) { await browser.tabs.setZoom((await active()).id, m.factor) },
}
async function dial() {
  const c = await browser.storage.managed.get(['url', 'secret', 'memoryMB']).catch(() => ({}))
  memoryMB = Number(c.memoryMB) || 2048
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
