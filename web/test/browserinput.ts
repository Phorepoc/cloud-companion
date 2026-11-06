// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of the browser's text fields, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types browserinput.ts
//
// The page is a picture; tapping a text field on it puts a real field over it. That field must have the focus and show a caret, so
// typing goes there, in the classic layout and in a desktop window.

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
  assert(f.w > 20 && f.h > 10 && f.onTop && f.opacity !== '0', `${where}: the field is shown on top (${JSON.stringify(f)})`)
  assert(f.caret !== 'transparent' && !/rgba\(.*, 0\)/.test(f.caret), `${where}: the caret is visible (${f.caret})`)
  await page.keyboard.type('abc')
  await until(page, () => window.__phe.state.focus.value === 'abc', `${where}: typing reaches the page`)
  console.log(where, 'ok')
}

await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await until(page, () => window.__phe?.state.status === 'online', 'online')

await page.goto(BASE + '/#/browse')
await page.waitForSelector(ADDR)
await checkField('classic')

await openApp(page, 'Desktop')
await page.waitForSelector('.x-win[data-win=browse] ' + ADDR)
await checkField('desktop')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
