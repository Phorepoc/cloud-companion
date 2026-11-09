// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// Firefox's state for the Browser page, and the page's commands for Firefox, through Cloud Companion's server.
let ws
const send = (m) => { if (ws && ws.readyState === 1) ws.send(JSON.stringify(m)) }
async function tabs() {
  const all = await browser.tabs.query({})
  send({ t: 'tabs', tabs: all.map((x) => ({ id: x.id, title: x.title, url: x.url, active: x.active, loading: x.status === 'loading' })) })
}
for (const e of ['onCreated', 'onRemoved', 'onActivated', 'onUpdated', 'onMoved']) browser.tabs[e].addListener(tabs)
const nav = (state) => (d) => { if (!d.frameId) send({ t: 'nav', state, tab: d.tabId, url: d.url }) }
browser.webNavigation.onBeforeNavigate.addListener(nav('start'))
browser.webNavigation.onCommitted.addListener(nav('commit'))
browser.webNavigation.onCompleted.addListener(nav('done'))
browser.webNavigation.onErrorOccurred.addListener(nav('error'))
browser.downloads.onCreated.addListener((d) => send({ t: 'download', state: 'start', id: d.id, url: d.url }))
browser.downloads.onChanged.addListener((d) => { if (d.state) send({ t: 'download', state: d.state.current, id: d.id }) })
const active = async () => (await browser.tabs.query({ active: true, currentWindow: true }))[0]
const sendZoom = async (id) => send({ t: 'zoom', factor: await browser.tabs.getZoom(id) })
browser.tabs.onZoomChange.addListener((z) => send({ t: 'zoom', factor: z.newZoomFactor }))
browser.tabs.onActivated.addListener((i) => sendZoom(i.tabId))
const cmds = {
  async newTab(m) { await browser.tabs.create(m.url ? { url: m.url } : {}) },
  async close(m) { await browser.tabs.remove(m.id) },
  async pick(m) { await browser.tabs.update(m.id, { active: true }) },
  async go(m) { await browser.tabs.update((await active()).id, { url: m.url }) },
  async back() { await browser.tabs.goBack((await active()).id) },
  async forward() { await browser.tabs.goForward((await active()).id) },
  async reload() { await browser.tabs.reload((await active()).id) },
  async zoom(m) { await browser.tabs.setZoom((await active()).id, m.factor) },
}
async function dial() {
  const c = await browser.storage.managed.get(['url', 'secret']).catch(() => ({}))
  if (!c.url) return setTimeout(dial, 3000)
  ws = new WebSocket(c.url + '?s=' + encodeURIComponent(c.secret))
  ws.onopen = async () => { tabs(); const a = await active(); if (a) sendZoom(a.id) }
  ws.onmessage = async (e) => {
    const m = JSON.parse(e.data)
    try { await cmds[m.c](m) } catch (err) { send({ t: 'error', c: m.c, error: String(err) }) }
  }
  ws.onclose = () => setTimeout(dial, 1000)
}
dial()
