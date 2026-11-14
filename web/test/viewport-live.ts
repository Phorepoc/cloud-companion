// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
//
// A look at a RUNNING server: the cloud browser and Android at full size, the Desktop site switch, the Android mode buttons, wide and narrow, light and dark.
// Pairs a throwaway admin browser named "viewport-live" (revoke it afterwards: `phecloud devices`, `phecloud revoke <id>`).
//   BASE=https://host:9443 CODE=<admin code> OUT=/out node --experimental-strip-types viewport-live.ts
import { chromium } from 'playwright'
import { view } from './chrome.ts'
import assert from 'node:assert/strict'

const BASE = process.env.BASE!, CODE = process.env.CODE!, OUT = process.env.OUT || '.'
const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, ignoreHTTPSErrors: true, viewport: { width: 1400, height: 900 } })
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
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const box = async (sel: string) => { const b = await page.locator(sel).first().boundingBox(); assert(b, sel + ' has no box'); return b }
const near = (a: number, b: number, t = 2) => Math.abs(a - b) <= t
const shots = async (name: string) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme }); await page.waitForTimeout(300)
    await page.screenshot({ path: `${OUT}/${name}-${scheme}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.fill('[data-test=name]', 'viewport-live')
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 30000 })

// ---- the cloud browser, wide ----
await page.goto(BASE + '/#/browse')
await page.waitForSelector('[data-test=canvas]')
await page.waitForTimeout(600)
let st = await box('[data-test=stage]'), cv = await box('[data-test=canvas]')
assert(near(cv.width, st.width) && near(cv.height, st.height), `the page fills the stage (${JSON.stringify(cv)} in ${JSON.stringify(st)})`)
await page.evaluate(() => window.__phe.api.open('https://example.com/'))
await page.waitForFunction(() => window.__phe.state.page.pid > 0 && window.__phe.state.page.w === window.__phe.state.viewport.w, null, { timeout: 40000 })
console.log('browser: page laid out at', await page.evaluate(() => window.__phe.state.page.w), 'px wide (the window), stage', Math.round(st.width), 'x', Math.round(st.height))
await page.waitForTimeout(1500)
await shots('live-browser-wide')
await menuItem('desktop-site')
await page.waitForTimeout(3000)
await shots('live-browser-mobile-site')
await menuItem('desktop-site')
// live window resize
await page.setViewportSize({ width: 1100, height: 800 })
await page.waitForFunction(() => window.__phe.state.page.w === window.__phe.state.viewport.w && window.__phe.state.viewport.w < 900, null, { timeout: 30000 })
console.log('browser: window resized -> page', await page.evaluate(() => window.__phe.state.page.w), 'px wide')

// ---- the cloud browser, narrow ----
await page.setViewportSize({ width: 390, height: 844 })
await page.waitForFunction(() => window.__phe.state.page.w === window.__phe.state.viewport.w && window.__phe.state.viewport.w < 400, null, { timeout: 30000 })
await page.waitForTimeout(1500)
await shots('live-browser-narrow')

// ---- Android ----
await page.setViewportSize({ width: 1400, height: 900 })
await page.goto(BASE + '/#/android?codec=vp8') // the test browser has no H.264 decoder; a real browser decodes the default
await page.waitForFunction(() => /Connected/.test(document.querySelector('[data-test=android-state]')?.textContent ?? ''), null, { timeout: 120000 })
const shape = async (wide: boolean) => page.waitForFunction((w) => { const c = document.querySelector('[data-test=android-screen]') as HTMLCanvasElement; return (c.width > c.height) === w && c.width > 400 }, wide, { timeout: 60000 })
await shape(true)
await page.waitForTimeout(2500)
st = await box('[data-test=android-stage]')
let fr = await box('.ph-frame')
assert(fr.width * fr.height >= st.width * st.height * 0.9, `Android fills the window in desktop mode (${JSON.stringify(fr)} in ${JSON.stringify(st)})`)
console.log('android: desktop mode, frame', Math.round(fr.width), 'x', Math.round(fr.height), 'in stage', Math.round(st.width), 'x', Math.round(st.height))
await shots('live-android-desktop')
await view(page, 'Phone mode')
await shape(false)
await page.waitForTimeout(2500)
console.log('android: phone mode -> portrait', await page.evaluate(() => { const c = document.querySelector('[data-test=android-screen]') as HTMLCanvasElement; return c.width + 'x' + c.height }))
await shots('live-android-phone-mode')
await view(page, 'Desktop mode')
await shape(true)
await page.waitForTimeout(2000)
console.log('android: back to desktop mode')
await page.setViewportSize({ width: 390, height: 844 })
await shape(false)
await page.waitForTimeout(2500)
await shots('live-android-narrow')
assert.deepEqual(errors, [], 'page errors')
await browser.close()
console.log('OK')
