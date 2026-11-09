// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The phone link's views (the cloud browser of old phones, internal/browser/firefox.go): each is a tab in its own small popup
// window behind the window the Browser page streams, laid out at the phone's width. Cloud Companion reads and drives the page
// from here; Firefox has no remote control turned on. Shares bg.js's scope (send, cmds, mobile, httpBase, secret).
const phone = new Map() // tab id -> {win, inflight: Set, last, hist: {list, i}, dir, commit: [resolve]}
const phoneWins = new Set()
const isPhoneTab = (id) => phone.has(id)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const tabOf = (m) => { const p = phone.get(m.tab); if (!p) throw new Error('no such view'); return p }

// what the pages are doing: nothing in flight for 60 ms is part of "settled"
browser.webRequest.onBeforeRequest.addListener((d) => {
  const p = phone.get(d.tabId); if (p) { p.inflight.add(d.requestId); p.last = Date.now() }
}, { urls: ['<all_urls>'] })
for (const e of ['onCompleted', 'onErrorOccurred']) browser.webRequest[e].addListener((d) => {
  const p = phone.get(d.tabId); if (p) { p.inflight.delete(d.requestId); p.last = Date.now() }
}, { urls: ['<all_urls>'] })

// history, for the phone's Back and Forward buttons
browser.webNavigation.onCommitted.addListener((d) => {
  const p = phone.get(d.tabId)
  if (!p) return
  if (d.frameId === 0) {
    const h = p.hist, q = d.transitionQualifiers || []
    if (q.includes('forward_back')) h.i = Math.max(0, Math.min(h.list.length - 1, h.i + (p.dir || 0)))
    else if (d.transitionType === 'reload') { /* same entry */ } else if (q.includes('client_redirect') && h.i >= 0) h.list[h.i] = d.url
    else if (!(d.url === 'about:blank' && h.i < 0)) { h.list = h.list.slice(0, h.i + 1); h.list.push(d.url); h.i++ }
    p.dir = 0
    for (const r of p.commit.splice(0)) r(true)
  }
  // a page's alert, confirm and prompt go to the phone (Firefox's own would stop the page in a window nobody sees)
  browser.tabs.executeScript(d.tabId, { code: DIALOGS(d.tabId), frameId: d.frameId, runAt: 'document_start' }).catch(() => {})
})
browser.webNavigation.onErrorOccurred.addListener((d) => {
  const p = phone.get(d.tabId)
  if (p && d.frameId === 0) for (const r of p.commit.splice(0)) r(false)
})
const DIALOGS = (tab) => `(() => { const base = ${JSON.stringify(httpBase)}, s = ${JSON.stringify(secret)}, W = window.wrappedJSObject
  const ask = (kind, msg, def) => { try {
    const x = new XMLHttpRequest()
    x.open('POST', base + '/phe/browser/ask?s=' + encodeURIComponent(s) + '&tab=${tab}&kind=' + kind + '&url=' + encodeURIComponent(location.href), false)
    x.send(JSON.stringify({ msg: String(msg ?? ''), def: String(def ?? '') }))
    return JSON.parse(x.responseText) } catch (e) { return { ok: false } } }
  W.alert = exportFunction((m) => { ask('alert', m) }, window)
  W.confirm = exportFunction((m) => !!ask('confirm', m).ok, window)
  W.prompt = exportFunction((m, d) => { const a = ask('prompt', m, d); return a.ok ? String(a.text ?? '') : null }, window)
})()`

// HTTP authentication: ask the phone
const asks = new Map()
let askN = 0
browser.webRequest.onAuthRequired.addListener((d) => {
  if (!isPhoneTab(d.tabId) || d.isProxy) return {}
  const id = 'a' + (++askN)
  send({ t: 'pask', id, tab: d.tabId, kind: 'auth', msg: d.challenger.host + (d.realm ? ' ' + d.realm : ''), url: d.url })
  return new Promise((resolve) => asks.set(id, (a) => resolve(a.ok ? { authCredentials: { username: a.text || '', password: a.pass || '' } } : { cancel: true })))
}, { urls: ['<all_urls>'] }, ['blocking'])

