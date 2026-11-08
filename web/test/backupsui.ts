// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of how Settings, Backups looks and reads, run by backupsui.sh: Cloud Companion with a real host
// manager and no board. Every state is screenshotted at phone width, light and dark (OUT/backups-*.png):
//   off (the three questions), each kind of place, the mistakes a person makes (said before the board is asked), on with a
//   last copy, on with a failed copy.
//   BASE=http://127.0.0.1:8080 OUT=/out CODE=... node --experimental-strip-types backupsui.ts

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const OUT = process.env.OUT || '.'
const CODE = process.env.CODE || ''
const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: 'light', serviceWorkers: 'block', locale: 'en-GB' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const consoleLog: string[] = []
page.on('console', (m) => { if (m.type() === 'error') consoleLog.push(m.text().slice(0, 300)) })
process.on('uncaughtException', async (e) => {
  await page.screenshot({ path: `${OUT}/backups-failed.png` }).catch(() => {})
  console.error('app state:', await page.evaluate(() => JSON.stringify({ status: window.__phe?.state.status, admin: window.__phe?.state.admin, url: location.href })).catch((x) => String(x)))
  console.error('console:', consoleLog.slice(-6).join('\n'), 'page errors:', errors.slice(-3))
  console.error(e); process.exit(1)
})
const visible = (sel: string, ms = 30000) => page.waitForSelector(sel, { state: 'visible', timeout: ms })
const field = (t: string) => page.locator(`input[data-test=${t}], [data-test=${t}] input`).first()
const shot = (name: string) => page.screenshot({ path: `${OUT}/backups-${name}.png`, fullPage: true })
const text = (sel: string) => page.locator(sel).first().textContent({ timeout: 2000 }).catch(() => '')
// the manager's backup.json (its state folder is mounted at STATE): the page's other states without a real backup
const STATE = process.env.STATE || '/state'
const DEST = { kind: 'sftp', path: 'anna@nas.local:cloud-backups', password: 'long enough password' }
const night = (daysAgo: number) => { const d = new Date(); d.setDate(d.getDate() - daysAgo); d.setHours(3, 30, 0, 0); return d.toISOString() }
const setBackup = (last: object) => writeFileSync(`${STATE}/backup.json`, JSON.stringify({ on: true, at: '03:30', dest: DEST, last }))
// a radio's dot animates: let it settle before a screenshot
const settle = () => page.waitForTimeout(400)

await page.goto(`${BASE}/#/pair?code=${CODE}`)
await page.click('[data-test=pair]')
for (let i = 0; i < 60 && !(await page.evaluate(() => window.__phe?.state.admin)); i++) await page.waitForTimeout(500)

// off: three questions, plain words
await page.goto(`${BASE}/#/settings/backups`)
await visible('[data-test=backups-form]')
assert.match(await text('[data-test=backups-status]') || '', /Backups are off/)
assert.equal(await page.locator('[data-test=backups-kind-rest]').count(), 0, 'the expert choice waits behind More choices')
await settle()
await shot('1-off')
await page.click('[data-test=backups-save]')
assert.match(await text('[data-test=backups-error]') || '', /USB drive/, 'no drive: said before asking the board')

await page.click('[data-test=backups-kind-sftp]')
await visible('[data-test=backups-key]')
assert.match(await text('[data-test=backups-key]') || '', /^ssh-ed25519 /)
await settle()
await shot('2-computer')
await field('backups-sftp-host').fill('nas.local')
await field('backups-sftp-user').fill('anna')
await field('backups-password').fill('long enough password')
await field('backups-password2').fill('long enough passwort')
await page.click('[data-test=backups-save]')
assert.match(await text('[data-test=backups-error]') || '', /don't match/)

await page.click('[data-test=backups-kind-s3]')
await visible('[data-test=backups-s3-bucket]')
await settle()
await shot('3-online')
await page.click('[data-test=backups-more]')
await visible('[data-test=backups-kind-rest]')

// on, with a copy last night: the manager's own file is written by backupsui.sh when asked
setBackup({ at: night(0), ok: true, snapshot: '4f2a9c1e' })
await page.reload()
await visible('[data-test=backups-settings]')
assert.match(await text('[data-test=backups-status]') || '', /Your data is backed up/)
assert.match(await text('[data-test=backups-last]') || '', /every night at 03:30\. Last backup today/)
await shot('4-on')
await page.click('[data-test=backups-off]')
await visible('[data-test=backups-off-confirm]')
await settle()
await shot('5-turn-off')
await page.keyboard.press('Escape')

setBackup({ at: night(0), ok: false, error: "the computer or service turned the board away. Check that the board's key is added there, or the password" })
await page.reload()
await visible('[data-test=backups-settings]')
assert.match(await text('[data-test=backups-status]') || '', /The last backup failed/)
await shot('6-failed')

await page.emulateMedia({ colorScheme: 'dark' })
await page.reload()
await visible('[data-test=backups-settings]')
await shot('7-failed-dark')
await page.click('[data-test=backups-where]')
await visible('[data-test=backups-form]')
await shot('8-change-dark')

assert.deepEqual(errors, [], 'no page errors')
console.log('backups ui ok')
await browser.close()
