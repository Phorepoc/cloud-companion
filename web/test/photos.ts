// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Photos through the real UI (photos.sh: a throwaway Immich next to Cloud Companion). Immich gets an owner, an API key, two photos
// (2024 in the album "Summer", 2023) through its own API, as its app would. Then, as a person would: Connections → Add →
// Photos: Immich, Test, Add; Files → a new folder "Camera" with a photo uploaded, and "Show in Photos" from its menu. Photos shows
// all three newest first under their months; a photo opens in the viewer, and the arrows go to the older one; Albums has "Summer"
// and "Camera"; an album shows its photo; removing the photo folder takes its photo off the timeline.
//   BASE=http://localhost:8080 CODE=<admin code> SERVER=http://immich:2283 OUT=/out node --experimental-strip-types photos.ts
import { chromium, type Page } from 'playwright'
import assert from 'node:assert/strict'
import { deflateSync } from 'node:zlib'

const BASE = process.env.BASE || 'http://localhost:8080'
const OUT = process.env.OUT || '.'
const { CODE, SERVER } = process.env
assert(CODE && SERVER, 'CODE and SERVER are required')
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)

// a w×h PNG of one colour
function png(w: number, h: number, rgb: [number, number, number]) {
  const crcT = Array.from({ length: 256 }, (_, n) => { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0 })
  const crc = (b: Buffer) => { let c = 0xffffffff; for (const x of b) c = crcT[(c ^ x) & 0xff]! ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0 }
  const chunk = (t: string, d: Buffer) => { const td = Buffer.concat([Buffer.from(t), d]); const o = Buffer.alloc(8 + d.length + 4); o.writeUInt32BE(d.length); td.copy(o, 4); o.writeUInt32BE(crc(td), 8 + d.length); return o }
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w); ihdr.writeUInt32BE(h, 4); ihdr.set([8, 2, 0, 0, 0], 8)
  const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: w }, () => rgb).flat())])
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(Buffer.concat(Array(h).fill(row)))), chunk('IEND', Buffer.alloc(0))])
}

// Immich, set up through its own API
for (let i = 0, last = ''; ; i++) {
  if (await fetch(SERVER + '/api/server/ping').then((r) => r.ok || !(last = String(r.status)), (e) => { last = String(e.cause ?? e); return false })) break
  assert(i < 120, 'Immich did not start: ' + last); await new Promise((r) => setTimeout(r, 3000))
}
const im = async (p: string, init: RequestInit & { json?: unknown; token?: string; key?: string } = {}) => {
  const h: Record<string, string> = {}
  if (init.json !== undefined) h['Content-Type'] = 'application/json'
  if (init.token) h.Authorization = 'Bearer ' + init.token
  if (init.key) h['x-api-key'] = init.key
  const r = await fetch(SERVER + '/api' + p, { ...init, headers: h, body: init.json !== undefined ? JSON.stringify(init.json) : init.body })
  assert(r.ok, `${p}: ${r.status} ${await r.clone().text()}`)
  return r.json()
}
await im('/auth/admin-sign-up', { method: 'POST', json: { email: 'ann@example.org', password: 'pw-ann-12345', name: 'Ann' } })
const { accessToken } = await im('/auth/login', { method: 'POST', json: { email: 'ann@example.org', password: 'pw-ann-12345' } })
const { secret: KEY } = await im('/api-keys', { method: 'POST', token: accessToken, json: { name: 'Cloud Companion', permissions: ['all'] } })
const upload = async (name: string, when: string, rgb: [number, number, number]) => {
  const f = new FormData()
  f.append('assetData', new Blob([png(320, 240, rgb)], { type: 'image/png' }), name)
  for (const [k, v] of Object.entries({ fileCreatedAt: when, fileModifiedAt: when, deviceAssetId: name, deviceId: 'cc-test' })) f.append(k, v)
  return (await im('/assets', { method: 'POST', key: KEY, body: f })).id as string
}
const sea = await upload('sea.png', '2024-07-01T10:00:00Z', [30, 110, 220])
await upload('snow.png', '2023-01-05T09:00:00Z', [235, 238, 245])
await im('/albums', { method: 'POST', token: accessToken, json: { albumName: 'Summer', assetIds: [sea] } })
step('Immich: owner, API key, two photos and the album Summer')

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 900, height: 1100 }, serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const shot = async (p: Page, name: string) => {
  for (const scheme of ['light', 'dark'] as const) {
    await p.emulateMedia({ colorScheme: scheme })
    await p.waitForTimeout(300)
    await p.screenshot({ path: `${OUT}/photos-${scheme}-${name}.png` })
  }
  await p.emulateMedia({ colorScheme: 'light' })
}
const poll = async (what: string, f: () => Promise<boolean>, tries = 80) => {
  for (let i = 0; !(await f()); i++) { assert(i < tries, what); await page.waitForTimeout(250) }
}
const names = () => page.locator('[data-test=photos-grid] button').evaluateAll((l) => l.map((b) => b.getAttribute('aria-label')))
// every picture in the grid shown (Immich makes thumbnails in the background: reload until it has)
const allShown = () => page.locator('[data-test=photos-grid] img').evaluateAll((l) => l.length > 0 && l.every((i) => (i as HTMLImageElement).complete && (i as HTMLImageElement).naturalWidth > 0))

