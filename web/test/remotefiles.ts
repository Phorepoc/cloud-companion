// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Files of other services through the real UI, against a throwaway Nextcloud (remotefiles.sh: user ann with a folder "CC Photos"
// holding sea.png): Connections → Add → Files: Nextcloud → Sign in with Nextcloud opens Nextcloud, where ann logs in and grants
// access, as a person would; the dialog then has her app password, Test passes, Add. In Files her Nextcloud is a top folder,
// read-only (no upload or new folder, a note saying where the files are); the photo opens from it.
//   BASE=http://localhost:8080 CODE=<admin code> SERVER=http://nextcloud OUT=/out node --experimental-strip-types remotefiles.ts
import { chromium, type Page } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE, SERVER } = process.env
assert(CODE && SERVER, 'CODE and SERVER are required')

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 900, height: 1200 }, serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const shot = async (p: Page, name: string) => {
  for (const scheme of ['light', 'dark'] as const) {
    await p.emulateMedia({ colorScheme: scheme })
    await p.waitForTimeout(250)
    await p.screenshot({ path: `${OUT}/remotefiles-${scheme}-${name}.png` })
  }
  await p.emulateMedia({ colorScheme: 'light' })
}

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
for (let i = 0; !(await page.evaluate(() => window.__phe?.state.status === 'online')); i++) { assert(i < 80, 'not online'); await page.waitForTimeout(250) }
await page.goto(BASE + '/#/connections')
await page.waitForSelector('[data-test=no-accounts]')
await page.click('[data-test=add-account]')
await page.click('[data-test=f-kind]')
await page.getByRole('option', { name: 'Files: Nextcloud' }).click()
await page.fill('[data-test=f-name]', 'My Nextcloud')
assert(await page.locator('[data-test=f-nc-signin]').isDisabled(), 'sign-in without an address')
await page.fill('[data-test=f-url]', SERVER)

// Nextcloud's own pages, in the tab the dialog opens
const [nc] = await Promise.all([ctx.waitForEvent('page'), page.click('[data-test=f-nc-signin]')])
await page.waitForSelector('[data-test=f-nc-waiting]')
await nc.waitForLoadState()
try {
  await nc.getByRole('button', { name: /log in/i }).first().click()
  await nc.locator('input[name=user]').fill('ann')
  await nc.locator('input[name=password]').fill('pw-ann-12345')
  await nc.locator('input[name=password]').press('Enter')
  await nc.getByRole('button', { name: /grant access/i }).click()
  await nc.getByText(/account connected|you can close this window/i).first().waitFor()
} catch (e) { await nc.screenshot({ path: `${OUT}/remotefiles-nextcloud-page.png` }); throw e }
await nc.close()
await page.locator('[data-test=f-nc-done]', { hasText: 'Signed in as ann' }).waitFor({ timeout: 15000 })
await shot(page, '1-signed-in')
step('Sign in with Nextcloud: approved in Nextcloud, the dialog has the app password')

await page.click('[data-test=f-test]')
await page.locator('[data-test=test-result]', { hasText: /signed in: \d+ items at the top/ }).waitFor()
await page.click('[data-test=f-add]')
await page.waitForSelector('[data-test=account-row]')
assert.match((await page.textContent('[data-test=account-row]')) ?? '', /Files \(Nextcloud\)/)
step('Test passes; added as Files (Nextcloud)')

await page.goto(BASE + '/#/files')
const top = page.locator('[data-test="file-My Nextcloud"]')
await top.waitFor()
assert.match((await top.textContent()) ?? '', /Nextcloud/)
await page.click('[data-test="file-more-My Nextcloud"]') // the owner's menu on it: only Show in Photos
assert.equal(await page.locator('[data-test="file-photos-My Nextcloud"]').count(), 1, 'Show in Photos')
assert.equal(await page.locator('[data-test="file-rename-My Nextcloud"], [data-test="file-delete-My Nextcloud"]').count(), 0, 'rename or delete on the account folder')
await page.keyboard.press('Escape')
await shot(page, '2-files-top')
await top.click()
await page.waitForSelector('[data-test=files-readonly]')
assert.match((await page.textContent('[data-test=files-readonly]')) ?? '', /in My Nextcloud/)
assert.equal(await page.locator('[data-test=files-upload]').count(), 0, 'upload offered in a read-only folder')
assert.equal(await page.locator('[data-test=files-mkdir]').count(), 0, 'new folder offered in a read-only folder')
assert.match((await page.textContent('.ph-crumbs')) ?? '', /My Nextcloud/)
step('Files: the account is a top folder; inside, read-only (no upload or new folder) and the crumb shows its name')

await page.click('[data-test="file-CC Photos"]')
await page.waitForSelector('[data-test="file-sea.png"]')
await page.click('[data-test="file-more-sea.png"]')
assert.equal(await page.locator('[data-test="file-share-sea.png"]').count(), 0, 'share offered')
assert.equal(await page.locator('[data-test="file-delete-sea.png"]').count(), 0, 'delete offered')
assert.equal(await page.locator('[data-test="file-rename-sea.png"]').count(), 0, 'rename offered')
await page.keyboard.press('Escape')
await shot(page, '3-folder')
const [photo] = await Promise.all([ctx.waitForEvent('page'), page.click('[data-test="file-sea.png"]')])
await photo.waitForLoadState()
const res = await ctx.request.get(photo.url())
assert.equal(res.status(), 200)
assert.equal(res.headers()['content-type'], 'image/png')
assert.equal((await res.body()).subarray(1, 4).toString(), 'PNG')
await photo.close()
step('in a folder of it: only Download in the menu; the photo opens, streamed from Nextcloud')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
console.log('remotefiles: all passed')
