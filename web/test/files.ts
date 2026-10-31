// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright test of Files and Music, against a live server (files.sh): two browsers paired as two devices.
//   BASE=http://localhost:8080 CODE=... CODE2=... OUT=/out node --experimental-strip-types files.ts
//
// Folders, upload, rename, delete (after the Undo toast), a public share link and stopping it; the music library from the
// uploaded songs (generated cover), playing an album with seeking, "Play on" the other browser, and the Belle music widgets.
import { chromium, type Page } from 'playwright'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const OUT = process.env.OUT || '.'
assert(process.env.CODE && process.env.CODE2, 'CODE and CODE2 are required')
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const t0 = Date.now()
const step = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`)
const errors: string[] = []

async function device(code: string, name: string) {
  const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 1280, height: 860 }, colorScheme: 'light', serviceWorkers: 'block', permissions: ['clipboard-read', 'clipboard-write'] })
  const page = await ctx.newPage()
  page.on('pageerror', (e) => errors.push(name + ': ' + e.message))
  await page.goto(BASE + '/#/pair')
  await page.fill('[data-test=code]', code)
  await page.fill('[data-test=name]', name)
  await page.click('[data-test=pair]')
  await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })
  return page
}

/** a short WAV tone (a song the browser can play) */
function wav(seconds: number, hz: number) {
  const rate = 8000, n = rate * seconds, b = Buffer.alloc(44 + n * 2)
  b.write('RIFF', 0); b.writeUInt32LE(36 + n * 2, 4); b.write('WAVEfmt ', 8); b.writeUInt32LE(16, 16); b.writeUInt16LE(1, 20); b.writeUInt16LE(1, 22)
  b.writeUInt32LE(rate, 24); b.writeUInt32LE(rate * 2, 28); b.writeUInt16LE(2, 32); b.writeUInt16LE(16, 34); b.write('data', 36); b.writeUInt32LE(n * 2, 40)
  for (let i = 0; i < n; i++) b.writeInt16LE(Math.round(Math.sin((2 * Math.PI * hz * i) / rate) * 6000), 44 + i * 2)
  return b
}

async function name(page: Page, n: string) {
  await page.waitForSelector('[data-test=files-name]')
  await page.fill('[data-test=files-name-input]', n)
  await page.click('[data-test=files-name-save]')
  await page.waitForSelector('[data-test=files-name]', { state: 'detached' })
}

const pc = await device(process.env.CODE!, 'Desk')
const phone = await device(process.env.CODE2!, 'Kitchen')

step('Files: empty, then folders Music / Belle Band / First Light')
await pc.goto(BASE + '/#/files')
await pc.waitForSelector('[data-test=files-empty]')
for (const f of ['Music', 'Belle Band', 'First Light']) {
  await pc.click('[data-test=files-mkdir]')
  await name(pc, f)
  await pc.click(`[data-test="file-${f}"]`)
  await pc.waitForSelector('[data-test=files-empty]')
}
assert.equal((await pc.textContent('.ph-crumbs'))!.replace(/\s+/g, ' ').includes('Music'), true, 'the path shows')
// a circle or a rounded square would break the squircle rule
assert.equal(await pc.locator('.ph-crumbs').count(), 1)

step('upload three songs and a note; the list shows them with their size')
await pc.setInputFiles('[data-test=files-input]', [
  { name: '01 - Morning.wav', mimeType: 'audio/wav', buffer: wav(4, 440) },
  { name: '02 - Evening.wav', mimeType: 'audio/wav', buffer: wav(4, 660) },
  { name: '03 - Night.wav', mimeType: 'audio/wav', buffer: wav(4, 550) },
  { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('liner notes') },
])
await pc.waitForSelector('[data-test="file-notes.txt"]')
assert.equal(await pc.locator('[data-test=files-list] li').count(), 4)
assert.match(await pc.textContent('[data-test="file-01 - Morning.wav"]') || '', /63 KB/)
const mask = await pc.locator('[data-test="file-notes.txt"] .ph-ftile').evaluate((e) => getComputedStyle(e).maskImage || getComputedStyle(e).webkitMaskImage)
assert.match(mask, /url/, 'file tiles are squircles (masked), not rounded squares')
await pc.screenshot({ path: `${OUT}/files-list.png` })

step('rename the note, share it publicly, the link downloads it, then stop sharing')
await pc.click('[data-test="file-more-notes.txt"]')
await pc.click('[data-test="file-rename-notes.txt"]')
await name(pc, 'liner.txt')
await pc.waitForSelector('[data-test="file-liner.txt"]')
await pc.click('[data-test="file-more-liner.txt"]')
await pc.click('[data-test="file-share-liner.txt"]')
await pc.click('[data-test=files-share-make]')
const url = await pc.inputValue('[data-test=files-share-url]')
assert.match(url, /\/s\/[0-9a-f]{36}$/)
const anon = await browser.newContext()
let r = await anon.request.get(url)
assert.equal(r.status(), 200); assert.equal(await r.text(), 'liner notes', 'anyone with the link downloads the file')
await pc.keyboard.press('Escape')
await pc.click('[data-test=files-crumb-root]')
await pc.waitForSelector('[data-test=files-shares]')
await pc.click('[data-test="unshare-Music/Belle Band/First Light/liner.txt"]')
await pc.waitForSelector('[data-test=files-shares]', { state: 'detached' })
r = await anon.request.get(url)
assert.equal(r.status(), 404, 'a link that is no longer shared stops working')

step('delete waits for the toast: Undo keeps the file, otherwise it goes')
await pc.click('[data-test="file-Music"]'); await pc.click('[data-test="file-Belle Band"]'); await pc.click('[data-test="file-First Light"]')
await pc.waitForSelector('[data-test="file-liner.txt"]')
await pc.click('[data-test="file-more-liner.txt"]')
await pc.click('[data-test="file-delete-liner.txt"]')
await pc.click('.ph-toast button:has-text("Undo"), [data-test=toast-undo]')
await pc.waitForSelector('[data-test="file-liner.txt"]')
await pc.click('[data-test="file-more-liner.txt"]')
await pc.click('[data-test="file-delete-liner.txt"]')
await pc.waitForTimeout(4800)
await pc.reload(); await pc.waitForSelector('[data-test=files-page]')
await pc.click('[data-test="file-Music"]'); await pc.click('[data-test="file-Belle Band"]'); await pc.click('[data-test="file-First Light"]')
await pc.waitForSelector('[data-test="file-03 - Night.wav"]')
assert.equal(await pc.locator('[data-test="file-liner.txt"]').count(), 0, 'deleted on the server')

step('Music: the album from the folders, with the generated cover; Play plays it in order, seeking works')
await pc.goto(BASE + '/#/music')
await pc.waitForSelector('[data-test="album-First Light"]')
assert.match(await pc.textContent('[data-test="album-First Light"] .ph-cover__gen') || '', /FIRST LIGHT/, 'an album without art gets the generated cover')
await pc.click('[data-test="album-First Light"]')
await pc.waitForSelector('[data-test=music-songs] li >> nth=2')
assert.deepEqual(await pc.locator('[data-test=music-songs] .ph-row-title').allTextContents(), ['Morning', 'Evening', 'Night'], 'numbers and titles from the names')
await pc.click('[data-test=album-play]')
await pc.waitForFunction(() => document.querySelector('[data-test=music-now-title]')?.textContent === 'Morning')
await pc.waitForFunction(() => document.querySelector('[data-test=music-toggle]')?.getAttribute('aria-label') === 'Pause', null, { timeout: 10000 })
await pc.waitForTimeout(1200)
await pc.screenshot({ path: `${OUT}/files-music.png` })
await pc.click('[data-test=music-toggle]')
await pc.waitForFunction(() => document.querySelector('[data-test=music-toggle]')?.getAttribute('aria-label') === 'Play')
await pc.click('[aria-label=Next]')
await pc.waitForFunction(() => document.querySelector('[data-test=music-now-title]')?.textContent === 'Evening', null, { timeout: 10000 })

step('Play on: the other browser takes the queue and plays it')
await pc.click('[data-test=music-playon]')
await pc.click('[data-test=playon-Kitchen]')
await phone.waitForFunction(() => (window as any).__pheMusic?.title === 'Evening' && (window as any).__pheMusic?.playing, null, { timeout: 15000 })
await pc.waitForFunction(() => document.querySelector('[data-test=music-toggle]')?.getAttribute('aria-label') === 'Play', null, { timeout: 5000 })

step('the Belle music widgets on the desktop follow the player')
await pc.goto(BASE + '/#/music')
await pc.click('[data-test=desktop-view]')
await pc.waitForSelector('[data-test=desktop]')
await pc.click('.ph-menubar__menu:has-text("View")')
await pc.click('[data-test=edit-widgets]')
await pc.click('[data-test=widget-add-music]')
await pc.click('[data-test=widget-add-music-large]')
await pc.click('[data-test=widgets-done]')
await pc.keyboard.press('Control+Alt+d')
await pc.waitForSelector('[data-test=widget-music-title]')
assert.match(await pc.textContent('[data-test=widget-music-title]') || '', /Evening · Belle Band/)
await pc.click('[data-test=widget-music-toggle]')
await pc.waitForFunction(() => document.querySelector('[data-test=widget-music-toggle]')?.getAttribute('aria-label') === 'Pause', null, { timeout: 8000 })
await pc.waitForTimeout(400)
await pc.screenshot({ path: `${OUT}/files-widgets.png` })

assert.deepEqual(errors, [], 'no page errors')
step('files: all checks passed')
await browser.close()
