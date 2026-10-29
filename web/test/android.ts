// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// Browser test of the Android stream page: decoded video on the canvas, touch and keys going back, latency numbers.
import { chromium } from 'playwright'
import fs from 'fs'
import assert from 'node:assert/strict'

const BASE = process.env.BASE, TICKET = process.env.TICKET, OUT = process.env.OUT || '/out'
assert(BASE && TICKET, 'BASE and TICKET (a bridge ticket) are required')
// hooks: the Playwright container cannot reach adb, so android.sh runs a small loop that executes "hook-<name>.req" files from OUT with adb and answers "hook-<name>.out/.done"
async function hook(name: string) {
  for (const x of ['.done', '.out']) try { fs.unlinkSync(`${OUT}/hook-${name}${x}`) } catch (e) {}
  fs.writeFileSync(`${OUT}/hook-${name}.req`, '')
  for (let i = 0; i < 200; i++) { if (fs.existsSync(`${OUT}/hook-${name}.done`)) return fs.readFileSync(`${OUT}/hook-${name}.out`, 'utf8'); await new Promise((r2) => setTimeout(r2, 250)) }
  throw new Error('hook ' + name + ' timed out')
}
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 700, height: 900 }, ignoreHTTPSErrors: true })
const msgs = []
page.on('console', (m) => msgs.push(m.text()))
const sup = await page.goto(BASE + '/?t=' + TICKET + (process.env.QUERY || '') + '&kind=phone').then(() => page.evaluate(async () => {
  if (!('VideoDecoder' in window)) return 'no VideoDecoder'
  const r: Record<string, boolean | undefined> = {}
  for (const c of ['avc1.42e01f', 'avc1.640028', 'vp8', 'vp09.00.10.08']) r[c] = (await VideoDecoder.isConfigSupported({ codec: c })).supported
  return JSON.stringify(r)
}))
console.log('codec support in this Chromium:', sup)

// H.264 probe without a decoder (this Chromium has none): the bridge must announce a codec string and send key frames that start with the parameter sets
// what the bridge sent first: its codec announcement, and the first key frame's flag byte, first bytes and size
interface Probe { cfg: { codec: string } | null; first: { flag: number; head: string; size: number } | null; frames: number }
const probe = await page.evaluate((t) => new Promise<Probe>((resolve) => {
  const w = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + location.pathname.replace(/[^/]*$/, '') + 'ws?t=' + encodeURIComponent(t) + '&codec=h264'); w.binaryType = 'arraybuffer'
  const r: Probe = { cfg: null, first: null, frames: 0 }
  const done = setTimeout(() => { w.close(); resolve(r) }, 12000)
  w.onmessage = (e) => {
    if (typeof e.data === 'string') { const m = JSON.parse(e.data); if (m.t === 'cfg') r.cfg = m; return }
    const u = new Uint8Array(e.data); r.frames++
    if (!r.first && (u[0] & 2)) r.first = { flag: u[0], head: Array.from(u.subarray(1, 8)).map((x) => x.toString(16).padStart(2, '0')).join(' '), size: u.length }
    if (r.first && r.frames >= 3) { clearTimeout(done); w.close(); resolve(r) }
  }
}), TICKET)
console.log('h264 probe:', JSON.stringify(probe))
if (!probe.cfg || !/^avc1\.[0-9a-f]{6}$/.test(probe.cfg.codec) || !probe.first || !probe.first.head.startsWith('00 00 00 01 67')) throw new Error('h264 stream not as expected')
await page.waitForTimeout(1500)
await page.goto(BASE + '/?t=' + TICKET + (process.env.QUERY || '') + '&kind=phone')

const canvasInfo = () => page.evaluate(() => {
  const c = (document.getElementById('screen') as HTMLCanvasElement), x = c.getContext('2d')!, d = x.getImageData(0, 0, c.width, c.height).data
  let sum = 0, n = 0, min = 255, max = 0
  for (let i = 0; i < d.length; i += 4 * 97) { const v = (d[i] + d[i + 1] + d[i + 2]) / 3; sum += v; n++; if (v < min) min = v; if (v > max) max = v }
  return { w: c.width, h: c.height, mean: sum / n, min, max, hash: Array.from(d.filter((_, i) => i % 4001 === 0)).join(',') }
})
const t0 = Date.now()
await page.waitForFunction(() => (document.getElementById('info')?.textContent ?? '').includes('fps'), null, { timeout: 60000 })
console.log('first stats after', Date.now() - t0, 'ms:', await page.textContent('#info'))
await page.click('button[data-k="3"]'); await page.waitForTimeout(1500) // start from the home screen (a previous run may have left the drawer open)
const a = await canvasInfo()
console.log('canvas', a.w + 'x' + a.h, 'mean', a.mean.toFixed(1), 'range', a.min.toFixed(0) + '..' + a.max.toFixed(0))
if (!(a.max - a.min > 30)) { await page.screenshot({ path: OUT + '/android-blank.png' }); throw new Error('the canvas looks blank (decoder not working?)') }
await page.screenshot({ path: OUT + '/android-1-home.png' })

