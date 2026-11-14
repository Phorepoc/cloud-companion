// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Playwright test (Firefox) of Settings → Features on a board: Cloud Companion started by the host manager (phehost), run by features.sh
//   BASE=http://127.0.0.1:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types features.ts
//
// Pairs as the owner, opens Features, sees the board and the features (Cloud Companion always on), switches Signal on and waits
// until the switch stays on; then waits for features.sh to check the containers ($OUT/features-checked) and switches it off.

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { existsSync, writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light', serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const isOn = (id: string) => page.locator(`[data-test=feature-switch-${id}]`).getAttribute('aria-checked')

await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
for (let i = 0; i < 60 && !(await page.evaluate(() => window.__phe?.state.status === 'online' && window.__phe.state.admin)); i++) await page.waitForTimeout(500)
await page.goto(BASE + '/#/settings/features')
await page.waitForSelector('[data-test=features-list]', { timeout: 30000 })
assert.match(await page.textContent('[data-test=features-board]') || '', /GB memory/, 'the board is named')
assert.equal(await isOn('core'), 'true', 'Cloud Companion is on')
assert(await page.locator('[data-test=feature-switch-core]').evaluate((e) => e.classList.contains('disabled') || e.getAttribute('aria-disabled') === 'true'), 'Cloud Companion cannot be switched off')
assert.equal(await page.textContent('[data-test=feature-note-core]'), 'Always on')

async function flip(phase: 'on' | 'off') {
  const want = phase === 'on' ? 'true' : 'false'
  assert.notEqual(await isOn('signal'), want, `Signal is not ${phase} yet`)
  await page.click('[data-test=feature-switch-signal]')
  await page.waitForSelector('[data-test=feature-job-signal]', { state: 'attached', timeout: 10000 }) // empty again when it ends fast
  await page.screenshot({ path: `${OUT}/features-${phase}-working.png` })
  for (let i = 0; i < 1200 && (await isOn('signal')) !== want; i++) await page.waitForTimeout(500)
  assert.equal(await isOn('signal'), want, `Signal switched ${phase}`)
  assert.equal((await page.textContent('[data-test=feature-job-signal]').catch(() => ''))?.trim() || '', '', 'no error under Signal')
  await page.screenshot({ path: `${OUT}/features-${phase}.png` })
  console.log('features', phase, 'ok')
}
await flip('on')
writeFileSync(`${OUT}/features-on-done`, '')
for (let i = 0; i < 120 && !existsSync(`${OUT}/features-checked`); i++) await page.waitForTimeout(500)
assert(existsSync(`${OUT}/features-checked`), 'features.sh checked the containers')
await flip('off')
// opened during a backup (started at night, or in another browser): the switches wait for it, and say why
const backup = { id: 'bb11cc22dd33', kind: 'backup', state: 'running', log: [] }
await page.route(/\/phe\/host\/jobs$/, (r) => r.fulfill({ json: { jobs: [backup] } }))
await page.route(/\/phe\/host\/jobs\/bb11cc22dd33/, (r) => r.fulfill({ json: backup }))
await page.reload()
await page.waitForSelector('[data-test=features-waiting]', { timeout: 30000 })
assert.match(await page.textContent('[data-test=features-waiting]') || '', /A backup is running\. Features can be turned on or off when it's done\./)
assert(await page.locator('[data-test=feature-switch-signal]').evaluate((e) => e.classList.contains('disabled') || e.getAttribute('aria-disabled') === 'true'), 'switches wait for the backup')
await page.screenshot({ path: `${OUT}/features-waiting.png` })
await page.unroute(/\/phe\/host\/jobs\/bb11cc22dd33/)
await page.route(/\/phe\/host\/jobs\/bb11cc22dd33/, (r) => r.fulfill({ json: { ...backup, state: 'done' } }))
await page.waitForSelector('[data-test=features-waiting]', { state: 'detached', timeout: 15000 })
await page.unroute(/\/phe\/host\/jobs$/); await page.unroute(/\/phe\/host\/jobs\/bb11cc22dd33/)
console.log('features waiting ok')
assert.deepEqual(errors, [], 'no page errors')
await browser.close()