await page.goto(BASE)
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await poll('online', () => page.evaluate(() => window.__phe?.state.status === 'online'))

await page.goto(BASE + '/#/connections')
await page.click('[data-test=add-account]')
await page.click('[data-test=f-kind]')
await page.getByRole('option', { name: 'Photos: Immich' }).click()
await page.fill('[data-test=f-name]', 'Home photos')
await page.fill('[data-test=f-url]', SERVER)
await page.fill('[data-test=f-secret]', KEY)
await page.click('[data-test=f-test]')
await page.waitForSelector('[data-test=test-result]')
await poll('Test', async () => !/testing/i.test((await page.textContent('[data-test=test-result]')) ?? ''))
assert.match((await page.textContent('[data-test=test-result]')) ?? '', /signed in: 1 item at the top/)
await page.click('[data-test=f-add]')
await page.waitForSelector('[data-test=account-row]')
assert.match((await page.textContent('[data-test=account-row]')) ?? '', /Photos \(Immich\)/)
step('Connections: Immich added; Test found its one album')

await page.goto(BASE + '/#/files')
await page.waitForSelector('[data-test="file-Home photos"]')
await page.click('[data-test=files-mkdir]')
await page.fill('[data-test=files-name-input]', 'Camera')
await page.click('[data-test=files-name-save]')
await page.click('[data-test=file-Camera]')
await page.setInputFiles('[data-test=files-input]', { name: 'hill.png', mimeType: 'image/png', buffer: png(320, 240, [60, 160, 70]) })
await page.waitForSelector('[data-test="file-hill.png"]')
await page.click('[data-test=files-crumb-root]')
await page.click('[data-test=file-more-Camera]')
await page.click('[data-test=file-photos-Camera]')
await page.getByText('Camera shows in Photos').waitFor()
step('Files: a folder Camera with a photo, marked Show in Photos from its menu')

await page.goto(BASE + '/#/photos')
await page.waitForSelector('[data-test=photos-grid]')
await poll('the timeline', async () => (await names()).join() === 'hill.png,sea.png,snow.png')
for (let i = 0; !(await allShown()); i++) { assert(i < 30, 'thumbnails'); await page.waitForTimeout(1000); if (i % 5 === 4) { await page.click('[data-test=photos-tab-albums]'); await page.click('[data-test=photos-tab-timeline]'); await page.waitForSelector('[data-test=photos-grid]') } }
const months = await page.locator('.ph-month h2').allTextContents()
assert.equal(months.length, 3, 'one heading per month: ' + months)
assert.match(months[1]!, /July 2024/)
await shot(page, '1-timeline')
step('Photos: the folder photo and both Immich photos, newest first, under their months, thumbnails shown')

await page.click('[data-test="photo-sea.png"]')
await page.waitForSelector('[data-test=photos-viewer]')
await poll('the photo', () => page.locator('[data-test=photos-image]').evaluate((i) => (i as HTMLImageElement).naturalWidth > 0), 120)
assert.equal(await page.getAttribute('[data-test=photos-image]', 'alt'), 'sea.png')
await shot(page, '2-viewer')
await page.click('[data-test=photos-next]')
await poll('the older photo', async () => (await page.getAttribute('[data-test=photos-image]', 'alt')) === 'snow.png')
assert.equal(await page.locator('[data-test=photos-next]').count(), 0, 'nothing older than the oldest')
await page.keyboard.press('ArrowLeft')
await poll('back with the arrow key', async () => (await page.getAttribute('[data-test=photos-image]', 'alt')) === 'sea.png')
await page.keyboard.press('Escape')
await page.waitForSelector('[data-test=photos-viewer]', { state: 'detached' })
step('the viewer: a preview of the photo, Older and the arrow key, Escape closes')

await page.click('[data-test=photos-tab-albums]')
await page.waitForSelector('[data-test=photos-albums]')
const albums = await page.locator('.ph-palbum b').allTextContents()
assert.deepEqual(albums, ['Camera', 'Summer'])
await poll('album covers', () => page.locator('.ph-palbum img').evaluateAll((l) => l.length === 2 && l.every((i) => (i as HTMLImageElement).naturalWidth > 0)))
await shot(page, '3-albums')
await page.click('[data-test=photos-album-Summer]')
await poll('the album', async () => (await names()).join() === 'sea.png')
assert.equal(await page.textContent('h1'), 'Summer')
await page.click('[data-test=photos-back]')
await page.waitForSelector('[data-test=photos-albums]')
step('Albums: the photo folder and the Immich album, with covers; Summer holds sea.png')

await page.click('[data-test=photos-folders]')
await page.click('[data-test=photos-folder-remove-Camera]')
await page.waitForSelector('[data-test=photos-folders-none]')
await page.keyboard.press('Escape')
await page.click('[data-test=photos-tab-timeline]')
await poll('the folder photo gone', async () => (await names()).join() === 'sea.png,snow.png')
step('Photo folders: removing Camera takes its photo off the timeline')

assert.deepEqual(errors, [], 'no page errors')
await browser.close()
console.log('photos: all passed')
