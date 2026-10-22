// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Screenshots of every page in light and dark, on a phone (390 px) and a desktop (1280 px) viewport:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out/before ROUTES=pair,browse,devices,settings node shots.mjs
// Files: <OUT>/<viewport>-<scheme>-<route>.png. Used for the before/after comparison of the style guide work.
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const OUT = process.env.OUT || './shots'
const ROUTES = (process.env.ROUTES || 'pair,browse,devices,settings').split(',')
mkdirSync(OUT, { recursive: true })

const viewports = { phone: { width: 390, height: 844 }, desktop: { width: 1280, height: 800 } }
const browser = await chromium.launch()

for (const [vpName, viewport] of Object.entries(viewports)) {
  for (const scheme of ['light', 'dark']) {
    const code = process.env.CODES.split(',').shift()
    process.env.CODES = process.env.CODES.split(',').slice(1).join(',')
    const ctx = await browser.newContext({ viewport, colorScheme: scheme, deviceScaleFactor: 1 })
    const page = await ctx.newPage()
    const shot = (route) => page.screenshot({ path: `${OUT}/${vpName}-${scheme}-${route}.png` })
    await page.goto(BASE + '/#/pair')
    await page.waitForSelector('[data-test=code]')
    await page.waitForTimeout(400)
    if (ROUTES.includes('pair')) await shot('pair')
    await page.fill('[data-test=code]', code)
    await page.click('[data-test=pair]')
    await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })
    await page.waitForSelector('[data-test=canvas]')
    await page.fill('[data-test=url]', 'https://example.com')
    await page.press('[data-test=url]', 'Enter')
    await page.waitForFunction(() => window.__phe.state.page.title === 'Example Domain', null, { timeout: 30000 })
    await page.waitForFunction(() => !window.__phe.state.loading)
    await page.waitForTimeout(500)
    for (const r of ROUTES.filter((x) => x !== 'pair')) {
      await page.evaluate((h) => { location.hash = '#/' + h }, r)
      await page.waitForTimeout(r === 'devices' ? 900 : 500)
      await shot(r)
    }
    await ctx.close()
  }
}
await browser.close()
console.log('screenshots in', OUT)
