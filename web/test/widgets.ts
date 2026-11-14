// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Playwright test (Firefox) of the desktop widgets, run against a live server:
//   BASE=http://phecloud:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types widgets.ts
//
// Through the screen, as a person would: Edit desktop from the View menu, add a widget, use it, reload.

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { openApp } from './chrome.ts'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'light', serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const note = '[data-test=widget-note]'
const noteText = () => page.locator(note).inputValue()

step('pair and open the desktop')
await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
for (let i = 0; !(await page.evaluate(() => window.__phe?.state.status === 'online')); i++) { assert(i < 100, 'online'); await page.waitForTimeout(200) }
await page.goto(BASE + '/#/browse')
await openApp(page, 'Desktop')
await page.waitForSelector('[data-test=desktop]')

step('Edit desktop from the View menu; add Notes')
await page.click('.x-mbar__menu:has-text("View")')
await page.click('text=Edit desktop')
await page.click('[data-test=widget-add-notes]')
await page.click('[data-test=widgets-done]')
await page.waitForSelector(note)
await page.click('.x-mbar__menu:has-text("View")')
await page.click('text=Show the desktop') // widgets sit under the windows

step('write a note, + for a second one, write it')
await page.click(note)
await page.keyboard.type('buy milk')
assert.equal(await noteText(), 'buy milk', 'typing reaches the note')
await page.click('[data-test=widget-note-add]')
assert.equal(await noteText(), '', 'a new empty note')
await page.click(note)
await page.keyboard.type('call Anna')
await page.screenshot({ path: `${OUT}/widgets-notes.png` })
await page.waitForTimeout(1500) // saving may wait for typing to stop

step('after a reload both notes are there')
await page.reload()
await page.waitForSelector(note)
await page.click('.x-mbar__menu:has-text("View")')
await page.click('text=Show the desktop')
assert.equal(await noteText(), 'buy milk', 'the first note is shown first')
await page.click('[aria-label="Next note"]')
assert.equal(await noteText(), 'call Anna', 'the second note is kept')

step('another browser, paired to the same server, shows the same widgets and notes')
const ctx2 = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'light', serviceWorkers: 'block' })
const other = await ctx2.newPage()
other.on('pageerror', (e) => errors.push(e.message))
await other.goto(BASE + '/#/pair')
await other.fill('[data-test=code]', process.env.CODE2 || '')
await other.click('[data-test=pair]')
for (let i = 0; !(await other.evaluate(() => window.__phe?.state.status === 'online')); i++) { assert(i < 100, 'second browser online'); await other.waitForTimeout(200) }
await other.goto(BASE + '/#/browse')
await openApp(other, 'Desktop')
await other.click('.x-mbar__menu:has-text("View")')
await other.click('text=Show the desktop')
await other.waitForSelector(note)
assert.equal(await other.locator(note).inputValue(), 'buy milk', 'the note reached the other browser')

step('the light mail widget (blue header, white list)')
await page.click('.x-mbar__menu:has-text("View")')
await page.click('text=Edit desktop')
await page.click('[data-test=widget-add-mail-light]')
await page.click('[data-test=widgets-done]')
await page.click('.x-mbar__menu:has-text("View")')
await page.click('text=Show the desktop')
const head = page.locator('[data-test=widget-mail-light-body] .bw-mail__head')
await head.waitFor()
assert.match(await head.evaluate((e) => getComputedStyle(e).backgroundImage), /gradient/, 'the Belle blue header')
await page.screenshot({ path: `${OUT}/widgets-mail-light.png` })

assert.deepEqual(errors, [], 'no page errors')
step('ok')
await browser.close()
