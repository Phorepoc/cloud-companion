// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Playwright test (Firefox) of Settings, System, run by backupsui.sh (UI_TEST=systemui) against a host manager on a board-like
// container with a real sshd: remote login off by default; on the first time asks for a password (mistakes said on the page);
// off; on again without asking. It is left on with its password in OUT/ssh-password: backupsui.sh then logs in for real.
// Screenshots: OUT/system-*.png.

import { firefox } from 'playwright'
import assert from 'node:assert/strict'
import { writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const OUT = process.env.OUT || '.'
const CODE = process.env.CODE || ''
const PASSWORD = 'tea at the board 42'
const browser = await firefox.launch()
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: 'light', serviceWorkers: 'block', locale: 'en-GB' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
process.on('uncaughtException', async (e) => {
  await page.screenshot({ path: `${OUT}/system-failed.png` }).catch(() => {})
  console.error(e); process.exit(1)
})
const visible = (sel: string, ms = 30000) => page.waitForSelector(sel, { state: 'visible', timeout: ms })
const gone = (sel: string) => page.waitForSelector(sel, { state: 'detached', timeout: 30000 })
const text = (sel: string) => page.locator(sel).first().textContent({ timeout: 2000 }).catch(() => '')
const field = (t: string) => page.locator(`input[data-test=${t}], [data-test=${t}] input`).first()
const toggle = () => page.locator('[data-test=system-ssh-switch] input, [data-test=system-ssh-switch][role=switch], [data-test=system-ssh-switch] [role=switch]').first().click()
const shot = async (name: string) => { await page.waitForTimeout(400); await page.screenshot({ path: `${OUT}/system-${name}.png`, fullPage: true }) }

await page.goto(`${BASE}/#/pair?code=${CODE}`)
await page.click('[data-test=pair]')
for (let i = 0; i < 60 && !(await page.evaluate(() => window.__phe?.state.admin)); i++) await page.waitForTimeout(500)
await page.goto(`${BASE}/#/settings/system`)
await page.reload()
await visible('[data-test=system-ssh]')
assert.match(await text('[data-test=system-ssh-item]') || '', /Off/, 'off by default')
await shot('1-off')

await toggle()
await visible('[data-test=system-ssh-form]')
await field('system-ssh-user').fill('Anna')
await field('system-ssh-password').fill(PASSWORD)
await field('system-ssh-password2').fill(PASSWORD)
await page.click('[data-test=system-ssh-on]')
assert.match(await text('[data-test=system-error]') || '', /small letters/)
await field('system-ssh-user').fill('anna')
await field('system-ssh-password').fill(PASSWORD)
await field('system-ssh-password2').fill(PASSWORD + '!')
await page.click('[data-test=system-ssh-on]')
assert.match(await text('[data-test=system-error]') || '', /not the same/)
await shot('2-choose-password')
await field('system-ssh-password2').fill(PASSWORD)
await page.click('[data-test=system-ssh-on]')
await visible('[data-test=system-ssh-how]')
assert.match(await text('[data-test=system-ssh-how]') || '', /ssh anna@cloud-companion\.local/)
await shot('3-on')

await toggle()
await gone('[data-test=system-ssh-how]')
assert.match(await text('[data-test=system-ssh-item]') || '', /Off/)
await toggle() // on again: the same password, nothing asked
await visible('[data-test=system-ssh-how]')
assert.equal(await page.locator('[data-test=system-ssh-form]').count(), 0, 'the password was asked again')

writeFileSync(`${OUT}/ssh-password`, 'anna ' + PASSWORD + '\n')
await page.emulateMedia({ colorScheme: 'dark' })
await shot('4-on-dark')
assert.deepEqual(errors, [], 'no page errors')
console.log('system ui ok')
await browser.close()