// swipe up from the lower middle (open the app drawer), through pointer events on the canvas
const box = await page.locator('#screen').boundingBox()
assert(box, 'the screen is laid out')
const px = (fx: number, fy: number) => [box.x + box.width * fx, box.y + box.height * fy]
async function swipe(f1: [number, number], f2: [number, number], steps = 8) {
  const [x1, y1] = px(...f1), [x2, y2] = px(...f2)
  await page.mouse.move(x1, y1); await page.mouse.down()
  for (let i = 1; i <= steps; i++) { await page.mouse.move(x1 + (x2 - x1) * i / steps, y1 + (y2 - y1) * i / steps); await page.waitForTimeout(20) }
  await page.mouse.up()
}
await swipe([0.5, 0.9], [0.5, 0.25])
await page.waitForTimeout(1500)
const b = await canvasInfo()
console.log('after swipe: canvas changed =', a.hash !== b.hash)
if (a.hash === b.hash) throw new Error('touch input did not change the screen')
await page.screenshot({ path: OUT + '/android-2-drawer.png' })

// keys: Home button, then open Settings search by tapping, typing is checked through the screen change
await page.click('button[data-k="3"]')
await page.waitForTimeout(1500)
const c = await canvasInfo()
console.log('after Home: canvas changed =', c.hash !== b.hash)
if (c.hash === b.hash) throw new Error('the Home key did not change the screen')
await page.screenshot({ path: OUT + '/android-3-home-again.png' })

// latency/CPU sample over 10 s of interaction (taps on the clock area and scroll)
const samples: string[] = []
const poll = setInterval(() => page.textContent('#info').then((t) => samples.push(t ?? '')).catch(() => {}), 400)
for (let i = 0; i < 8; i++) { await swipe([0.5, 0.8], [0.5, 0.3], 6); await page.waitForTimeout(500); await swipe([0.5, 0.3], [0.5, 0.8], 6); await page.waitForTimeout(500) }
clearInterval(poll)
const nums = (re: RegExp) => samples.map((t) => (t.match(re) || [])[1]).filter(Boolean).map(Number).sort((x, y) => x - y)
const med = (a: number[]) => (a.length ? a[Math.floor(a.length / 2)] : NaN)
const i2f = nums(/next frame (\d+) ms/), fps = nums(/(\d+) fps/), kb = nums(/(\d+) kbit/)
console.log('interaction: input-to-next-frame ms median', med(i2f), 'max', i2f[i2f.length - 1], '| fps median', med(fps), 'max', fps[fps.length - 1], '| kbit/s median', med(kb), 'max', kb[kb.length - 1])
await page.waitForTimeout(2500)
const last = await page.textContent('#info')
console.log('during interaction:', last)
// keyboard: focus the canvas and type (goes to the focused field, if any); only checks that sending does not break the stream
await page.locator('#screen').focus(); await page.keyboard.type('hello'); await page.keyboard.press('Escape')
await page.waitForTimeout(800)
console.log('still connected:', ((await page.textContent('#info')) ?? '').startsWith('connected'))
fs.writeFileSync(OUT + '/android-last-info.txt', last + '\n')

// ---- round 30 ----
const changed = async (fn: () => Promise<unknown>, ms = 1500) => { const h0 = (await canvasInfo()).hash; await fn(); await page.waitForTimeout(ms); return (await canvasInfo()).hash !== h0 }
const results: Record<string, unknown> = {}

// scroll wheel: Settings is a long list
await hook('applist'); await page.waitForTimeout(2500) // a long list (all apps), so there is something to scroll
const wb = await page.locator('#screen').boundingBox()
assert(wb, 'the screen is laid out')
await page.mouse.move(wb.x + wb.width / 2, wb.y + wb.height / 2)
results.wheel = await changed(async () => { for (let i = 0; i < 4; i++) { await page.mouse.wheel(0, 300); await page.waitForTimeout(150) } })
console.log('wheel scroll changed the screen:', results.wheel)

