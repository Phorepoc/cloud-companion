// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright test of the web app, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types smoke.ts
// Environment: QUICK=1 stops after the Browser page (used for the TLS proxy check); IGNORE_TLS=1 accepts a self-signed proxy cert.
//
// Pairs, opens the Browser page (no engine for now: it says so), the devices page (admin), settings, and a reload that
// re-authenticates from the stored key.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { openApp } from './chrome.ts'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
const QUICK = process.env.QUICK === '1'
assert(CODE, 'CODE (a pairing code, preferably from `phecloud paircode --admin`) is required')

const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 360, height: 760 }, ignoreHTTPSErrors: process.env.IGNORE_TLS === '1' })
const page = await ctx.newPage()

// the browser frame (Etesian XBrowserFrame): the address field, Back, the ⋮ menu (new tab, other devices, zoom, phone size) and the tab
// picker (the tab count opens the pages as cards)
const ADDR = '.x-bf__addr input'
const BACK = '.x-bf__row [aria-label=Back]'
async function more(p = page) { if (!(await p.locator('[data-test=browser-more]').isVisible())) { await p.click(".x-bf__row [aria-label^='More']"); await p.waitForSelector('[data-test=browser-more]') } }
async function menuItem(id: string, p = page) { await more(p); await p.click(`[data-test=${id}]`) }
async function picker(p = page) { if (!(await p.locator('.x-bf__picker').isVisible())) await p.click('.x-bf__count') }
async function tabGo(n: number, p = page) { await picker(p); await p.click(`.x-bf__card:nth-child(${n}) .x-bf__shot`) }
async function tabClose(n: number, p = page) { await picker(p); await p.click(`.x-bf__card:nth-child(${n}) .x-bt__x`); if (await p.locator('.x-bf__picker').isVisible()) await p.click('.x-bf__count') }
// this test measures the phone layout: a phone-wide window, so 360 px wide pages (page heights, the 150% render)
const log: string[] = []
page.on('console', (m) => log.push(m.type() + ': ' + m.text()))
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const S = <T>(expr: () => T) => page.evaluate(expr)
const text = async (sel: string) => (await page.textContent(sel)) ?? ''
const title = async () => (await text('[data-test=title]')).trim()
const waitTitle = (t: string, ms = 30000) => page.waitForFunction((x) => window.__phe.state.page.title === x, t, { timeout: ms })
const idle = () => page.waitForFunction(() => !window.__phe.state.loading && window.__phe.state.dialogs.length === 0)
const openUrl = async (u: string) => {
  // the ⋮ menu stays open over zoom steps; close it first, as a person would
  if (await page.locator('[data-test=browser-more]').isVisible()) { await page.keyboard.press('Escape'); await page.locator('[data-test=browser-more]').waitFor({ state: 'hidden' }) }
  await page.fill(ADDR, u)
  await page.press(ADDR, 'Enter')
}

// move the canvas so the region is inside it (scroll with the wheel), then return its client position
async function bringIntoView(q: string | null, kind?: string) {
  const box = await page.locator('[data-test=canvas]').boundingBox()
  assert(box, 'the canvas is laid out')
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  for (let i = 0; i < 12; i++) {
    const pos = await page.evaluate(([a, b]) => window.__phe.regionClientPos(a, b), [q, kind] as const)
    assert(pos, `region ${q || kind} must exist`)
    if (pos.y > box.y + 20 && pos.y < box.y + box.height - 20 && pos.x > box.x + 10 && pos.x < box.x + box.width - 10) return pos
    await page.mouse.wheel(0, pos.y > box.y + box.height / 2 ? 150 : -150)
    await page.waitForTimeout(120)
  }
  assert.fail('could not bring ' + q + ' into view')
}
const tapRegion = async (q: string | null, kind?: string) => {
  const pos = await bringIntoView(q, kind)
  await page.mouse.click(pos.x, pos.y)
}

try {
  // 1. pair
  await page.goto(BASE + '/#/pair')
  await page.fill('[data-test=code]', CODE)
  await page.click('[data-test=pair]')
  await page.waitForSelector('[data-test=browser-frame]')
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
  step('paired and online (' + BASE + ')')
  const stored = await page.evaluate(() => new Promise((res) => {
    const r = indexedDB.open('phecloud', 1)
    r.onsuccess = () => { const q = r.result.transaction('kv').objectStore('kv').get('creds'); q.onsuccess = () => res(!!(q.result && q.result.dev && q.result.key && q.result.key.length === 32)) }
  }))
  assert(stored, 'device key must be stored in IndexedDB')
  step('device key stored in IndexedDB')

  // 2. without the browser container the Browser page still opens, and says how to set it up
  await page.goto(BASE + '/#/browse')
  await page.waitForSelector('[data-test=browser-frame]')
  // (on plain http it asks for a secure address first, as device requests can't be signed there)
  await page.waitForSelector(BASE.startsWith('https:') || BASE.includes('localhost') ? '[data-test=browser-off]' : '[data-test=browser-insecure]', { timeout: 15000 })
  step('Browser page: the UI is there and says what it needs')
  if (QUICK) { console.log('SMOKE PASS'); await browser.close(); process.exit(0) }

  // 8. devices page (the first/--admin device)
  await openApp(page, 'Settings'); await page.click('.x-sset__side .hd-list-item:has-text("Devices")')
  await page.waitForSelector('[data-test=device-list] .q-item')
  const n = await page.locator('[data-test=device-list] .q-item').count()
  assert(n >= 1, 'the device list must show this browser')
  step('devices page lists ' + n + ' device(s)')
  await page.screenshot({ path: OUT + '/web-devices.png' })

  // 9. settings: set a byte budget (in a wider window: the settings list beside the page)
  await page.setViewportSize({ width: 900, height: 900 })
  await openApp(page, 'Settings'); await page.click('.x-sset__side .hd-list-item:has-text("Data use")')
  await page.click('[data-test=gray]')
  await page.click('[data-test=apply]')
  await page.waitForSelector('[data-test=saved]')
  step('settings applied')
  // dark mode: the toggle in settings, and the choice sticks
  await page.click('.x-sset__side .hd-list-item:has-text("Appearance")')
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
  console.error('SMOKE FAIL:', (e as Error).message)
  console.error(log.slice(-15).join('\n'))
  console.error(await page.evaluate(() => JSON.stringify({ err: window.__phe.state.error, sel: window.__phe.state.regions.filter((r) => r.kind === 'select'), focus: window.__phe.state.focus, dialogs: window.__phe.state.dialogs, page: window.__phe.state.page })).catch(() => ''))
  await page.screenshot({ path: OUT + '/web-fail.png' }).catch(() => {})
  process.exitCode = 1
} finally {
  await browser.close()
}
