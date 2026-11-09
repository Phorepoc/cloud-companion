// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Two browsers (two paired devices) at once: signing one out from the other.
//   BASE=http://phecloud:8080 CODES=AAAA-BBBB,CCCC-DDDD OUT=/out node --experimental-strip-types multi.ts
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const [CODE_A, CODE_B] = (process.env.CODES || '').split(',')
const OUT = process.env.OUT || '.'
assert(CODE_A && CODE_B, 'CODES must hold two pairing codes')
const browser = await chromium.launch()
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)

// the browser frame (Etesian XBrowserFrame): the address field, Back, the ⋮ menu (new tab, other devices, zoom, phone size) and the tab
// picker (the tab count opens the pages as cards)
const BACK = '.x-bf__row [aria-label=Back]'
async function more(p: any) { if (!(await p.locator('[data-test=browser-more]').isVisible())) { await p.click(".x-bf__row [aria-label^='More']"); await p.waitForSelector('[data-test=browser-more]') } }
async function menuItem(id: string, p: any) { await more(p); await p.click(`[data-test=${id}]`) }
async function picker(p: any) { if (!(await p.locator('.x-bf__picker').isVisible())) await p.click('.x-bf__count') }
async function tabGo(n: number, p: any) { await picker(p); await p.click(`.x-bf__card:nth-child(${n}) .x-bf__shot`) }
async function tabClose(n: number, p: any) { await picker(p); await p.click(`.x-bf__card:nth-child(${n}) .x-bt__x`); if (await p.locator('.x-bf__picker').isVisible()) await p.click('.x-bf__count') }

async function device(code: string, name: string, viewport = { width: 1280, height: 800 }) {
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
  // signing out the Phone from the Laptop cuts it off at once: it lands on pairing, told why, and does not keep retrying
  const phoneDev = await B.page.evaluate(() => window.__phe.state.dev)
  await A.page.goto(BASE + '/#/settings/devices')
  await A.page.click(`[data-test=revoke-${phoneDev}]`)
  await A.page.click('[data-test=devices-confirm-ok]') // the app's own dialog, not the browser's
  await B.page.waitForFunction(() => location.hash.startsWith('#/pair'), null, { timeout: 10000 })
  assert.match(await B.page.textContent('[data-test=pair-error]') || '', /signed out from another device/)
  assert.equal(await B.page.evaluate(() => window.__phe.state.status), 'offline')
  await B.page.screenshot({ path: `${OUT}/multi-signed-out.png` })
  step('signed out from the Laptop: the Phone is cut off and back on pairing, told why')
  console.log('MULTI PASS')
} catch (e) {
  console.error('MULTI FAIL:', (e as Error).message)
  process.exitCode = 1
} finally {
  await browser.close()
}
