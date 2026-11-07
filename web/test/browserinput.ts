// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of the browser's text fields, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types browserinput.ts
//
// The page is a picture; tapping a text field on it puts an invisible field over it that takes the keys. It must have the focus, so
// typing reaches the page as key presses (in the classic layout and in a desktop window), and the page's own field shows the text:
// nothing of ours drawn over it. A search box like Google's (a textarea that searches on Enter) gets every key as a real key press,
// Enter included, and the page sees no marks or globals of ours.

import { firefox, type Page } from 'playwright'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { openApp } from './chrome.ts'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const ADDR = '.x-bf__addr input'
const until = async (p: Page, f: () => boolean, what: string) => { for (let i = 0; !(await p.evaluate(f)); i++) { assert(i < 150, what); await p.waitForTimeout(200) } }

async function checkField(where: string) {
  await page.fill(ADDR, 'data:text/html,' + encodeURIComponent(readFileSync(new URL('./webgaps.html', import.meta.url), 'utf8')))
  await page.press(ADDR, 'Enter')
  await until(page, () => window.__phe.state.page.title === 'WebGaps' && !window.__phe.state.loading, 'the page loads')
  await until(page, () => !!window.__phe.regionClientPos(null, 'input'), 'the field is on the page')
  await page.waitForSelector('[data-test=keyboard-overlay]', { state: 'detached' }) // the field from before the page loaded
  const pos = (await page.evaluate(() => window.__phe.regionClientPos(null, 'input')))!
  await page.mouse.click(pos.x, pos.y)
  await page.waitForSelector('[data-test=keyboard-overlay]')
  await page.waitForTimeout(300) // anything that takes the focus back does it now
  const f = await page.evaluate(() => {
    const el = document.querySelector<HTMLInputElement>('[data-test=keyboard-overlay]')!
    const r = el.getBoundingClientRect(), cs = getComputedStyle(el)
    const top = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)
    return { focused: document.activeElement === el, w: r.width, h: r.height, caret: cs.caretColor, color: cs.color, opacity: cs.opacity, onTop: top === el }
  })
  await page.screenshot({ path: `${OUT}/browserinput-${where}.png` })
  assert(f.focused, `${where}: the field has the focus`)
  assert(f.w > 20 && f.h > 10 && f.onTop, `${where}: the key field is over the page's field (${JSON.stringify(f)})`)
  assert.equal(f.opacity, '0', `${where}: nothing of ours is drawn over the page's field`)
  await page.keyboard.type('abc')
  await until(page, () => window.__phe.state.focus.value === 'abc', `${where}: typing reaches the page`)
  console.log(where, 'ok')
}

async function checkKeys() {
  await page.fill(ADDR, 'data:text/html,' + encodeURIComponent(readFileSync(new URL('./typing.html', import.meta.url), 'utf8')))
  await page.press(ADDR, 'Enter')
  await until(page, () => window.__phe.state.page.title === 'Typing' && !!window.__phe.regionClientPos(null, 'textarea'), 'the search page loads')
  const pos = (await page.evaluate(() => window.__phe.regionClientPos(null, 'textarea')))!
  await page.mouse.click(pos.x, pos.y)
  await until(page, () => window.__phe.state.focus.kind === 'textarea', 'the search box has the focus')
  await page.waitForSelector('[data-test=keyboard-overlay]')
  await page.keyboard.type('weather athens', { delay: 30 })
  await page.keyboard.press('Enter')
  await until(page, () => window.__phe.state.page.title.startsWith('{'), 'Enter reaches the search box')
  const r = JSON.parse(await page.evaluate(() => window.__phe.state.page.title))
  assert.deepEqual(r, { q: 'weather athens', keys: 15, trusted: true, marks: 0, globals: [] }, 'every key is a real key press; the page sees nothing of ours')
  console.log('keys ok')
}

await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await until(page, () => window.__phe?.state.status === 'online', 'online')

await page.goto(BASE + '/#/browse')
await page.waitForSelector(ADDR)
await checkField('classic')
await checkKeys()

