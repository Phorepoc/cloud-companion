// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Playwright test (Firefox) of Settings, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types settings.ts
//
// Settings is the shared Etesian split: on a wide screen a side panel of grouped pages (this device; the server's Connections, Devices,
// Catalogue, Weather and Security for the owner) with the page beside it; on a phone the list drills down and the page has Back. Each
// page has its address, the old /connections opens its page, and in a desktop window a link to a page of Settings opens it there.

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { openApp } from './chrome.ts'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, colorScheme: 'light', serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const side = '.x-sset__side'
const row = (title: string) => page.locator(`${side} .hd-list-item`, { hasText: title }).first()
const hash = () => page.evaluate(() => location.hash)

step('pair (as the owner)')
await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
for (let i = 0; !(await page.evaluate(() => window.__phe?.state.status === 'online')); i++) { assert(i < 100, 'online'); await page.waitForTimeout(200) }

step('wide: the side panel lists this device and the server, the page beside it')
await page.goto(BASE + '/#/settings')
await page.waitForSelector(side)
const sideText = (await page.textContent(side)) ?? ''
for (const t of ['This device', 'Appearance', 'Data use', 'Pair a new device', 'This browser', 'Your server', 'Connections', 'Devices', 'Catalogue', 'Weather', 'Security'])
  assert(sideText.includes(t), `the side panel has ${t}`)
assert(await page.locator('[data-test=theme]').isVisible(), 'Appearance is the first page')
const sb = (await page.locator(side).boundingBox())!, pb = (await page.locator('.x-sset__page').boundingBox())!
assert(pb.x >= sb.x + sb.width - 1, 'the page sits beside the panel')
await page.screenshot({ path: `${OUT}/settings-wide.png` })

step('Hybrid (the N9 look, the default): light pages, Music dark all the way; Light keeps Music light')
const pageBg = () => page.evaluate(() => {
  const el = document.querySelector('#main .q-page') as HTMLElement
  const [r, g, b] = (getComputedStyle(el.closest('.ph-page-mode, [data-hd-mode=dark]') || el).backgroundColor.match(/\d+/g) || []).map(Number)
  return (r + g + b) / 3
})
const pickTheme = async (name: string) => {
  await page.goto(BASE + '/#/settings/appearance')
  await page.locator('[data-test=theme]').getByRole('button', { name, exact: true }).click()
}
const openMusic = async () => { await openApp(page, 'Music'); await page.waitForSelector('#main .q-page') }
assert.equal(await page.evaluate(() => document.documentElement.dataset.hdMode), 'hybrid', 'Hybrid is the default')
assert((await pageBg()) < 40, 'Settings is black in Hybrid')
await openMusic()
assert((await pageBg()) < 40, 'Music is dark in Hybrid')
const barBg = await page.evaluate(() => getComputedStyle(document.querySelector('.x-cl__bar')!).backgroundColor.match(/\d+/g)!.slice(0, 3).map(Number).reduce((a, b) => a + b) / 3)
assert(barBg < 40, 'its bar is dark too')
const btn = await page.evaluate(() => getComputedStyle(document.querySelector('[data-test=music-empty] .q-btn')!).color.match(/\d+/g)!.slice(0, 3).map(Number).reduce((a, b) => a + b) / 3)
assert(btn > 120, 'the Open Files button is readable on black')
await page.screenshot({ path: `${OUT}/theme-hybrid-music.png` })
await pickTheme('Light')
assert.equal(await page.evaluate(() => document.documentElement.dataset.hdMode), 'light')
await openMusic()
assert((await pageBg()) > 180, 'Music stays light in Light')
await pickTheme('Hybrid')
await page.screenshot({ path: `${OUT}/theme-hybrid-settings.png` })

step('picking Devices shows it and gives it its address')
await row('Devices').click()
await page.waitForSelector('.x-sset__page h1:has-text("Devices")')
assert.equal(await hash(), '#/settings/devices')

step('the old address /connections opens Connections in Settings')
await page.goto(BASE + '/#/connections')
await page.waitForSelector('.x-sset__page h1:has-text("Connections")')
assert.equal(await hash(), '#/settings/connections')
assert(await page.locator(`${side} .hd-list-item.is-selected`, { hasText: 'Connections' }).count(), 'Connections is marked in the panel')
await page.click('.x-cl__apps')
assert.equal(await page.locator('.x-cl__grid .x-cl__icon', { hasText: 'Connections' }).count(), 0, 'no separate Connections section in the apps menu')
await page.click('.x-cl__apps')

step('search narrows the panel')
await page.fill(`${side} input`, 'sec')
const left = await page.locator(`${side} .hd-list-item`).count()
assert.equal(left, 1, 'only Security matches "sec"')
await page.fill(`${side} input`, '')

step('phone: the list first; a page drills down and has Back')
await page.setViewportSize({ width: 390, height: 844 })
await page.goto(BASE + '/#/browse')
await page.goto(BASE + '/#/settings') // no page in the address: the list first
await page.waitForSelector(side)
await page.screenshot({ path: `${OUT}/settings-phone-list.png` })
await row('Data use').click()
await page.waitForSelector('[data-test=quality]')
assert(!(await page.locator(side).isVisible()), 'the list steps aside for the page')
await page.screenshot({ path: `${OUT}/settings-phone-page.png` })
await page.click('.x-sset__back')
await page.waitForSelector(side)

step('desktop: Settings in a window is the same split; a link to a page of Settings opens it there')
await page.setViewportSize({ width: 1440, height: 900 })
await page.goto(BASE + '/#/settings/appearance')
await openApp(page, 'Desktop')
await page.waitForSelector('.x-win[data-win=settings] .x-sset').catch(async (e) => { await page.screenshot({ path: `${OUT}/settings-fail.png` }); throw e })
await page.screenshot({ path: `${OUT}/settings-desktop-open.png` })
const setBox = (await page.locator('.x-win[data-win=settings] .x-sset').boundingBox())!
assert(setBox.height > 300 && setBox.width > 500, 'the dark Settings window fills its area')
await page.evaluate(() => { location.hash = '#/connections' })
await page.waitForSelector('.x-win[data-win=settings] .x-sset__page h1:has-text("Connections")')
assert(await page.locator('[data-test=desktop]').isVisible(), 'still on the desktop')
await page.screenshot({ path: `${OUT}/settings-desktop.png` })

assert.deepEqual(errors, [], 'no page errors')
step('ok')
await browser.close()
