// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The phone link's views (the cloud browser of old phones, internal/browser/firefox.go): each is a tab in its own small popup
// window behind the window the Browser page streams, laid out at the phone's width. Cloud Companion reads and drives the page
// from here; Firefox has no remote control turned on. Shares bg.js's scope (send, cmds, mobile, httpBase, secret).
const phone = new Map() // tab id -> {win, inflight: Set, last, hist: {list, i}, dir, commit: [resolve]}
const phoneWins = new Set()
const isPhoneTab = (id) => phone.has(id)
// one Firefox container per paired device: its own cookies, so sign-ins never cross between devices or into the Browser page
const stores = new Map() // device -> cookieStoreId
async function storeFor(dev) {
  if (!dev || !browser.contextualIdentities) return undefined
  if (stores.has(dev)) return stores.get(dev)
  try {
    const name = 'Phone ' + dev
    const have = await browser.contextualIdentities.query({ name })
    const id = have.length ? have[0] : await browser.contextualIdentities.create({ name, color: 'blue', icon: 'fingerprint' })
    stores.set(dev, id.cookieStoreId); phoneStores.add(id.cookieStoreId)
    return id.cookieStoreId
  } catch (e) { return undefined }
}
if (browser.contextualIdentities) browser.contextualIdentities.query({}).then((l) => { for (const c of l) if (c.name.startsWith('Phone ')) phoneStores.add(c.cookieStoreId) }).catch(() => {})
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

// a certificate Firefox refuses: the phone is asked (kind "cert"); the reply of pNav / pSettle says so ({cert: {url, text}})
browser.webRequest.onErrorOccurred.addListener((d) => {
  const p = phone.get(d.tabId)
  if (p && /SEC_ERROR|SSL_ERROR|MOZILLA_PKIX|CERT/i.test(d.error)) p.cert = { url: d.url, text: d.error }
}, { urls: ['<all_urls>'], types: ['main_frame'] })
const certOf = (p) => { const c = p.cert; p.cert = null; return c ? { cert: c } : true }

// downloads of a view's container are the phone's: reported once the file has a name; the file goes to Files, Downloads
const phoneDownloads = new Map() // download id -> view tab
const phoneReported = new Set()
function phoneDownload(d) {
  let best = null
  for (const [id, p] of phone) if (p.store === d.cookieStoreId && (!best || p.last > best[1].last)) best = [id, p]
  if (!best) return
  phoneDownloads.set(d.id, best[0])
  for (const r of best[1].commit.splice(0)) r(true) // the navigation was a download: it is over
  reportDownload(d.id)
}
async function reportDownload(id) {
  if (phoneReported.has(id)) return
  const [it] = await browser.downloads.search({ id }).catch(() => [])
  if (!it || !it.filename) return
  phoneReported.add(id)
  send({ t: 'pdl', tab: phoneDownloads.get(id), url: String(it.url).slice(0, 2000), name: it.filename.split('/').pop() })
}
function phoneDownloadChanged(d) { if (d.filename || d.state) reportDownload(d.id) }

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
    const store = await storeFor(m.dev)
    // the title is how openbox keeps it below the streamed window (deploy/browser/Dockerfile); until the title is set the window
    // would show in front, so it opens beyond the screen's edge and comes in once it is titled
    const opts = { type: 'popup', width: m.w, height: m.h, left: 6000, top: 0, focused: false, titlePreface: 'Cloud Companion phone view ' }
    let w, t
    if (store) { // a tab of the device's container, moved into its own window
      const nt = await browser.tabs.create({ cookieStoreId: store, active: false, url: 'about:blank' })
      w = await browser.windows.create({ ...opts, tabId: nt.id }); t = nt.id
    } else { w = await browser.windows.create({ ...opts, url: 'about:blank' }); t = w.tabs[0].id }
    phoneWins.add(w.id)
    phone.set(t, { win: w.id, store, inflight: new Set(), last: Date.now(), hist: { list: [], i: -1 }, dir: 0, commit: [], cert: null })
    mobile.add(t)
    // the stream shows the main window: if opening the view took the focus, it gets it back
    if (was && !(await mainWindow()).focused && was.focused) await browser.windows.update(was.id, { focused: true }).catch(() => {})
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
    return certOf(p)
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
    return certOf(p)
  },
  // a tap Firefox delivers itself (trusted); false: this Firefox has no privileged path, the caller clicks inside the page
  async pClick(m) {
    tabOf(m)
    if (!browser.phe) return false
    return browser.phe.click(m.tab, m.x, m.y)
  },
  // "continue anyway" for the refused certificate the view shows (this site, until Firefox restarts)
  async pCert(m) {
    tabOf(m)
    if (!browser.phe) throw new Error('no privileged path')
    const r = await browser.phe.acceptCert(m.tab)
    if (r !== 'ok') throw new Error('certificate: ' + r)
    return true
  },
  async pCookies(m) {
    const p = tabOf(m)
    if (!p.store) return []
    const all = await browser.cookies.getAll({ storeId: p.store })
    const same = { no_restriction: 'None', lax: 'Lax', strict: 'Strict' } // the shape the old engine exported
    return all.map((c) => ({ name: c.name, value: c.value, domain: c.domain, path: c.path, expires: c.expirationDate || -1, size: c.name.length + c.value.length,
      httpOnly: c.httpOnly, secure: c.secure, session: !!c.session, sameSite: same[c.sameSite] || '' }))
  },
  async pSetCookies(m) {
    const p = tabOf(m)
    if (!p.store) return true
    const same = { None: 'no_restriction', Lax: 'lax', Strict: 'strict' }
    for (const c of m.cookies || []) {
      const host = c.domain.replace(/^\./, '')
      const o = { url: (c.secure ? 'https://' : 'http://') + host + (c.path || '/'), name: c.name, value: c.value, path: c.path || '/', secure: !!c.secure,
        httpOnly: !!c.httpOnly, storeId: p.store }
      if (c.domain.startsWith('.')) o.domain = c.domain
      if (same[c.sameSite]) o.sameSite = same[c.sameSite]
      if (!c.session && c.expires > 0) o.expirationDate = c.expires
      await browser.cookies.set(o).catch(() => {})
    }
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
