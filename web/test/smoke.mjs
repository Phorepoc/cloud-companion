// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright test of the web app, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node smoke.mjs
// Environment: QUICK=1 stops after the tap test (used for the TLS proxy check); IGNORE_TLS=1 accepts a self-signed proxy cert.
//
// Pairs, opens example.com, taps the link on the canvas, tests zoom, then a page with an input, a select and three
// JavaScript dialogs (keyboard overlay typing and Backspace, select chooser, alert/confirm/prompt), HTTP auth (wrong then
// right password), the devices page (admin), settings, and a reload that re-authenticates from the stored key.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
const QUICK = process.env.QUICK === '1'
assert(CODE, 'CODE (a pairing code, preferably from `phecloud paircode --admin`) is required')

const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 900, height: 900 }, ignoreHTTPSErrors: process.env.IGNORE_TLS === '1' })
const page = await ctx.newPage()
const log = []
page.on('console', (m) => log.push(m.type() + ': ' + m.text()))
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const S = (expr) => page.evaluate(expr)
const title = async () => (await page.textContent('[data-test=title]')).trim()
const waitTitle = (t, ms = 30000) => page.waitForFunction((x) => window.__phe.state.page.title === x, t, { timeout: ms })
const idle = () => page.waitForFunction(() => !window.__phe.state.loading && window.__phe.state.dialogs.length === 0)
const openUrl = async (u) => {
  await page.fill('[data-test=url]', u)
  await page.press('[data-test=url]', 'Enter')
}

// move the canvas so the region is inside it (scroll with the wheel), then return its client position
async function bringIntoView(q, kind) {
  const box = await page.locator('[data-test=canvas]').boundingBox()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  for (let i = 0; i < 12; i++) {
    const pos = await page.evaluate(([a, b]) => window.__phe.regionClientPos(a, b), [q, kind])
    assert(pos, `region ${q || kind} must exist`)
    if (pos.y > box.y + 20 && pos.y < box.y + box.height - 20 && pos.x > box.x + 10 && pos.x < box.x + box.width - 10) return pos
    await page.mouse.wheel(0, pos.y > box.y + box.height / 2 ? 150 : -150)
    await page.waitForTimeout(120)
  }
  assert.fail('could not bring ' + q + ' into view')
}
const tapRegion = async (q, kind) => {
  const pos = await bringIntoView(q, kind)
  await page.mouse.click(pos.x, pos.y)
}