// rotation: the stream follows the device (the canvas becomes landscape, then portrait again)
await hook('rot1'); await page.waitForTimeout(3500)
const land = await canvasInfo()
console.log('after rotating the device: canvas', land.w + 'x' + land.h)
results.rotate = land.w > land.h
await page.screenshot({ path: OUT + '/android-4-landscape.png' })
// a tap in landscape still lands where it should: open the first Settings entry area and check the screen changes
results.landscapeTouch = await changed(async () => { const [x, y] = px(0.5, 0.2); await page.mouse.click(x, y) }) // the first rows of the app list
await hook('rot0'); await page.waitForTimeout(3500)
const port = await canvasInfo()
results.rotateBack = port.h > port.w
console.log('rotated back: canvas', port.w + 'x' + port.h)
await hook('home'); await page.waitForTimeout(1200)

// clipboard, browser -> phone: paste into a text field (Settings search), read it back from the UI hierarchy
await swipe([0.5, 0.9], [0.5, 0.25]); await page.waitForTimeout(1500) // the launcher's app drawer has a search field
let [sx, sy] = px(0.5, 0.115)
await page.mouse.click(sx, sy); await page.waitForTimeout(1500)
await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
await page.evaluate(() => navigator.clipboard.writeText('pasted from the browser 42'))
await page.screenshot({ path: OUT + '/android-5a-search.png' })
await page.click('#pasteto'); await page.waitForTimeout(2000)
await page.screenshot({ path: OUT + '/android-5b-pasted.png' })
const ui = await hook('dump')
console.log('ui edit fields:', (ui.match(/<node[^>]*EditText[^>]*>/g) || []).map((x) => (x.match(/text="[^"]*"/) || [''])[0]).join(' | '), '| note:', await page.textContent('#msg'))
results.clipPaste = ui.includes('pasted from the browser 42')
console.log('paste to phone reached the text field:', results.clipPaste)
// phone -> browser: select all in the field and copy through the page (Ctrl+C asks the phone for its clipboard)
await hook('selectall'); await page.waitForTimeout(500)
await page.evaluate(() => { window.__clip = ''; return navigator.clipboard.writeText('overwritten') })
await page.locator('#screen').focus(); await page.keyboard.press('Control+c'); await page.waitForTimeout(2000)
const back = await page.evaluate(() => window.__clip || '')
results.clipCopy = back.includes('pasted from the browser 42')
console.log('phone clipboard arrived in the browser:', results.clipCopy, JSON.stringify(back.slice(0, 40)))
await hook('home'); await page.waitForTimeout(800)

// APK install: upload a file, and an F-Droid package by name
await page.setInputFiles('#apk', process.env.APK || '/t/markor.apk')
await page.waitForFunction(() => /installed|failed/.test((document.getElementById('msg')?.textContent ?? '')), null, { timeout: 120000 })
console.log('upload:', await page.textContent('#msg'))
results.apkUpload = ((await page.textContent('#msg')) ?? '').includes('installed.')
await page.fill('#pkg', 'org.fossify.notes'); await page.click('#fd')
await page.waitForFunction(() => /org.fossify.notes from F-Droid: (installed|failed)/.test((document.getElementById('msg')?.textContent ?? '')), null, { timeout: 180000 })
console.log('f-droid:', await page.textContent('#msg'))
results.fdroid = ((await page.textContent('#msg')) ?? '').includes('installed.')
const pk = await hook('pkgs')
results.pkgsListed = pk.includes('net.gsantner.markor') && pk.includes('org.fossify.notes')
await page.fill('#pkg', 'not a package'); await page.click('#fd'); await page.waitForTimeout(800)
console.log('bad name:', await page.textContent('#msg'))

// audio: switch it on, play a tone on the device, see decoded frames (only if the device can capture audio)
await page.click('#sound'); await page.waitForTimeout(500)
await hook('tone'); await page.waitForTimeout(4000)
const af = await page.evaluate(() => window.__audioFrames || 0)
console.log('audio message from the bridge:', await page.evaluate(() => window.__audioMsg), '| audio frames decoded in the browser:', af, '| page says:', await page.textContent('#msg'))
results.audio = af > 0
console.log('RESULTS', JSON.stringify(results))
const failed = Object.entries(results).filter(([, v]) => !v).map(([k]) => k)
if (failed.length) throw new Error('failed: ' + failed.join(', '))
await browser.close()
console.log('OK')
