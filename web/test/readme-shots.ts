// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The README screenshots, with DEMO data only (invented numbers and messages). A fake phone (a second paired context) uploads a few
// text messages; the owner's browser (1440 x 900) shows Chats, the Browser page on a real site, and the desktop with windows open.
//   BASE=http://localhost:8080 CODES=<admin>,<plain> SITE_URL=https://example.com OUT=/out node --experimental-strip-types readme-shots.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import { openApp } from './chrome.ts'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const SITE = process.env.SITE_URL || 'https://en.wikipedia.org/wiki/Symbian'
const [ADMIN, PLAIN] = (process.env.CODES || '').split(',')
assert(ADMIN && PLAIN, 'CODES=<admin>,<plain> is required')
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const errors: string[] = []
const step = (s: string) => console.log('ok -', s)

async function pairIn(code: string, name: string, viewport: { width: number; height: number }) {
  const ctx = await browser.newContext({ viewport, colorScheme: 'light', bypassCSP: true })
  const page = await ctx.newPage()
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto(BASE + '/#/pair')
  await page.fill('[data-test=code]', code)
  await page.fill('[data-test=name]', name).catch(() => {})
  await page.click('[data-test=pair]')
  for (let i = 0; !(await page.evaluate(() => window.__phe?.state.status === 'online')); i++) { assert(i < 150, 'online'); await page.waitForTimeout(200) }
  return page
}
const owner = await pairIn(ADMIN, 'Laptop', { width: 1440, height: 900 })
const phone = await pairIn(PLAIN, 'Phone', { width: 400, height: 700 })
const sync = (body: Record<string, unknown>) => phone.evaluate((b) => window.__phe.apiCall('POST', '/sms/sync', b), body)
const now = Math.floor(Date.now() / 1000)
const m = (id: string, number: string, dir: 'in' | 'out', body: string, ago: number) => ({ id, number, dir, body, ts: now - ago, ...(dir === 'out' ? { status: 'delivered' } : {}) })
await sync({ messages: [
  m('d1', '+49 171 5550100', 'in', 'Are you coming to the barbecue on Saturday?', 7200),
  m('d2', '+49 171 5550100', 'out', 'Yes! I can bring the salad.', 7100),
  m('d3', '+49 171 5550100', 'in', 'Perfect. We start around four.', 7000),
  m('d4', '+49 171 5550100', 'out', 'See you then. Should I get ice on the way?', 3600),
  m('d5', '+49 171 5550100', 'in', 'Yes please, two bags. Thank you!', 3500),
  m('d6', '+49 176 5550123', 'in', 'Your parcel is out for delivery today.', 10000),
  m('d7', '+49 160 5550177', 'in', 'Dinner at eight at the Italian place?', 90000),
  m('d8', '+49 160 5550177', 'out', 'Sounds great, I booked a table.', 89000),
  m('d9', 'LIBRARY', 'in', 'Your book is ready for pickup until Friday.', 200000),
] })

// Chats
await owner.goto(BASE + '/#/chats')
await owner.waitForSelector('[data-test=conv]', { timeout: 15000 })
await owner.locator('[data-test=conv]', { hasText: 'two bags' }).click()
await owner.waitForFunction(() => document.querySelectorAll('[data-test=msg]').length === 5)
await owner.waitForTimeout(500)
await owner.screenshot({ path: `${OUT}/readme-chats.png`, clip: { x: 0, y: 0, width: 1440, height: 740 } })
step('chats')

// Wikipedia sometimes shows its donation banner (a dark purple box). It is dismissed with its "Maybe later" (and its X) in the
// stream, found by the colour of the page behind them, so the article fills the shot.
const bannerUp = (page: typeof owner, sel: string) => page.evaluate((sel) => {
  const c = document.querySelector(sel + ' canvas') as HTMLCanvasElement | null
  if (!c) return false
  const px = c.getContext('2d')?.getImageData(Math.floor(c.width * 0.35), Math.floor(c.height * 0.45), 1, 1).data
  return !!px && px[0] > 70 && px[0] < 130 && px[1] < 70 && px[2] > 70 && px[2] < 130
}, sel).catch(() => false)
async function noBanner(page: typeof owner, sel: string) {
  for (let i = 0; i < 3 && (await bannerUp(page, sel)); i++) {
    const b = (await page.locator(sel).first().boundingBox())!
    await page.mouse.click(b.x + b.width * 0.86, b.y + b.height * 0.665) // Maybe later
    await page.waitForTimeout(1500)
    if (await bannerUp(page, sel)) { await page.mouse.click(b.x + b.width * 0.964, b.y + b.height * 0.22); await page.waitForTimeout(1500) } // the X
  }
  assert(!(await bannerUp(page, sel)), 'the donation banner is dismissed')
}