try {
  // 1. pair
  await page.goto(BASE + '/#/pair')
  await page.fill('[data-test=code]', CODE)
  await page.click('[data-test=pair]')
  await page.waitForSelector('[data-test=canvas]')
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
  step('paired and online (' + BASE + ')')
  const stored = await page.evaluate(() => new Promise((res) => {
    const r = indexedDB.open('phecloud', 1)
    r.onsuccess = () => { const q = r.result.transaction('kv').objectStore('kv').get('creds'); q.onsuccess = () => res(!!(q.result && q.result.dev && q.result.key && q.result.key.length === 32)) }
  }))
  assert(stored, 'device key must be stored in IndexedDB')
  step('device key stored in IndexedDB')

  // 2. open example.com
  await openUrl('https://example.com')
  await waitTitle('Example Domain')
  await idle()
  await page.waitForTimeout(300)
  assert.equal(await title(), 'Example Domain')
  step('example.com loaded')
  await page.screenshot({ path: OUT + '/web-example.png' })

  // 3. scroll + tap the link on the canvas
  await tapRegion('Learn more')
  await waitTitle('Example Domains')
  await idle()
  assert(await S(() => window.__phe.state.page.back), 'history: back must be available')
  step('tapped the link: title is now ' + (await title()))
  await page.waitForTimeout(300)
  await page.screenshot({ path: OUT + '/web-after-tap.png' })

  // 4. back button
  await page.click('[data-test=back]')
  await waitTitle('Example Domain')
  await idle()
  step('back button works')

  if (QUICK) { console.log('SMOKE PASS (quick)'); process.exit(0) }

  // 5. zoom: the cloud re-renders at the nearest scale (sharp text); the tap still lands on the right region
  assert((await S(() => window.__phe.state.features)).includes('zoom'), 'the server must advertise the zoom feature')
  await page.click('[data-test=zoom-in]')
  await page.click('[data-test=zoom-in]') // 1.25^2 = 1.56 -> the 150% render
  await page.waitForFunction(() => window.__phe.state.page.zoom === 150, null, { timeout: 20000 })
  await page.waitForFunction(() => !window.__phe.state.loading)
  const ph = await S(() => window.__phe.state.page.h)
  assert(ph > 918 * 1.4, 'the page is rendered 1.5x taller at 150%, got h=' + ph)
  const z = await S(() => window.__phe.renderer.view.zoom)
  assert(z > 1 && z < 1.1, 'the leftover local zoom is 1.5625/1.5, got ' + z)
  await page.waitForTimeout(250)
  await page.screenshot({ path: OUT + '/web-zoomed.png' })
  await tapRegion('Learn more')
  await waitTitle('Example Domains')
  await idle()
  step('zoom: server render at 150%, page height ' + ph + ', tap correct')
  await page.click('[data-test=back]')
  await waitTitle('Example Domain')
  await idle()
  // ctrl + wheel zoom to the top of the ladder, then back out to 100%
  const box = await page.locator('[data-test=canvas]').boundingBox()
  await page.mouse.move(box.x + 180, box.y + 200)
  await page.keyboard.down('Control')
  await page.mouse.wheel(0, -100)
  await page.keyboard.up('Control')
  await page.waitForFunction(() => window.__phe.state.page.zoom === 300, null, { timeout: 20000 })
  for (let i = 0; i < 9; i++) await page.click('[data-test=zoom-out]')
  await page.waitForFunction(() => window.__phe.state.page.zoom === 100, null, { timeout: 20000 })
  await page.waitForFunction(() => !window.__phe.state.loading)
  assert.equal(await S(() => window.__phe.renderer.view.zoom), 1)
  step('ctrl+wheel -> 300% render, zoom-out -> back to 100%')

  // 6. a page with an input, a select and dialogs
  const html = readFileSync(new URL('./webgaps.html', import.meta.url), 'utf8')
  await openUrl('data:text/html,' + encodeURIComponent(html))
  await waitTitle('WebGaps')
  await idle()

  // keyboard overlay: tap the field, type, Backspace
  await tapRegion(null, 'input')
  await page.waitForSelector('[data-test=keyboard-overlay]')
  await page.keyboard.type('abc')
  await page.waitForFunction(() => window.__phe.state.focus.value === 'abc', null, { timeout: 10000 })
  step('keyboard overlay: typed "abc", server field value is ' + (await S(() => window.__phe.state.focus.value)))
  await page.keyboard.press('Backspace')
  await page.waitForFunction(() => window.__phe.state.focus.value === 'ab', null, { timeout: 10000 })
  step('keyboard overlay: Backspace -> "ab"')
  await page.screenshot({ path: OUT + '/web-keyboard.png' })

  // select chooser
  await tapRegion(null, 'select')
  await page.waitForSelector('[data-test=select-dialog]')
  await page.locator('[data-test=select-dialog] .q-radio').nth(2).click()
  await page.click('[data-test=select-ok]')
  await page.waitForSelector('[data-test=select-dialog]', { state: 'detached' })
  await page.waitForFunction(() => { const r = window.__phe.state.regions.find((g) => g.kind === 'select'); return r && r.opts[2].s }, null, { timeout: 10000 })
  step('select chooser: option "Three" selected on the server')

  // alert
  await tapRegion('alert')
  await page.waitForSelector('[data-test=dialog]')
  assert((await page.textContent('[data-test=dialog]')).includes('hello'))
  await page.click('[data-test=dialog-ok]')
  await page.waitForSelector('[data-test=dialog]', { state: 'detached' })
  await waitTitle('alerted')
  step('alert dialog: shown and accepted')
  // confirm: cancel then ok
  await tapRegion('confirm')
  await page.waitForSelector('[data-test=dialog]')
  assert((await page.textContent('[data-test=dialog]')).includes('sure?'))
  await page.click('[data-test=dialog-cancel]')
  await page.waitForSelector('[data-test=dialog]', { state: 'detached' })
  await waitTitle('no')
  await tapRegion('confirm')
  await page.waitForSelector('[data-test=dialog]')
  await page.click('[data-test=dialog-ok]')
  await page.waitForSelector('[data-test=dialog]', { state: 'detached' })
  await waitTitle('yes')
  step('confirm dialog: cancel -> "no", ok -> "yes"')
  // prompt
  await tapRegion('ask')
  await page.waitForSelector('[data-test=dialog]')
  assert.equal(await page.inputValue('[data-test=dialog-text]'), 'def')
  await page.fill('[data-test=dialog-text]', 'Zed')
  await page.click('[data-test=dialog-ok]')
  await page.waitForSelector('[data-test=dialog]', { state: 'detached' })
  await waitTitle('Zed')
  step('prompt dialog: default shown, answer "Zed" reached the page')
  await page.screenshot({ path: OUT + '/web-dialogs.png' })

  // 7. HTTP authentication: a wrong password is asked again, the right one is accepted
  await openUrl('https://httpbin.org/basic-auth/zed/pw')
  await page.waitForSelector('[data-test=dialog-user]', { timeout: 30000 })
  assert((await page.textContent('[data-test=dialog]')).includes('httpbin.org'))
  await page.fill('[data-test=dialog-user]', 'zed')
  await page.fill('[data-test=dialog-pass]', 'wrong')
  await page.screenshot({ path: OUT + '/web-auth.png' })
  const firstId = await S(() => window.__phe.state.dialogs[0].id)
  await page.click('[data-test=dialog-ok]')
  // the server rejected the password: a NEW auth dialog (another id) is asked
  await page.waitForFunction((id) => window.__phe.state.dialogs.length > 0 && window.__phe.state.dialogs[0].id !== id, firstId, { timeout: 30000 })
  await page.waitForSelector('[data-test=dialog-user]')
  await page.fill('[data-test=dialog-user]', 'zed')
  await page.fill('[data-test=dialog-pass]', 'pw')
  await page.click('[data-test=dialog-ok]')
  await page.waitForSelector('[data-test=dialog]', { state: 'detached', timeout: 30000 })
  await page.waitForFunction(() => window.__phe.state.dialogs.length === 0)
  await page.waitForTimeout(3000)
  assert.equal(await S(() => window.__phe.state.dialogs.length), 0, 'no further auth prompt after the right password')
  await idle()
  step('auth dialog: wrong password asked again, right password accepted')

  // 7b. several tabs over one connection: three at most
  assert((await S(() => window.__phe.state.features)).includes('tabs'))
  await page.click('[data-test=tab-new]')
  await page.waitForFunction(() => window.__phe.state.tabs.length === 2 && window.__phe.state.activeView === 2)
  await openUrl('https://example.com')
  await waitTitle('Example Domain')
  await idle()
  await page.click('[data-test=tab-new]')
  await page.waitForFunction(() => window.__phe.state.tabs.length === 3 && window.__phe.state.activeView === 3)
  const gaps = readFileSync(new URL('./webgaps.html', import.meta.url), 'utf8')
  await openUrl('data:text/html,' + encodeURIComponent(gaps))
  await waitTitle('WebGaps')
  await idle()
  assert(await page.locator('[data-test=tab-new]').isDisabled(), 'the fourth tab must be refused (cap of 3)')
  await page.screenshot({ path: OUT + '/web-tabs.png' })
  await page.click('[data-test=tab-2]')
  await waitTitle('Example Domain')
  await idle()
  await page.click('[data-test=tab-1]')
  await page.waitForFunction(() => window.__phe.state.activeView === 1 && window.__phe.state.page.url.includes('httpbin.org'), null, { timeout: 20000 })
  await page.click('[data-test=tab-3]')
  await waitTitle('WebGaps')
  await page.click('[data-test=tab-close-2]')
  await page.waitForFunction(() => window.__phe.state.tabs.length === 2)
  await page.click('[data-test=tab-new]')
  await page.waitForFunction(() => window.__phe.state.tabs.length === 3)
  step('tabs: three tabs, fourth refused, switching re-shows each page, close and new work')

  // 8. devices page (the first/--admin device)
  await page.click('[data-test=nav-devices]')
  await page.waitForSelector('[data-test=device-list] .q-item')
  const n = await page.locator('[data-test=device-list] .q-item').count()
  assert(n >= 1, 'the device list must show this browser')
  step('devices page lists ' + n + ' device(s)')
  await page.screenshot({ path: OUT + '/web-devices.png' })

  // 9. settings: set a byte budget
  await page.click('[data-test=nav-settings]')
  await page.click('[data-test=gray]')
  await page.click('[data-test=apply]')
  await page.waitForSelector('[data-test=saved]')
  step('settings applied')
  // dark mode: the toggle in settings, and the choice sticks
  await page.locator('[data-test=theme]').getByRole('button', { name: 'Dark', exact: true }).click()
  await page.waitForFunction(() => document.body.classList.contains('body--dark'))
  await page.locator('[data-test=theme]').getByRole('button', { name: 'Match system', exact: true }).click()
  step('theme toggle: dark mode applies and system mode restores')

  // 10. reload: the stored key authenticates without pairing again
  await page.goto(BASE + '/#/browse')
  await page.reload()
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })
  step('reload: authenticated from the IndexedDB key')
  console.log('SMOKE PASS')
} catch (e) {
  console.error('SMOKE FAIL:', e.message)
  console.error(log.slice(-15).join('\n'))
  console.error(await page.evaluate(() => JSON.stringify({ err: window.__phe.state.error, sel: window.__phe.state.regions.filter((r) => r.kind === 'select'), focus: window.__phe.state.focus, dialogs: window.__phe.state.dialogs, page: window.__phe.state.page })).catch(() => ''))
  await page.screenshot({ path: OUT + '/web-fail.png' }).catch(() => {})
  process.exitCode = 1
} finally {
  await browser.close()
}
