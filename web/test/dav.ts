// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
//
// Files over WebDAV, the part in the browser (dav.sh runs rclone against it after): Connections → Messaging → Phone logins →
// add "laptop" and note the password; Files → upload hello.txt; Files → Use from other apps shows the address (this site's /dav/).
// Writes "<address> <login> <password>" to $OUT/dav-login for dav.sh.
import { chromium } from 'playwright'
import { tool } from './chrome.ts'
import assert from 'node:assert/strict'
import { writeFileSync } from 'node:fs'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE } = process.env
assert(CODE, 'CODE is required')
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 900, height: 1000 }, serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
for (let i = 0; !(await page.evaluate(() => window.__phe?.state.status === 'online')); i++) { assert(i < 80, 'not online'); await page.waitForTimeout(250) }

await page.goto(BASE + '/#/connections')
await page.fill('[data-test=sip-login-name]', 'laptop')
await page.click('[data-test=sip-login-add]')
await page.waitForSelector('[data-test=sip-login-made]')
const pass = (await page.textContent('[data-test=sip-login-password]'))?.trim() ?? ''
assert.match((await page.textContent('[data-test=sip-login-made]')) ?? '', /file manager \(WebDAV/)
step('Connections: a phone login made; the note says it opens Files over WebDAV')

await page.goto(BASE + '/#/files')
await page.waitForSelector('[data-test=files-page]')
await page.setInputFiles('[data-test=files-input]', { name: 'hello.txt', mimeType: 'text/plain', buffer: Buffer.from('hello from the browser') })
await page.waitForSelector('[data-test="file-hello.txt"]')
await tool(page, 'Use from other apps')
const url = await page.inputValue('[data-test=files-dav-url]')
assert.equal(url, BASE + '/dav/')
for (const s of ['light', 'dark'] as const) { await page.emulateMedia({ colorScheme: s }); await page.waitForTimeout(300); await page.screenshot({ path: `${OUT}/dav-${s}-dialog.png` }) }
step('Files: Use from other apps shows ' + url)

assert.deepEqual(errors, [], 'no page errors')
writeFileSync(`${OUT}/dav-login`, `${url} laptop ${pass}\n`)
await browser.close()