const frames2 = 'new Promise(r=>{const f=()=>requestAnimationFrame(()=>requestAnimationFrame(()=>r(true)));if(document.readyState==="complete")f();else addEventListener("load",f,{once:true});setTimeout(()=>r(true),1500)})'
const mainWindow = async () => (await browser.windows.getAll({ windowTypes: ['normal'] }))[0]

Object.assign(cmds, {
  async pOpen(m) {
    const was = await mainWindow()
    // the title is how openbox keeps it below the streamed window (deploy/browser/Dockerfile)
    const w = await browser.windows.create({ type: 'popup', url: 'about:blank', width: m.w, height: m.h, focused: false, titlePreface: 'Cloud Companion phone view ' })
    phoneWins.add(w.id)
    const t = w.tabs[0].id
    phone.set(t, { win: w.id, inflight: new Set(), last: Date.now(), hist: { list: [], i: -1 }, dir: 0, commit: [] })
    mobile.add(t)
    // the stream shows the main window: it gets the focus back, and stays in front
    if (was) await browser.windows.update(was.id, { focused: true }).catch(() => {})
    await browser.tabs.setZoomSettings(t, { scope: 'per-tab' }).catch(() => {})
    return t
  },
  async pClose(m) {
    const p = phone.get(m.tab)
    if (!p) return true
    phone.delete(m.tab); phoneWins.delete(p.win); mobile.delete(m.tab)
    await browser.windows.remove(p.win).catch(() => {})
    return true
  },
  // the page area is w x h device px, the layout w/zoom css px wide
  async pView(m) {
    const p = tabOf(m)
    let W = m.w, H = m.h
    for (let i = 0; i < 3; i++) {
      await browser.windows.update(p.win, { width: W, height: H })
      const t = await browser.tabs.get(m.tab)
      if (t.width === m.w && t.height === m.h) break
      W += m.w - t.width; H += m.h - t.height
    }
    if ((await browser.tabs.getZoom(m.tab)) !== m.zoom) await browser.tabs.setZoom(m.tab, m.zoom)
    return true
  },
  async pSite(m) {
    tabOf(m)
    if (m.mobile) mobile.add(m.tab); else mobile.delete(m.tab)
    if (m.reload) await browser.tabs.reload(m.tab, { bypassCache: true })
    return true
  },
  async pNav(m) {
    const p = tabOf(m)
    const committed = new Promise((r) => { p.commit.push(r); setTimeout(() => r(false), m.action ? 5000 : 30000) })
    if (m.action === 'back') { if (p.hist.i <= 0) throw new Error('no page to go to'); p.dir = -1; await browser.tabs.goBack(m.tab) }
    else if (m.action === 'forward') { if (p.hist.i >= p.hist.list.length - 1) throw new Error('no page to go to'); p.dir = 1; await browser.tabs.goForward(m.tab) }
    else if (m.action === 'reload') await browser.tabs.reload(m.tab)
    else await browser.tabs.update(m.tab, { url: m.url })
    await committed
    return true
  },
  async pSettle(m) {
    const p = tabOf(m)
    const end = Date.now() + 15000
    while (Date.now() < end) {
      const t = await browser.tabs.get(m.tab)
      if (t.status !== 'loading' && !p.inflight.size && Date.now() - p.last >= 60) break
      await sleep(20)
    }
    await browser.tabs.executeScript(m.tab, { code: frames2 }).catch(() => {})
    return true
  },
  async pEval(m) {
    tabOf(m)
    const [v] = await browser.tabs.executeScript(m.tab, { code: m.code, frameId: 0 })
    return v === undefined ? null : v
  },
  async pShot(m) {
    tabOf(m)
    return browser.tabs.captureTab(m.tab, { rect: { x: m.x, y: m.y, width: m.w, height: m.h }, scale: m.scale })
  },
  async pHist(m) {
    const h = tabOf(m).hist
    return { Back: h.i > 0, Fwd: h.i < h.list.length - 1 }
  },
  async pAnswer(m) {
    const f = asks.get(m.id)
    asks.delete(m.id)
    if (f) f(m)
    return true
  },
})
browser.tabs.onRemoved.addListener((id) => { const p = phone.get(id); if (p) { phone.delete(id); phoneWins.delete(p.win); mobile.delete(id) } })
