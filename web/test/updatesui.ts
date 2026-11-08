// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of how Settings, Updates looks and reads, run by backupsui.sh (UI_TEST=updatesui): Cloud Companion
// with a real host manager and no board. The manager's update.json is written here for each state (nothing reaches GitHub);
// switching night updates off goes through the real API. Screenshots: OUT/updates-*.png.

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { readFileSync, writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const OUT = process.env.OUT || '.'
const CODE = process.env.CODE || ''
const STATE = process.env.STATE || '/state'
const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: 'light', serviceWorkers: 'block', locale: 'en-GB' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
process.on('uncaughtException', async (e) => {
  await page.screenshot({ path: `${OUT}/updates-failed.png` }).catch(() => {})
  console.error(e); process.exit(1)
})
const visible = (sel: string, ms = 30000) => page.waitForSelector(sel, { state: 'visible', timeout: ms })
const text = (sel: string) => page.locator(sel).first().textContent({ timeout: 2000 }).catch(() => '')
const shot = async (name: string) => { await page.waitForTimeout(400); await page.screenshot({ path: `${OUT}/updates-${name}.png`, fullPage: true }) }
const ago = (days: number) => { const d = new Date(); d.setDate(d.getDate() - days); d.setHours(3, 31, 0, 0); return d.toISOString() }
const setUpdate = (c: object) => writeFileSync(`${STATE}/update.json`, JSON.stringify({ images: 'v0.3.0', auto: true, checked: ago(0), ...c }))
async function open() {
  await page.goto(`${BASE}/#/settings/updates`)
  await page.reload() // a reload on the page: it still loads (the owner is known only after connecting)
  await visible('[data-test=updates-status]')
}

await page.goto(`${BASE}/#/pair?code=${CODE}`)
await page.click('[data-test=pair]')
for (let i = 0; i < 60 && !(await page.evaluate(() => window.__phe?.state.admin)); i++) await page.waitForTimeout(500)

setUpdate({})
await open()
assert.match(await text('[data-test=updates-status]') || '', /up to date/)
assert.match(await text('[data-test=updates-version]') || '', /Cloud Companion v0\.3\.0/)
assert.equal(await page.locator('[data-test=updates-now]').count(), 0)
await shot('1-up-to-date')

setUpdate({ available: { version: 'v0.4.0', notes: 'Features\n• settings: Backups in plain words\n• release: Mail and Phone calls on a Raspberry Pi' } })
await open()
assert.match(await text('[data-test=updates-status]') || '', /Version v0\.4\.0 is available/)
assert.match(await text('[data-test=updates-text]') || '', /tonight at 03:30/)
await visible('[data-test=updates-now]')
assert.match(await text('[data-test=updates-notes]') || '', /Backups in plain words/)
await shot('2-ready')

// night updates off: through the manager, kept in its file
await page.click('[data-test=updates-auto] input, [data-test=updates-auto] [role=switch]')
for (let i = 0; i < 20 && JSON.parse(readFileSync(`${STATE}/update.json`, 'utf8')).auto; i++) await page.waitForTimeout(250)
assert.equal(JSON.parse(readFileSync(`${STATE}/update.json`, 'utf8')).auto, false, 'switching off did not reach the manager')
for (let i = 0; i < 20 && !/whenever suits you/.test(await text('[data-test=updates-text]') || ''); i++) await page.waitForTimeout(250)
assert.match(await text('[data-test=updates-text]') || '', /whenever suits you/)
await shot('3-auto-off')

setUpdate({ images: 'v0.3.0', last: { at: ago(1), from: 'v0.3.0', to: 'v0.4.0', ok: false, error: 'the update to v0.4.0 did not start, so the board went back to v0.3.0: Cloud Companion is unhealthy' },
  available: { version: 'v0.4.0' } })
await open()
assert.match(await text('[data-test=updates-last]') || '', /failed.*v0\.3\.0 was restored\. No data was lost/)
await shot('4-went-back')
await page.emulateMedia({ colorScheme: 'dark' })
await shot('5-went-back-dark')

// the sentences read as sentences: a check that failed, the last update, the automatic updates line
setUpdate({ error: "Couldn't reach GitHub (dial tcp: no route to host)", last: { at: ago(2), from: 'v0.2.0', to: 'v0.3.0', ok: true } })
await open()
assert.match(await text('[data-test=updates-text]') || '', /^The last check didn't get an answer \(dial tcp: no route to host\)\. Cloud Companion checks again tomorrow, or you can check now\.$/)
assert.match(await text('[data-test=updates-last]') || '', /Updated from v0\.2\.0 to v0\.3\.0 on /)
assert.match(await text('[data-test=updates-auto]') || '', /New versions install at 03:30/)
await shot('7-check-failed')
setUpdate({ available: { version: 'v0.4.0' } })

// opened while an update is being installed (another browser started it, or this page was reloaded): it follows that update
await page.emulateMedia({ colorScheme: 'light' })
const running = { id: 'aa11bb22cc33', kind: 'update', state: 'running', log: ['backing up first'] }
await page.route(/\/phe\/host\/jobs$/, (r) => r.fulfill({ json: { jobs: [running] } }))
await page.route(/\/phe\/host\/jobs\/aa11bb22cc33/, (r) => r.fulfill({ json: running }))
await open()
for (let i = 0; i < 20 && !/Backing up first/.test(await text('[data-test=updates-job]') || ''); i++) await page.waitForTimeout(250)
assert.match(await text('[data-test=updates-job]') || '', /Backing up first/, 'the running update is shown')
assert.equal(await page.locator('[data-test=updates-now][disabled], [data-test=updates-now]:disabled, [data-test=updates-now][aria-disabled=true]').count(), 1, 'Update now waits for it')
await shot('6-rejoined')
await page.unroute(/\/phe\/host\/jobs$/); await page.unroute(/\/phe\/host\/jobs\/aa11bb22cc33/)

assert.deepEqual(errors, [], 'no page errors')
console.log('updates ui ok')
await browser.close()
