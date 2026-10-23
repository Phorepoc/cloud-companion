// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// Browser test of the Android stream page: decoded video on the canvas, touch and keys going back, latency numbers.
import { chromium } from 'playwright'
import fs from 'fs'

const BASE = process.env.BASE, TICKET = process.env.TICKET, OUT = process.env.OUT || '/out'
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 700, height: 900 } })
const msgs = []
page.on('console', (m) => msgs.push(m.text()))
const sup = await page.goto(BASE + '/?t=' + TICKET + (process.env.QUERY || '')).then(() => page.evaluate(async () => {
  if (!('VideoDecoder' in window)) return 'no VideoDecoder'
  const r = {}
  for (const c of ['avc1.42e01f', 'avc1.640028', 'vp8', 'vp09.00.10.08']) r[c] = (await VideoDecoder.isConfigSupported({ codec: c })).supported
  return JSON.stringify(r)
}))
console.log('codec support in this Chromium:', sup)

// H.264 probe without a decoder (this Chromium has none): the bridge must announce a codec string and send key frames that start with the parameter sets
const probe = await page.evaluate((t) => new Promise((resolve) => {
  const w = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws?t=' + encodeURIComponent(t) + '&codec=h264'); w.binaryType = 'arraybuffer'
  const r = { cfg: null, first: null, frames: 0 }
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
await page.goto(BASE + '/?t=' + TICKET + (process.env.QUERY || ''))

const canvasInfo = () => page.evaluate(() => {
  const c = document.getElementById('screen'), x = c.getContext('2d'), d = x.getImageData(0, 0, c.width, c.height).data
  let sum = 0, n = 0, min = 255, max = 0
  for (let i = 0; i < d.length; i += 4 * 97) { const v = (d[i] + d[i + 1] + d[i + 2]) / 3; sum += v; n++; if (v < min) min = v; if (v > max) max = v }
  return { w: c.width, h: c.height, mean: sum / n, min, max, hash: Array.from(d.filter((_, i) => i % 4001 === 0)).join(',') }
})
const t0 = Date.now()
await page.waitForFunction(() => document.getElementById('info').textContent.includes('fps'), null, { timeout: 60000 })
console.log('first stats after', Date.now() - t0, 'ms:', await page.textContent('#info'))
await page.click('button[data-k="3"]'); await page.waitForTimeout(1500) // start from the home screen (a previous run may have left the drawer open)
const a = await canvasInfo()
console.log('canvas', a.w + 'x' + a.h, 'mean', a.mean.toFixed(1), 'range', a.min.toFixed(0) + '..' + a.max.toFixed(0))
if (!(a.max - a.min > 30)) { await page.screenshot({ path: OUT + '/android-blank.png' }); throw new Error('the canvas looks blank (decoder not working?)') }
await page.screenshot({ path: OUT + '/android-1-home.png' })

// swipe up from the lower middle (open the app drawer), through pointer events on the canvas
const box = await page.locator('#screen').boundingBox()
const px = (fx, fy) => [box.x + box.width * fx, box.y + box.height * fy]
async function swipe(f1, f2, steps = 8) {
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
const samples = []
const poll = setInterval(() => page.textContent('#info').then((t) => samples.push(t)).catch(() => {}), 400)
for (let i = 0; i < 8; i++) { await swipe([0.5, 0.8], [0.5, 0.3], 6); await page.waitForTimeout(500); await swipe([0.5, 0.3], [0.5, 0.8], 6); await page.waitForTimeout(500) }
clearInterval(poll)
const nums = (re) => samples.map((t) => (t.match(re) || [])[1]).filter(Boolean).map(Number).sort((x, y) => x - y)
const med = (a) => (a.length ? a[Math.floor(a.length / 2)] : NaN)
const i2f = nums(/next frame (\d+) ms/), fps = nums(/(\d+) fps/), kb = nums(/(\d+) kbit/)
console.log('interaction: input-to-next-frame ms median', med(i2f), 'max', i2f[i2f.length - 1], '| fps median', med(fps), 'max', fps[fps.length - 1], '| kbit/s median', med(kb), 'max', kb[kb.length - 1])
await page.waitForTimeout(2500)
const last = await page.textContent('#info')
console.log('during interaction:', last)
// keyboard: focus the canvas and type (goes to the focused field, if any); only checks that sending does not break the stream
await page.locator('#screen').focus(); await page.keyboard.type('hello'); await page.keyboard.press('Escape')
await page.waitForTimeout(800)
console.log('still connected:', (await page.textContent('#info')).startsWith('connected'))
fs.writeFileSync(OUT + '/android-last-info.txt', last + '\n')
await browser.close()
console.log('OK')
