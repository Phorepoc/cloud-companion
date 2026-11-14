// SPDX-License-Identifier: AGPL-3.0-only
// What a person does on the Browser page, against real Firefox, with the web app's real CSP.
import { chromium } from 'playwright'
import assert from 'node:assert/strict'
const { BASE, CODE, CODE2, OUT } = process.env as Record<string, string>
const SITE = 'http://site/'
const b = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const ctx = await b.newContext({ viewport: { width: 1280, height: 860 }, permissions: ['clipboard-read', 'clipboard-write'] })
const p = await ctx.newPage()
const errs: string[] = []
p.on('pageerror', (e) => errs.push(e.message))
p.on('console', (m) => { if (m.type() === 'error') errs.push(m.text()) })
const step = (s: string) => console.log('ok -', s)
const addr = '.x-bf__row input'
const until = async (f: () => Promise<boolean>, what: string, ms = 15000) => { const t = Date.now(); for (;;) { if (await f()) return; assert(Date.now() - t < ms, 'timed out: ' + what); await p.waitForTimeout(50) } }
const live = () => until(() => p.evaluate(() => { const v = document.querySelector('[data-test=screen] video') as HTMLVideoElement | null; return !!v && v.videoWidth > 0 && v.currentTime > 0 }), 'live video', 30000)
const value = () => p.inputValue(addr)
const title = () => p.locator('[data-test=title]').textContent()
const tabCount = () => p.evaluate(() => (window as any).__pheBrowser.tabs.length as number)
const more = async (t: string) => { await p.click(".x-bf__row [aria-label^='More']"); await p.click(`[data-test=${t}]`) }
// the video element fills the stage, the picture is centred in it
const at = async (x: number, y: number) => { const v = await p.locator('[data-test=screen] video').boundingBox(); assert(v); const s = await p.evaluate(() => { const e = document.querySelector('[data-test=screen] video') as HTMLVideoElement; return [e.videoWidth, e.videoHeight] }); const k = Math.min(v.width / s[0], v.height / s[1]); return [v.x + (v.width - s[0] * k) / 2 + x * k, v.y + (v.height - s[1] * k) / 2 + y * k] as const }
const go = async (u: string) => { await p.fill(addr, u); await p.press(addr, 'Enter') }
try {
  await p.goto(BASE + '/#/pair'); await p.fill('[data-test=code]', CODE); await p.click('[data-test=pair]')
  await p.waitForSelector('[data-test=browser-frame]', { timeout: 30000 })
  const trace = () => p.evaluate(() => { const t0 = Date.now(); let last = ''; setInterval(() => { const v = document.querySelector('[data-test=screen] video') as HTMLVideoElement | null; const s = document.querySelector('[data-test=browser-connecting]') ? 'overlay' : 'clear'; const n = s + ' ' + (v ? v.videoWidth + 'x' + v.videoHeight + ' ' + (v.currentTime > 0) + ' ' + v.readyState : 'novideo'); if (n !== last) { console.log('STATE', Date.now() - t0, n); last = n } }, 100) })
  p.on('console', (m) => { if (m.text().startsWith('STATE')) console.log(m.text()) })
  await p.goto(BASE + '/#/browse'); await trace(); await live(); step('the picture is live in the real page')
  await go(SITE + 'index.html')
  await until(async () => (await title()) === 'probe one', 'title'); step('address bar loads a page, title follows')
  await p.waitForTimeout(800)
  await p.keyboard.type('typed through our page', { delay: 30 }); await p.waitForTimeout(800)
  await p.screenshot({ path: OUT + '/browser-typed.png' }); step('keys go to the page straight after the address bar (see the screenshot)')
  await p.mouse.move(...await at(60, 130)); await p.waitForTimeout(300); await p.mouse.down(); await p.mouse.up()
  await until(async () => (await value()).includes('two.html'), 'link updates address'); step('a link in the stream updates the address bar')
  await p.click(".x-bf__row [aria-label='Back']"); await until(async () => (await title()) === 'probe one', 'back'); step('Back')
  await p.click(".x-bf__row [aria-label='Forward']"); await until(async () => (await title()) === 'probe two', 'forward'); step('Forward')
  await p.reload(); await trace(); await live(); await until(async () => (await title()) === 'probe two', 'tab back after reload'); step('reloading our page reconnects and restores the tab')
  assert.deepEqual(errs.filter((e) => /Content Security Policy/.test(e)), [], 'CSP'); step('no CSP errors')
  const n = await tabCount()
  for (let i = 0; i < 5; i++) { await more('tab-new'); await p.waitForTimeout(150) }
  await until(async () => (await tabCount()) === n + 5, 'five new tabs, more than the old limit'); step('five quick new tabs, past the old limit of three')
  await more('other-devices'); await p.waitForSelector('[data-test=others-dialog]')
  for (let i = 0; i < 5; i++) { await p.locator('[data-test=others-dialog] [aria-label^=Close]').last().click(); await p.waitForTimeout(150) }
  await until(async () => (await tabCount()) === n, 'closing them'); await p.keyboard.press('Escape'); step('closing them from Tabs on your devices')
  await p.waitForTimeout(500); await p.click(".x-bf__row [aria-label^='More']"); await p.click('[data-test=zoom-in]')
  await until(async () => (await p.textContent('[data-test=zoom-pct]')) === '125%', 'zoom'); await p.click('[data-test=zoom-out]'); await p.keyboard.press('Escape'); step('Zoom')
  // Desktop site off: the phone version of sites, in the request and to the page's scripts; on again: the desktop one
  await go(SITE + 'ua'); await until(async () => (await title()) === 'header-desktop script-desktop', 'desktop ua')
  await more('desktop-site'); await until(async () => (await title()) === 'header-mobile script-mobile', 'mobile ua')
  await more('desktop-site'); await until(async () => (await title()) === 'header-desktop script-desktop', 'desktop ua again'); step('Desktop site switches the tab between the phone and desktop versions')
  // Send to device: a second device (its own browser context) is asked, and opens the page in the Browser
  const ctx2 = await b.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true }); const q = await ctx2.newPage()
  await q.goto(BASE + '/#/pair'); await q.fill('[data-test=code]', CODE2); await q.click('[data-test=pair]'); await q.waitForURL(/#\/(?!pair)/, { timeout: 30000 })
  await go(SITE + 'two.html'); await until(async () => (await title()) === 'probe two', 'two')
  await more('send-device'); await p.locator('[data-test=send-dialog] [data-test^=send-btn-]').first().click({ timeout: 10000 })
  await q.waitForSelector('[data-test=sent-dialog]', { timeout: 10000 }); assert.match(await q.textContent('[data-test=sent-dialog]') || '', /two\.html|probe two/)
  const before = await tabCount()
  await q.click('[data-test=sent-open]'); await q.waitForURL(/#\/browse/)
  await until(async () => (await tabCount()) === before + 1, 'the sent page opens in a new tab'); step('Send to device: the other device asks, and opens it in the Browser')
  // that device is a phone: the picture is portrait and fills its page closely, and a tap on a link follows it
  const qv = () => q.evaluate(() => { const v = document.querySelector('[data-test=screen] video') as HTMLVideoElement | null; const s = document.querySelector('[data-test=stage]')!.getBoundingClientRect(); return v && v.videoWidth ? { w: v.videoWidth, h: v.videoHeight, sw: Math.floor(s.width), sh: Math.floor(s.height) } : null })
  await until(async () => { const v = await qv(); return !!v && v.h > v.w && v.w <= v.sw && v.h <= v.sh && v.sw - v.w < 40 && v.sh - v.h < 60 }, 'phone-sized picture', 20000)
  console.log('phone picture', JSON.stringify(await qv()))
  await q.waitForSelector('[data-test=sent-dialog]', { state: 'detached' }); await q.waitForTimeout(500)
  await q.screenshot({ path: OUT + '/browser-phone.png' }); step('on a phone the picture is portrait and fits the page (see the screenshot)')
  await q.fill(addr, SITE + 'index.html'); await q.press(addr, 'Enter')
  await until(async () => (await q.locator('[data-test=title]').textContent()) === 'probe one', 'phone: page one', 15000); await q.waitForTimeout(800)
  { const v = await q.locator('[data-test=screen] video').boundingBox(); const s = await qv(); assert(v && s); const k = Math.min(v.width / s.w, v.height / s.h)
    await q.touchscreen.tap(v.x + (v.width - s.w * k) / 2 + 60 * k, v.y + (v.height - s.h * k) / 2 + 130 * k) }
  await until(async () => (await q.inputValue(addr)).includes('two.html'), 'phone: a tap follows the link'); step('on a phone a tap follows a link')
  await ctx2.close()
  // the shared screen is the phone's now: Firefox's window follows it (it has a minimum width of its own, lifted)
  await go(SITE + 'size.html'); await until(async () => /^size (\d+)x\d+ screen \1x/.test(await title() || ''), 'Firefox as narrow as the phone'); step('Firefox\'s window is as narrow as the phone\'s screen')
  // using this device again takes the screen back to its own size
  await p.mouse.move(...await at(10, 300)); await p.mouse.down(); await p.mouse.up()
  await until(() => p.evaluate(() => (document.querySelector('[data-test=screen] video') as HTMLVideoElement).videoWidth >= 1000), 'desktop size again'); step('using the desktop again gives it its own screen size back')
  // the clipboard both ways: this device's text pastes into the page, and what is copied there comes back to this device
  await go(SITE + 'clip.html'); await until(async () => (await title()) === 'clip', 'clip page'); await p.waitForTimeout(800)
  await p.evaluate(() => navigator.clipboard.writeText('from the device'))
  await p.mouse.move(...await at(150, 100)); await p.mouse.down(); await p.mouse.up(); await p.waitForTimeout(300)
  // headless Chromium does not turn Ctrl+V into a paste event, as a browser on a device does: the event is made here
  await p.keyboard.down('Control')
  await p.evaluate(() => { const d = new DataTransfer(); d.setData('text/plain', 'from the device'); document.activeElement!.dispatchEvent(new ClipboardEvent('paste', { clipboardData: d, bubbles: true, cancelable: true })) })
  await p.keyboard.up('Control')
  await until(async () => (await title()) === 'clip:from the device', 'paste from the device'); step('paste: this device\'s clipboard goes into the page')
  await p.keyboard.type(' and back', { delay: 30 }); await p.keyboard.press('Control+A'); await p.keyboard.press('Control+C')
  await until(() => p.evaluate(() => navigator.clipboard.readText().then((t) => t === 'from the device and back')), 'copy back to the device'); step('copy: text copied in the page reaches this device\'s clipboard')
  // a refused certificate: the address is https, but the bar does not show it as secure
  const notSecure = () => p.locator(".x-bf__row [aria-label='Not secure']").count().then((n) => n > 0)
  await go('https://site-bad/two.html')
  await until(() => p.evaluate(() => (window as any).__pheBrowser.tabs.find((t: any) => t.active)?.security === 'broken'), 'certificate refused')
  await until(notSecure, 'the bar shows Not secure'); assert(await notSecure(), 'a refused certificate shows Not secure'); await p.waitForTimeout(2000); await p.screenshot({ path: OUT + '/browser-cert.png' }); step('a refused certificate shows Not secure, not a lock')
  // the star bookmarks the page; Bookmarks and history lists it, with the pages visited, and removes it
  const starred = () => p.evaluate(() => !!(window as any).__pheBrowser.tabs.find((t: any) => t.active)?.starred)
  await go(SITE + 'two.html'); await until(async () => (await title()) === 'probe two', 'two again')
  await p.click(".x-bf__row [aria-label='Bookmark']"); await until(starred, 'starred')
  assert.equal(await p.getAttribute(".x-bf__row [aria-label='Bookmark']", 'aria-pressed'), 'true'); step('the star bookmarks the page and shows it')
  await more('saved-open'); await p.waitForSelector('[data-test=saved-dialog]')
  await p.locator('[data-test=saved-dialog]').getByText('probe two').first().waitFor({ timeout: 5000 })
  await p.click('[data-test=saved-history]'); await p.locator('[data-test=saved-dialog]').getByText('probe one').first().waitFor({ timeout: 5000 })
  await p.click('[data-test=saved-marks]'); await p.locator('[data-test=saved-dialog] [aria-label^=Remove]').first().click()
  await until(async () => !(await starred()), 'unstarred'); await p.keyboard.press('Escape'); step('Bookmarks and history: the bookmark and the visited pages, and removing it')
  await p.waitForTimeout(400)
  // passkeys: a site's request reaches this device; with no passkey provider here the site hears no, with one it gets the answer
  await go('https://site/passkey.html')
  await until(async () => (await title()) === 'passkey', 'passkey page'); await p.waitForTimeout(800)
  const press = async () => { await p.mouse.move(...await at(150, 100)); await p.mouse.down(); await p.mouse.up() }
  await press()
  await p.waitForSelector('[data-test=passkey-none]', { timeout: 10000 }); await p.click('[data-test=passkey-cancel]')
  await until(async () => (await title()) === 'err NotAllowedError', 'no provider: not allowed'); step('passkey with no provider on this device: the site is told no, and can offer another way')
  await p.evaluate(() => { const b = (s: string) => btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    ;(window as any).phePasskeys = { request: async (r: any) => { (window as any).__pkAsked = r
      return { id: 'Y3JlZA', rawId: 'Y3JlZA', type: 'public-key', response: { clientDataJSON: b(JSON.stringify({ type: 'webauthn.get', origin: r.origin, challenge: r.options.challenge })), authenticatorData: b('a'.repeat(37)), signature: b('s'.repeat(70)), userHandle: null } } } } })
  await p.waitForSelector('[data-test=passkey-dialog]', { state: 'detached' }); await p.waitForTimeout(300)
  await press(); await p.waitForSelector('[data-test=passkey-go]', { timeout: 10000 }); await p.click('[data-test=passkey-go]')
  await until(async () => (await title()) === 'got Y3JlZA public-key https://site sig=70', 'the site got the credential')
  const asked = await p.evaluate(() => (window as any).__pkAsked)
  assert.equal(asked.origin, 'https://site'); assert.equal(asked.kind, 'get'); assert.equal(asked.options.rpId, 'site'); assert.equal(Buffer.from(asked.options.challenge, 'base64url').toString(), 'challenge-123')
  step('passkey with this device\'s provider: it gets the site, the origin and the challenge, and the site gets the credential')
  // a page's file input asks this device for the file, and the page gets it
  await go(SITE + 'upload.html'); await until(async () => (await title()) === 'upload', 'upload page'); await p.waitForTimeout(800)
  await p.mouse.move(...await at(150, 100)); await p.mouse.down(); await p.mouse.up()
  await p.waitForSelector('[data-test=pick-dialog]', { timeout: 10000 })
  await p.setInputFiles('[data-test=pick-input]', { name: 'hello.txt', mimeType: 'text/plain', buffer: Buffer.from('hello from the device') })
  await until(async () => (await title()) === 'got hello.txt: hello from the device', 'the page got the file'); step('a page asks for a file: this device picks it, the page gets it')
  // a download lands in Files, Downloads, and the page says so with a way there
  await go(SITE + 'phe-download.txt')
  await until(async () => /Saved in Files, Downloads: phe-download\.txt/.test(await p.textContent('[data-test=browser-note]').catch(() => '') || ''), 'download saved')
  await p.click('[data-test=download-show]'); await p.waitForURL(/#\/files/)
  await p.getByText('phe-download.txt').first().waitFor({ timeout: 10000 }); step('a download is saved in Files, Downloads, and Show in Files opens it')
  // Settings, Browser: the number of tabs kept loaded follows the memory; set to 2, the others unload and come back when picked
  const loaded = () => p.evaluate(() => (window as any).__pheBrowser.tabs.filter((t: any) => !t.asleep).length as number)
  await p.goto(BASE + '/#/settings/webbrowser'); await p.waitForSelector('[data-test=bset-memory]')
  await until(async () => /GB|MB/.test(await p.textContent('[data-test=bset-memory]') || ''), 'memory shown')
  assert.match(await p.textContent('[data-test=bset-auto]') || '', /\d+ tabs loaded at most/); step('Settings shows the memory and the automatic number')
  await p.click('[data-test=bset-auto-switch]'); await p.fill('[data-test=bset-number] input', '2'); await p.click('[data-test=bset-save]')
  await until(async () => /loaded/.test(await p.textContent('[data-test=bset-open]') || ''), 'saved')
  await p.goto(BASE + '/#/browse'); await live()
  for (const u of ['index.html', 'two.html', 'ua']) { await more('tab-new'); await p.waitForTimeout(400); await go(SITE + u); await p.waitForTimeout(1200) }
  await until(async () => (await loaded()) <= 2, 'at most two loaded'); assert((await tabCount()) >= 4, 'no tab was closed'); step('above the number, background tabs unload and stay open')
  const sleeper = await p.evaluate(() => (window as any).__pheBrowser.tabs.find((t: any) => t.asleep)?.id as number)
  await p.locator(`.x-bf [data-tab-id="${sleeper}"], .x-bf [data-id="${sleeper}"]`).first().click({ timeout: 3000 }).catch(async () => { await more('other-devices'); await p.locator(`[data-test=fx-tab-${sleeper}] [aria-label='Open here']`).click() })
  await until(() => p.evaluate((id) => !(window as any).__pheBrowser.tabs.find((t: any) => t.id === id)?.asleep, sleeper), 'picked tab loads again'); step('an unloaded tab loads again when picked')
  await p.goto(BASE + '/#/settings/webbrowser'); await p.waitForSelector('[data-test=bset-auto-switch]'); await p.click('[data-test=bset-auto-switch]')
  await until(async () => /from the memory/.test(await p.textContent('[data-test=bset-auto]') || ''), 'auto again'); step('back to automatic')
  await p.goto(BASE + '/#/browse'); await live()
  await p.screenshot({ path: OUT + '/browser-end.png' })
  console.log('BROWSER PASS')
} catch (e) { await p.screenshot({ path: OUT + '/browser-fail.png' }).catch(() => {}); console.log(errs.slice(-8).join('\n').slice(0, 900)); throw e } finally { await b.close() }