// Browser: Firefox on the server, on a real site
await owner.goto(BASE + '/#/browse')
await owner.waitForSelector('.x-bf__row input', { timeout: 30000 })
const live = () => owner.evaluate(() => { const v = document.querySelector('[data-test=screen] video') as HTMLVideoElement | null; return !!v && v.videoWidth > 0 && v.currentTime > 0 })
await owner.waitForTimeout(6000)
const addr = '.x-bf__row input'
await owner.fill(addr, SITE); await owner.press(addr, 'Enter')
await owner.waitForTimeout(Number(process.env.SITE_WAIT || 15000))
await noBanner(owner, '[data-test=stage]')

await owner.screenshot({ path: `${OUT}/readme-browser.png` })
step('browser: ' + await owner.inputValue(addr) + ' / ' + await owner.locator('[data-test=title]').textContent({ timeout: 2000 }).catch(() => '?'))

// the desktop (1280 x 800), with overlapping windows: Chats (the conversation), the Browser on a real site, Files
await owner.setViewportSize({ width: 1280, height: 800 })
await owner.goto(BASE + '/#/chats')
await owner.waitForSelector('[data-test=conv]')
await openApp(owner, 'Desktop')
await owner.waitForSelector('[data-test=desktop]')
const win = (id: string) => `.x-win[data-win=${id}]`
const dockBtn = (label: string) => owner.locator(`.x-shell-dock button[aria-label="${label}"], .x-shell-dock button[aria-label^="${label}, "]`).first()
await owner.waitForSelector(win('chats'))
await dockBtn('Browser').click()
await owner.waitForSelector(win('browse'))
await owner.waitForSelector(`${win('browse')} .x-bf__row input`, { timeout: 30000 })
await owner.waitForTimeout(3000)
await owner.fill(`${win('browse')} .x-bf__row input`, SITE); await owner.press(`${win('browse')} .x-bf__row input`, 'Enter')
await owner.waitForTimeout(12000)
await noBanner(owner, `${win('browse')} [data-test=stage]`)
// the windows have no resize handles: their places, sizes and stacking (z) go to the desktop's stored layout, and the desktop is
// opened again with it -- the Browser at the back, Files over its corner, Chats in front, partly over both
const layout = [
  { id: 'browse', x: 300, y: 10, w: 960, h: 600, z: 1 },
  { id: 'files', x: 790, y: 400, w: 460, h: 356, z: 2 },
  { id: 'chats', x: 90, y: 250, w: 740, h: 500, z: 3 },
].map((l) => ({ ...l, snap: null, min: false }))
await owner.evaluate((v) => window.__phe.apiCall('PUT', '/phe/prefs/desktop.windows', { value: v }), JSON.stringify(layout))
await owner.waitForTimeout(1000)
await owner.reload()
await owner.waitForSelector(win('files'), { timeout: 20000 })
await owner.waitForSelector(`${win('chats')} [data-test=conv]`, { timeout: 20000 })
await owner.locator(`${win('chats')} [data-test=conv]`, { hasText: 'two bags' }).click()
// the Browser window comes back on a new tab: the site again
await owner.waitForSelector(`${win('browse')} .x-bf__row input`, { timeout: 30000 })
await owner.waitForTimeout(3000)
await owner.fill(`${win('browse')} .x-bf__row input`, SITE); await owner.press(`${win('browse')} .x-bf__row input`, 'Enter')
await owner.locator(`${win('chats')} [data-test=conv]`, { hasText: 'two bags' }).click()
await owner.waitForTimeout(12000)
await noBanner(owner, `${win('browse')} [data-test=stage]`)
await owner.mouse.move(640, 790)
await owner.screenshot({ path: `${OUT}/readme-desktop.png` })
assert.deepEqual(errors, [], 'no page errors')
await browser.close()