// the tabs: in the header in place of the title (no bar of their own), on the side when asked, and that choice kept
const HT = '.x-cl__bar [role=tab]'
assert.equal(await page.locator(HT).count(), 1, 'the open page is tab 1 in the header')
assert.equal(await page.locator('.x-bf__strip').count(), 0, 'no tab bar under the header')
await page.click('.x-cl__bar [aria-label="New tab"]')
await until(page, () => window.__phe.state.tabs.length === 2, 'a second tab')
await page.waitForFunction((s) => document.querySelectorAll(s).length === 2, HT)
// closing the new tab and then the last one: back to one tab each time, and the browser goes on working
await page.locator(HT).nth(1).locator('[aria-label^="Close"]').click()
await until(page, () => window.__phe.state.tabs.length === 1 && window.__phe.state.activeView === 1, 'the new tab closed')
await page.locator(HT).first().locator('[aria-label^="Close"]').click()
await until(page, () => window.__phe.state.tabs.length === 1 && window.__phe.state.activeView !== 1 && !window.__phe.state.error, 'the last tab closed: a blank one, no error')
await page.fill(ADDR, 'data:text/html,<title>after</title>')
await page.press(ADDR, 'Enter')
await until(page, () => window.__phe.state.page.title === 'after' && !window.__phe.state.error, 'a page opens after closing the last tab')
await page.click('.x-cl__bar [aria-label="New tab"]')
await until(page, () => window.__phe.state.tabs.length === 2, 'two tabs again')
const menu = async (id: string) => { await page.click(".x-bf__row [aria-label^='More']"); await page.click(`[data-test=${id}]`) }
await menu('tabs-side')
await page.waitForSelector('.x-bf__side [role=tab]')
assert(!(await page.locator(HT).count()), 'no tabs in the header with the tabs on the side')
await page.reload()
// a reload is a new session: its tabs show once a page is open
await page.waitForSelector(ADDR)
await page.fill(ADDR, 'data:text/html,<title>kept</title>')
await page.press(ADDR, 'Enter')
await until(page, () => window.__phe.state.page.title === 'kept' && !window.__phe.state.loading, 'a page opens after the reload')
await page.waitForSelector('.x-bf__side [role=tab]')
await menu('tabs-side')
await page.waitForSelector(HT)
await page.screenshot({ path: `${OUT}/browserinput-tabs.png` })

// Desktop site: on by itself in a wide window; off asks sites for their mobile page (the page reloads)
const uaPage = 'data:text/html,' + encodeURIComponent('<title>x</title><script>document.title = /Mobile/.test(navigator.userAgent) ? "mobile" : "desktop"</script>')
await page.fill(ADDR, uaPage)
await page.press(ADDR, 'Enter')
await until(page, () => window.__phe.state.page.title === 'desktop', 'a wide window gets the desktop site')
await menu('desktop-site')
await until(page, () => window.__phe.state.page.title === 'mobile', 'Desktop site off: the mobile site')
// the menu's switches show what is on: Desktop site off, tabs on top
await page.click(".x-bf__row [aria-label^='More']")
await page.waitForSelector('[data-test=desktop-site] .q-toggle__inner--falsy')
assert.equal(await page.locator('[data-test=tabs-side] .q-toggle__inner--falsy').count(), 1, 'Tabs on the side: off')
const z = await page.locator('[data-test=zoom-out]').boundingBox(), zi = await page.locator('[data-test=zoom-in]').boundingBox()
assert(z && zi && Math.abs(z.y - zi.y) < 4, 'Zoom: − and + on one line')
await page.screenshot({ path: `${OUT}/browserinput-menu.png` })
await page.keyboard.press('Escape')
await menu('desktop-site')
await until(page, () => window.__phe.state.page.title === 'desktop', 'Desktop site on again')
console.log('tabs and desktop site ok')

await openApp(page, 'Desktop')
await page.waitForSelector('.x-win[data-win=browse] ' + ADDR)
assert(await page.locator('.x-win[data-win=browse] .x-win__bar [role=tab]').count() >= 1, 'the tabs are in the window decoration')
assert.equal(await page.locator('.x-bf__strip').count(), 0, 'no tab bar under the decoration')
await page.screenshot({ path: `${OUT}/browserinput-desktop-tabs.png` })
await checkField('desktop')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
