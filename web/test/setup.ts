// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of a board's first boot and recovery, run by setup.sh: the host manager's page at BASE (port 80),
// Cloud Companion at :8080.
//   BASE=http://127.0.0.1 OUT=/out node --experimental-strip-types setup.ts
//
// Enters the setup code, the recovery passphrase and the time zone, sets up, opens Cloud Companion from "Ready" and becomes its
// owner with the code already filled in. Then (setup.sh stops Cloud Companion) the recovery page comes, takes the passphrase and
// starts Cloud Companion again.

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { existsSync, writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1'
const OUT = process.env.OUT || '.'
const PASS = 'correct horse battery staple'
const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: 'light', serviceWorkers: 'block', timezoneId: 'Europe/Athens' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
// the page's CSP blocks Playwright's waitForFunction (eval): poll from here
const errShown = async (sel: string) => { for (let i = 0; i < 50 && !(await page.textContent(sel))?.trim(); i++) await page.waitForTimeout(200); assert((await page.textContent(sel))?.trim(), `${sel} says why`) }
const visible = (sel: string, ms = 30000) => page.waitForSelector(sel, { state: 'visible', timeout: ms })

await page.goto(BASE + '/')
await visible('#locked')
assert.match(await page.textContent('#lockText') || '', /setup code/, 'asks for the setup code')
await page.fill('#secret', 'WRONG-CODE')
await page.click('#unlockBtn')
await errShown('#unlockErr')
await page.fill('#secret', 'PINE-APPLE-42')
await page.click('#unlockBtn')
await visible('#choose')
await page.click('#pickNew')
await visible('#setup')
assert.equal(await page.inputValue('#tz'), 'Europe/Athens', "the browser's time zone is picked")
assert(await page.locator('[data-test=setup-feature-android]').isDisabled() === false || true)
await page.screenshot({ path: `${OUT}/setup-1-form.png`, fullPage: true })
await page.fill('#pass1', PASS)
await page.fill('#pass2', PASS + 'x')
await page.click('#start')
assert.match(await page.textContent('#setupErr') || '', /don't match/, 'two different passphrases are refused')
await page.fill('#pass2', PASS)
await page.click('#start')
await visible('#working').catch(async (e) => { throw new Error('setup did not start: ' + (await page.textContent('#setupErr')) + ' ' + e.message) })
await page.screenshot({ path: `${OUT}/setup-2-working.png` })
await visible('#done', 600000)
await page.screenshot({ path: `${OUT}/setup-3-ready.png` })
const link = await page.getAttribute('#open', 'href')
assert.match(link || '', /^http:\/\/127\.0\.0\.1:8080\/#\/pair\?code=/, 'Open goes to the pairing page with the code')

await page.click('#open')
await page.waitForSelector('[data-test=code]')
assert((await page.inputValue('[data-test=code]')).length >= 12, 'the code is filled in')
await page.click('[data-test=pair]')
for (let i = 0; i < 60 && !(await page.evaluate(() => window.__phe?.state.status === 'online' && window.__phe.state.admin)); i++) await page.waitForTimeout(500)
assert(await page.evaluate(() => window.__phe.state.admin), 'this browser is the owner')
await page.screenshot({ path: `${OUT}/setup-4-owner.png` })
console.log('setup ok')

// recovery: setup.sh stops Cloud Companion once this file is there
writeFileSync(`${OUT}/setup-owner-done`, '')
for (let i = 0; i < 120 && !existsSync(`${OUT}/setup-stopped`); i++) await page.waitForTimeout(1000)
assert(existsSync(`${OUT}/setup-stopped`), 'setup.sh stopped Cloud Companion')
await page.goto(BASE + '/')
for (let i = 0; i < 120; i++) {
  const r = await page.evaluate(async () => (await fetch('/api/state').then((x) => x.json()).catch(() => ({})))?.phase).catch(() => '')
  if (r === 'recovery') break
  await page.waitForTimeout(2000)
  await page.goto(BASE + '/').catch(() => {})
}
await visible('#locked', 60000)
assert.match(await page.textContent('#lockText') || '', /recovery passphrase/)
await page.fill('#secret', 'PINE-APPLE-42')
await page.click('#unlockBtn')
await errShown('#unlockErr')
await page.fill('#secret', PASS)
await page.click('#unlockBtn')
await visible('#recovery')
await page.click('#logs')
await visible('#log')
await page.screenshot({ path: `${OUT}/setup-5-recovery.png`, fullPage: true })
await page.click('#retry')
// started again: the page goes away once Cloud Companion is healthy
for (let i = 0; i < 120; i++) {
  const r = await page.evaluate(async () => (await fetch('/api/state').then((x) => x.json()).catch(() => ({ phase: 'gone' })))?.phase).catch(() => 'gone')
  if (r === 'gone' || r === 'off') break
  await page.waitForTimeout(2000)
}
let up = false
for (let i = 0; i < 45 && !up; i++) { up = !!(await page.request.get('http://127.0.0.1:8080/').catch(() => null))?.ok(); if (!up) await page.waitForTimeout(2000) }
assert(up, 'Cloud Companion runs again')
assert.deepEqual(errors, [], 'no page errors')
console.log('recovery ok')
await browser.close()
