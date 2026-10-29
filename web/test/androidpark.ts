// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// Resource parking: after the last viewer leaves, the bridge stops the Android container (PHE_ANDROID_PARK_AFTER); the next viewer sees "Starting Android" and gets the screen.
// Measures RAM saved and the cold start to the first frame. Runs against the bridge's standalone page (BASE) with the hooks of android.sh.
import { chromium, type Page } from 'playwright'
import assert from 'node:assert/strict'
import fs from 'fs'

const BASE = process.env.BASE, TICKET = process.env.TICKET, OUT = process.env.OUT || '/out', QUERY = process.env.QUERY || ''
assert(BASE && TICKET, 'BASE and TICKET (a bridge ticket) are required')
async function hook(name: string) {
  for (const x of ['.done', '.out']) try { fs.unlinkSync(`${OUT}/hook-${name}${x}`) } catch (e) {}
  fs.writeFileSync(`${OUT}/hook-${name}.req`, '')
  for (let i = 0; i < 400; i++) { if (fs.existsSync(`${OUT}/hook-${name}.done`)) return fs.readFileSync(`${OUT}/hook-${name}.out`, 'utf8').trim(); await new Promise((r) => setTimeout(r, 250)) }
  throw new Error('hook ' + name + ' timed out')
}
const browser = await chromium.launch()
const live = (p: Page) => p.waitForFunction(() => (document.getElementById('info')?.textContent ?? '').includes('fps'), null, { timeout: 180000 })
const nonblank = (p: Page) => p.evaluate(() => { const c = (document.getElementById('screen') as HTMLCanvasElement), d = c.getContext('2d')!.getImageData(0, 0, c.width, c.height).data; let mn = 255, mx = 0; for (let i = 0; i < d.length; i += 388) { const v = (d[i] + d[i + 1] + d[i + 2]) / 3; if (v < mn) mn = v; if (v > mx) mx = v } return mx - mn > 30 })

let page = await browser.newPage()
await page.goto(BASE + '/?t=' + TICKET + QUERY)
await live(page)
await page.waitForTimeout(3000)
const before = { state: await hook('ctstate'), mem: await hook('ctmem'), host: await hook('hostmem') }
console.log('viewer connected: container', before.state, '| container memory', before.mem, '| host used MiB', before.host)
await page.close() // the last viewer leaves
const t0 = Date.now()
for (let i = 0; i < 400; i++) { if ((await hook('ctstate')) !== 'running') break; await new Promise((r) => setTimeout(r, 1000)) }
const parked = await hook('ctstate')
console.log('parked after', ((Date.now() - t0) / 1000).toFixed(0), 's without a viewer: container', parked)
assert.notEqual(parked, 'running', 'the container was not parked')
await new Promise((r) => setTimeout(r, 3000))
const after = { mem: await hook('ctmem'), host: await hook('hostmem') }
console.log('parked: container memory', after.mem, '| host used MiB', after.host, '| saved about', Number(before.host) - Number(after.host), 'MiB of host memory')

// the next viewer: the page says so while it starts, then shows the screen
page = await browser.newPage()
const t1 = Date.now()
const seen: string[] = []
const poll = setInterval(() => page.textContent('#info').then((t) => { if (t !== null && !seen.includes(t)) seen.push(t) }).catch(() => {}), 200)
await page.goto(BASE + '/?t=' + TICKET + QUERY)
await live(page)
clearInterval(poll)
const cold = (Date.now() - t1) / 1000
await page.waitForTimeout(1500)
console.log('cold start to the first frame:', cold.toFixed(1), 's; page said:', JSON.stringify(seen.slice(0, 3)))
assert(seen.some((t) => t.includes('Starting Android')), 'the page never said "Starting Android"')
assert(await nonblank(page), 'the screen is blank after the start')
const pk = await hook('pkgs')
console.log('apps kept across the stop:', pk.includes('net.gsantner.markor') ? 'yes' : 'NO')
assert(pk.includes('net.gsantner.markor'))
console.log('container memory running again:', await hook('ctmem'))
await browser.close()
console.log('OK')
