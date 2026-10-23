// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Two browsers (two paired devices) at once: the "tabs on your devices" list in the Browser section.
//   BASE=http://phecloud:8080 CODES=AAAA-BBBB,CCCC-DDDD OUT=/out node multi.mjs
// B opens example.com; A sees B's tab in the list, closes it from there, opens a page on B, and opens B's tab "here".
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const [CODE_A, CODE_B] = (process.env.CODES || '').split(',')
const OUT = process.env.OUT || '.'
assert(CODE_A && CODE_B, 'CODES must hold two pairing codes')
const browser = await chromium.launch()
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)

async function device(code, name, viewport = { width: 1280, height: 800 }) {
  const ctx = await browser.newContext({ bypassCSP: true, viewport })
  const page = await ctx.newPage()
  await page.goto(BASE + '/#/pair')
  await page.fill('[data-test=name]', name)
  await page.fill('[data-test=code]', code)
  await page.click('[data-test=pair]')
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
  return { ctx, page }
}

try {
  const A = await device(CODE_A, 'Laptop')
  const B = await device(CODE_B, 'Phone', { width: 390, height: 844 })
  step('two devices paired and online')

  await B.page.fill('[data-test=url]', 'https://example.com')
  await B.page.press('[data-test=url]', 'Enter')
  await B.page.waitForFunction(() => window.__phe.state.page.title === 'Example Domain', null, { timeout: 30000 })
  await B.page.waitForFunction(() => window.__phe.state.tabs.length === 1)
  step('Phone opened example.com')

  await A.page.click('[data-test=other-devices]')
  await A.page.waitForSelector('[data-test=others-dialog]')
  await A.page.waitForSelector('[data-test^=dev-tab-] >> text=Example Domain', { timeout: 15000 })
  const dialogText = await A.page.textContent('[data-test=others-dialog]')
  assert.match(dialogText, /Phone/)
  assert.match(dialogText, /This browser/)
  step('Laptop lists the Phone and its open tab "Example Domain"')
  for (const [vp, size] of [['desktop', { width: 1280, height: 800 }], ['phone', { width: 390, height: 844 }]]) {
    await A.page.setViewportSize(size)
    for (const scheme of ['light', 'dark']) {
      await A.page.emulateMedia({ colorScheme: scheme })
      await A.page.waitForTimeout(300)
      await A.page.screenshot({ path: `${OUT}/${vp}-${scheme}-other-devices.png` })
    }
  }
  await A.page.setViewportSize({ width: 1280, height: 800 })
  await A.page.emulateMedia({ colorScheme: 'light' })

  // close the Phone's tab from the Laptop
  const phoneSid = await A.page.evaluate(() => window.__phe.state.sessions.find((s) => s.name === 'Phone').sid)
  const phoneView = await A.page.evaluate(() => window.__phe.state.sessions.find((s) => s.name === 'Phone').tabs[0].v)
  await A.page.click(`[data-test=dev-close-${phoneSid}-${phoneView}]`)
  await B.page.waitForFunction(() => window.__phe.state.tabs.length === 0, null, { timeout: 15000 })
  step('closed the Phone tab from the Laptop: the Phone has no tabs any more')

  // open a page on the Phone from the Laptop
  await A.page.fill(`[data-test=dev-url-${phoneSid}]`, 'https://example.com')
  await A.page.click(`[data-test=dev-send-${phoneSid}]`)
  await B.page.waitForFunction(() => window.__phe.state.tabs.length === 1 && window.__phe.state.page.title === 'Example Domain', null, { timeout: 30000 })
  step('opened example.com on the Phone from the Laptop: the Phone shows it')

  // "Open here": the Phone's tab opens in a new tab on the Laptop
  await A.page.waitForSelector(`[data-test^=dev-open-${phoneSid}-]`, { timeout: 15000 })
  await A.page.click(`[data-test^=dev-open-${phoneSid}-]`)
  await A.page.waitForFunction(() => window.__phe.state.page.title === 'Example Domain' && window.__phe.state.tabs.length === 1, null, { timeout: 30000 })
  step('Open here: the Phone tab opened in a tab of the Laptop')
  console.log('MULTI PASS')
} catch (e) {
  console.error('MULTI FAIL:', e.message)
  process.exitCode = 1
} finally {
  await browser.close()
}
