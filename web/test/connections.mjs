// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Connections page: add / test / sync / remove accounts through the real UI against a CardDAV server (Radicale) and an IMAP server
// (the go-imap in-memory server), no hub configured here (so the copy itself is covered by the Go tests).
//   BASE=http://localhost:8080 CODE=<admin code> DAV_URL=http://host:5232/user/collection/ DAV_USER=.. DAV_PASS=.. IMAP_HOST=.. IMAP_PORT=1143 OUT=/out node connections.mjs
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE, DAV_URL, DAV_USER, DAV_PASS, IMAP_HOST, IMAP_PORT } = process.env
assert(CODE && DAV_URL && IMAP_HOST, 'CODE, DAV_URL, IMAP_HOST are required')

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 900, height: 1100 } })
const page = await ctx.newPage()
const log = []
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
async function shots(name) {
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.waitForTimeout(250)
    await page.screenshot({ path: `${OUT}/connections-${scheme}-${name}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online')
await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=no-accounts]')
await page.waitForSelector('[data-test=no-hub]')
await shots('1-empty')
step('Connections page: empty, hub note shown')

// contacts account: wrong password first
await page.click('[data-test=add-account]')
await page.click('[data-test=f-kind]')
await page.getByRole('option', { name: /CardDAV/ }).click()
await page.fill('[data-test=f-name]', 'Radicale contacts')
await page.fill('[data-test=f-url]', DAV_URL)
await page.fill('[data-test=f-user]', DAV_USER)
await page.fill('[data-test=f-secret]', 'definitely-wrong')
await page.click('[data-test=f-test]')
await page.waitForSelector('[data-test=test-result]')
assert.match(await page.textContent('[data-test=test-result]'), /login refused|HTTP 40/i)
step('a wrong password fails the test')
await page.fill('[data-test=f-secret]', DAV_PASS)
await page.click('[data-test=f-test]')
await page.waitForFunction(() => /login works/.test(document.querySelector('[data-test=test-result]')?.textContent || ''))
await shots('2-add-dialog')
await page.click('[data-test=f-add]')
await page.waitForSelector('[data-test=account-row]')
step('CardDAV account added after a good test')

// mail account against the in-memory IMAP server
await page.click('[data-test=add-account]')
await page.fill('[data-test=f-name]', 'Test mail')
await page.fill('[data-test=f-imap-host]', IMAP_HOST)
await page.fill('[data-test=f-imap-port]', IMAP_PORT)
await page.click('[data-test=f-imap-tls]')
await page.getByRole('option', { name: /None/ }).click()
await page.fill('[data-test=f-user]', 'me')
await page.fill('[data-test=f-secret]', 'secret')
await page.click('[data-test=f-test]')
await page.waitForFunction(() => /IMAP login works/.test(document.querySelector('[data-test=test-result]')?.textContent || ''))
await page.click('[data-test=f-add]')
await page.waitForFunction(() => document.querySelectorAll('[data-test=account-row]').length === 2)
step('IMAP account added')

// no hub: a sync reports it plainly, the account shows a problem
await page.locator('[data-test=account-sync]').first().click()
await page.waitForSelector('[data-test=conn-error]')
assert.match(await page.textContent('[data-test=conn-error]'), /hub is not configured/)
await page.waitForFunction(() => /Problem/.test(document.querySelector('[data-test=account-status]').textContent))
await shots('3-accounts')
step('sync without a hub reports the problem on the account')

await page.click('[data-test=find-dups]')
await page.waitForSelector('[data-test=dup-error]')
step('duplicate finder reports the missing hub')

// secrets never come back to the browser
const list = await page.evaluate(async () => JSON.stringify(await (await fetch('/api/connections')).text()))
assert(!list.includes('definitely-wrong') && !list.includes('secret"'))

await page.locator('[data-test=account-remove]').first().click()
await page.waitForSelector('[data-test=remove-dialog]')
await shots('4-remove')
await page.click('[data-test=remove-confirm]')
await page.waitForFunction(() => document.querySelectorAll('[data-test=account-row]').length === 1)
await page.locator('[data-test=account-remove]').first().click()
await page.click('[data-test=remove-confirm]')
await page.waitForSelector('[data-test=no-accounts]')
step('accounts removed')
assert.deepEqual(log, [], 'page errors: ' + log.join('; '))
await browser.close()
console.log('CONNECTIONS PASS')
