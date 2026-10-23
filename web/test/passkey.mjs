// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Owner sign-in (passkeys + recovery code) with a Chromium VIRTUAL authenticator:
//   BASE=http://phecloud:8080 CODES=<admin>,<plain>,<plain> OUT=/out node passkey.mjs
// WebAuthn needs a secure origin: the test container shares the server's network namespace and uses http://localhost:8080
// (real use: https, or localhost).
import { chromium } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const [ADMIN, PLAIN1, PLAIN2] = (process.env.CODES || '').split(',')
const OUT = process.env.OUT || '.'
assert(ADMIN && PLAIN1 && PLAIN2, 'CODES=<admin>,<plain>,<plain> is required')

const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 900, height: 1000 } })
const page = await ctx.newPage()
const log = []
page.on('pageerror', (e) => log.push('pageerror: ' + e.message))
const t0 = Date.now()
const step = (s) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)

const cdp = await ctx.newCDPSession(page)
await cdp.send('WebAuthn.enable')
const AUTH_OPTS = { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true }
let { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', { options: AUTH_OPTS })
const creds = async () => (await cdp.send('WebAuthn.getCredentials', { authenticatorId })).credentials

async function shots(name) {
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.waitForTimeout(250)
    await page.screenshot({ path: `${OUT}/passkey-${scheme}-${name}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
}
const online = () => page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })
const forget = async () => {
  await page.evaluate(() => window.__phe.forget())
  await page.goto(BASE)
  await page.waitForSelector('[data-test=code]')
}
const status = async () => (await page.evaluate(() => fetch('/passkey/status').then((r) => r.json())))

// 1. no owner yet: the pairing page offers to set up a passkey
await page.goto(BASE)
await page.waitForSelector('[data-test=passkey-register]')
assert.equal((await status()).owner, false)
assert.equal(await page.locator('[data-test=passkey-login]').count(), 0, 'no passkey sign-in before an owner exists')
await shots('1-setup')
step('setup page: no owner, passkey registration offered')

// 2. a plain (non-admin) code must not register a passkey
await page.fill('[data-test=code]', PLAIN1)
await page.click('[data-test=passkey-register]')
await page.waitForSelector('[data-test=pair-error]')
assert.match(await page.textContent('[data-test=pair-error]'), /admin/i)
assert.equal((await creds()).length, 0)
step('a non-admin code is refused, no credential was made')

// 3. an admin code registers the owner passkey and shows the recovery code once
await page.fill('[data-test=code]', ADMIN)
await page.click('[data-test=passkey-register]')
await page.waitForSelector('[data-test=recovery-dialog]')
let rec1 = (await page.textContent('[data-test=recovery-shown]')).trim()
assert.match(rec1, /^[A-Z2-9]{4}(-[A-Z2-9]{4}){3}$/)
assert.equal((await creds()).length, 1)
await shots('2-recovery-code')
await page.click('[data-test=recovery-saved]')
await online()
assert.equal(await page.evaluate(() => window.__phe.state.admin), true)
assert.equal((await status()).owner, true)
step(`owner passkey registered (${rec1.slice(0, 4)}-....), signed in as admin`)

// 4. sign out; the page now offers the passkey
await forget()
await page.waitForSelector('[data-test=passkey-login]')
await shots('3-signin')
await page.click('[data-test=passkey-login]')
await online()
assert.equal(await page.evaluate(() => window.__phe.state.admin), true)
const cred = (await creds())[0]
assert(cred.signCount >= 1, 'the assertion bumped the sign counter')
step('sign in with the passkey')

// 4b. Settings -> Security: list, add (signed in, no code), new recovery code, remove
await page.goto(BASE + '/#/settings')
await page.waitForSelector('[data-test=security]')
await online()
await page.waitForSelector('[data-test=passkey-row]')
assert.equal(await page.locator('[data-test=passkey-row]').count(), 1)
assert.equal(await page.locator('[data-test=passkey-blocker]').count(), 0, 'localhost is fine for passkeys')
// a second passkey needs another authenticator (the first one refuses a duplicate): swap in a fresh virtual device
await cdp.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId })
;({ authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', { options: AUTH_OPTS }))
await page.fill('[data-test=passkey-name]', 'Second key')
await page.click('[data-test=passkey-add]')
await page.waitForSelector('[data-test=passkey-row]:nth-child(2), [data-test=security-error]')
assert.equal(await page.locator('[data-test=security-error]').count(), 0, 'add failed: ' + (await page.locator('[data-test=security-error]').allTextContents()).join())
await page.waitForFunction(() => document.querySelectorAll('[data-test=passkey-row]').length === 2)
assert.equal((await creds()).length, 1, 'the second authenticator holds the new passkey')
assert.equal(await page.locator('[data-test=recovery-dialog]').count(), 0, 'adding a second passkey shows no new recovery code')
assert.match(await page.locator('[data-test=passkey-row]').nth(1).textContent(), /Second key/)
assert.match(await page.locator('[data-test=passkey-row]').first().textContent(), /last used/)
await page.locator('[data-test=security]').scrollIntoViewIfNeeded()
await shots('4-security')
step('Security: two passkeys listed (name, added, last used); one added while signed in, no code')
await page.click('[data-test=recovery-regen]')
await page.waitForSelector('[data-test=recovery-dialog]')
const recNew = (await page.textContent('[data-test=recovery-shown]')).trim()
assert.match(recNew, /^[A-Z2-9]{4}(-[A-Z2-9]{4}){3}$/)
assert.notEqual(recNew, rec1)
await shots('5-new-recovery')
await page.click('[data-test=recovery-saved]')
const stale = await page.evaluate(async (c) => (await fetch('/passkey/recover', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code: c, name: 'x' }) })).status, rec1)
assert.equal(stale, 403, 'the old recovery code stopped working')
step('a new recovery code replaces the old one')
await page.locator('[data-test=passkey-remove]').nth(1).click()
await page.waitForSelector('[data-test=remove-dialog]')
await shots('6-remove-confirm')
await page.click('[data-test=remove-confirm]')
await page.waitForFunction(() => document.querySelectorAll('[data-test=passkey-row]').length === 1)
step('a passkey is removed after confirmation')
// the recovery code that exists now is recNew (rec1 was replaced): keep it for step 5
rec1 = recNew
// a browser that is not signed in cannot use these endpoints
const anon = await page.evaluate(async () => (await fetch('/passkey/list')).status)
assert.equal(anon, 403)
// opened by an IP address: passkeys need a domain name
const ip = await ctx.newPage()
await ip.goto('http://127.0.0.1:8080/#/pair')
await ip.waitForSelector('[data-test=passkey-blocker]')
assert.match(await ip.textContent('[data-test=passkey-blocker]'), /IP address/)
await ip.screenshot({ path: `${OUT}/passkey-light-7-ip-note.png` })
await ip.close()
step('an IP address shows the "passkeys need a domain name" note')

// 5. recovery code: typed sloppily, works once, a new one replaces it
await forget()
await page.click('[data-test=recovery-toggle]')
await page.fill('[data-test=recovery-code]', 'nope-nope-nope-nope')
await page.click('[data-test=recovery-go]')
await page.waitForSelector('[data-test=pair-error]')
assert.match(await page.textContent('[data-test=pair-error]'), /did not work/)
await page.fill('[data-test=recovery-code]', rec1.toLowerCase().replace(/-/g, ' '))
await page.click('[data-test=recovery-go]')
await page.waitForSelector('[data-test=recovery-dialog]')
const rec2 = (await page.textContent('[data-test=recovery-shown]')).trim()
assert.notEqual(rec2, rec1)
await page.click('[data-test=recovery-saved]')
await online()
step('recovery code signs in once and is replaced')
await forget()
await page.click('[data-test=recovery-toggle]')
await page.fill('[data-test=recovery-code]', rec1)
await page.click('[data-test=recovery-go]')
await page.waitForSelector('[data-test=pair-error]')
step('the used recovery code no longer works')

// 6. device pairing is unchanged
await page.fill('[data-test=code]', PLAIN2)
await page.click('[data-test=pair]')
await online()
assert.equal(await page.evaluate(() => window.__phe.state.admin), false, 'a plain code pairs a non-admin device')
step('a plain pairing code still pairs a device')

assert.deepEqual(log, [], 'page errors: ' + log.join('; '))
await browser.close()
console.log('PASSKEY PASS')
