// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright smoke test of the web app, run against a live server:
//   BASE=http://phe-cloud:8080 CODE=ABCD-EFGH OUT=/out node smoke.mjs
// Pairs with the code, opens example.com, taps the link on the canvas, asserts the title, checks the devices page
// (admin) and the settings page, takes screenshots.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code, preferably from `phecloud paircode --admin`) is required')

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 900, height: 900 } })
const page = await ctx.newPage()
const log = []
page.on('console', (m) => log.push(m.type() + ': ' + m.text()))
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)

try {
  // 1. pair
  await page.goto(BASE + '/#/pair')
  await page.fill('[data-test=code]', CODE)
  await page.click('[data-test=pair]')
  await page.waitForSelector('[data-test=canvas]')
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
  step('paired and online')
  const stored = await page.evaluate(() => new Promise((res) => {
    const r = indexedDB.open('phecloud', 1)
    r.onsuccess = () => { const q = r.result.transaction('kv').objectStore('kv').get('creds'); q.onsuccess = () => res(!!(q.result && q.result.dev && q.result.key && q.result.key.length === 32)) }
  }))
  assert(stored, 'device key must be stored in IndexedDB')
  step('device key stored in IndexedDB')

  // 2. open example.com
  await page.fill('[data-test=url]', 'https://example.com')
  await page.press('[data-test=url]', 'Enter')
  await page.waitForFunction(() => window.__phe.state.page.title === 'Example Domain', null, { timeout: 30000 })
  await page.waitForFunction(() => !window.__phe.state.loading)
  await page.waitForTimeout(300)
  step('example.com loaded, title ' + (await page.textContent('[data-test=title]')))
  assert.equal((await page.textContent('[data-test=title]')).trim(), 'Example Domain')
  await page.screenshot({ path: OUT + '/web-example.png' })

  // 3. tap the link on the canvas
  const pos = await page.evaluate(() => window.__phe.regionClientPos('Learn more'))
  assert(pos, 'the "Learn more" link region must exist')
  // the link is below the first screen: scroll the canvas locally, then tap
  await page.mouse.move(pos.x, 300)
  await page.mouse.wheel(0, 400)
  await page.waitForTimeout(300)
  const pos2 = await page.evaluate(() => window.__phe.regionClientPos('Learn more'))
  await page.mouse.click(pos2.x, pos2.y)
  await page.waitForFunction(() => window.__phe.state.page.title === 'Example Domains', null, { timeout: 30000 })
  await page.waitForFunction(() => !window.__phe.state.loading)
  step('tapped the link: title is now ' + (await page.textContent('[data-test=title]')))
  assert.equal((await page.textContent('[data-test=title]')).trim(), 'Example Domains')
  assert(await page.evaluate(() => window.__phe.state.page.back), 'history: back must be available')
  await page.waitForTimeout(300)
  await page.screenshot({ path: OUT + '/web-after-tap.png' })

  // 4. back button
  await page.click('[data-test=back]')
  await page.waitForFunction(() => window.__phe.state.page.title === 'Example Domain', null, { timeout: 30000 })
  step('back button works')

  // 5. devices page (the first/--admin device)
  await page.click('[data-test=tab-devices]')
  await page.waitForSelector('[data-test=device-list] .q-item')
  const n = await page.locator('[data-test=device-list] .q-item').count()
  assert(n >= 1, 'the device list must show this browser')
  step('devices page lists ' + n + ' device(s)')
  await page.screenshot({ path: OUT + '/web-devices.png' })

  // 6. settings: set a byte budget
  await page.click('[data-test=tab-settings]')
  await page.click('[data-test=gray]')
  await page.click('[data-test=apply]')
  await page.waitForSelector('[data-test=saved]')
  step('settings applied')

  // 7. reload: the stored key authenticates without pairing again
  await page.goto(BASE + '/#/browse')
  await page.reload()
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })
  step('reload: authenticated from the IndexedDB key')
  console.log('SMOKE PASS')
} catch (e) {
  console.error('SMOKE FAIL:', e.message)
  console.error(log.slice(-15).join('\n'))
  await page.screenshot({ path: OUT + '/web-fail.png' }).catch(() => {})
  process.exitCode = 1
} finally {
  await browser.close()
}
