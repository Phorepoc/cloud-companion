// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Playwright test of the weather (internal/weather, /phe/weather), run against a live server whose network this test shares:
//   BASE=http://localhost:8080 CODE=ABCD-EFGH OUT=/out node --experimental-strip-types weather.ts
//
// A fake Open-Meteo on localhost:6197 (no internet needed) stands in for the source. Settings → Weather points the server at it and
// sets a default place; the desktop's Weather widget shows it; the place picker lists both Oulus (Finland and Wisconsin) and the
// pick is this browser's own (kept on reload). A wrong OpenWeatherMap key is refused before it is kept.
import { chromium } from 'playwright'
import http from 'node:http'
import assert from 'node:assert/strict'

const BASE = process.env.BASE || 'http://127.0.0.1:8080'
const CODE = process.env.CODE
const OUT = process.env.OUT || '.'
assert(CODE, 'CODE (a pairing code from `phecloud paircode --admin`) is required')

const asked: string[] = []
const up = http.createServer((req, res) => {
  asked.push(req.url || '')
  const u = new URL(req.url || '/', 'http://x')
  res.setHeader('Content-Type', 'application/json')
  if (u.pathname === '/v1/search') {
    const all = [{ name: 'Tampere', latitude: 61.5, longitude: 23.79, admin1: 'Pirkanmaa', country: 'Finland' },
      { name: 'Oulu', latitude: 65.01, longitude: 25.47, admin1: 'North Ostrobothnia', country: 'Finland' },
      { name: 'Oulu', latitude: 46.33, longitude: -91.41, admin1: 'Wisconsin', country: 'United States' }]
    res.end(JSON.stringify({ results: all.filter((p) => p.name.toLowerCase().startsWith((u.searchParams.get('name') || '').toLowerCase())) }))
  } else if (u.pathname === '/v1/forecast') {
    const cold = u.searchParams.get('latitude')?.startsWith('46') // Wisconsin
    res.end(JSON.stringify({ current: { temperature_2m: cold ? -3 : 12, weather_code: cold ? 73 : 2, is_day: 1 },
      daily: { time: ['2026-10-31', '2026-11-01', '2026-11-02'], temperature_2m_max: [14, 13, 15], temperature_2m_min: [6, 5, 7], weather_code: [2, 61, 0] } }))
  } else if (u.pathname === '/data/2.5/weather') {
    res.statusCode = 401; res.end(JSON.stringify({ message: 'Invalid API key' }))
  } else { res.statusCode = 404; res.end('{}') }
}).listen(6197, '127.0.0.1')

const browser = await chromium.launch()
const ctx = await browser.newContext({ bypassCSP: true, viewport: { width: 1440, height: 900 }, colorScheme: 'light', serviceWorkers: 'block' })
const page = await ctx.newPage()
const errors: string[] = []
page.on('pageerror', (e) => errors.push(e.message))
const step = (s: string) => console.log('· ' + s)

step('pair')
await page.goto(BASE + '/#/pair')
await page.fill('[data-test=code]', CODE)
await page.click('[data-test=pair]')
await page.waitForFunction(() => window.__phe && window.__phe.state.status === 'online', null, { timeout: 20000 })

step('Settings → Weather: Open-Meteo on our own address, default place Tampere')
await page.goto(BASE + '/#/settings')
await page.waitForSelector('[data-test=weather-provider]')
await page.click('text=Own server')
await page.fill('[data-test=weather-base]', 'http://127.0.0.1:6197')
await page.fill('input[aria-label="Place search address"]', 'http://127.0.0.1:6197')
await page.click('[data-test=weather-save]')
await page.waitForSelector('[data-test=weather-saved]')
await page.click('[data-test=weather-default-change]')
await page.fill('[data-test=weather-default-q]', 'Tamp')
await page.click('[data-test=weather-default-row]')
assert.match(await page.textContent('[data-test=weather-default]') || '', /Tampere, Pirkanmaa, Finland/)
await page.click('[data-test=weather-save]')
await page.waitForSelector('[data-test=weather-saved]')

step('a wrong OpenWeatherMap key is refused, the saved source stays')
await page.click('[data-test=weather-provider] >> text=OpenWeatherMap')
await page.fill('[data-test=weather-key]', 'not-a-key')
await page.fill('[data-test=weather-base]', 'http://127.0.0.1:6197')
await page.click('[data-test=weather-save]')
await page.waitForSelector('[data-test=weather-error]')
assert.match(await page.textContent('[data-test=weather-error]') || '', /did not answer/)
assert.doesNotMatch(await page.textContent('[data-test=weather-error]') || '', /not-a-key/)
await page.goto(BASE + '/#/') // away and back: the card reads the stored config again
await page.goto(BASE + '/#/settings')
await page.waitForSelector('[data-test=weather-provider]')
assert(await page.locator('[data-test=weather-provider] [aria-checked=true]:has-text("Open-Meteo")').count(), 'Open-Meteo is still the source')

step('desktop: add the Weather widget; it shows the default place')
await page.click('[data-test=desktop-view]')
await page.waitForSelector('[data-test=desktop]')
await page.keyboard.press('Control+Alt+d')
await page.click('.ph-menubar__menu:has-text("View")')
await page.click('[data-test=edit-widgets]')
await page.click('[data-test=widget-add-weather]')
await page.click('[data-test=widgets-done]')
await page.waitForFunction(() => /12°/.test(document.querySelector('[data-test=weather-temp]')?.textContent || ''), null, { timeout: 15000 })
assert.equal((await page.textContent('[data-test=weather-place-open]'))?.trim(), 'Tampere')
await page.locator('[data-test=widget-weather]').screenshot({ path: OUT + '/weather-widget.png' })

step('the place picker: both Oulus, with region and country; Wisconsin is this browser\'s pick')
await page.click('[data-test=weather-place-open]')
await page.fill('[data-test=weather-place-q]', 'Oulu')
await page.waitForFunction(() => document.querySelectorAll('[data-test=weather-place-row]').length === 2, null, { timeout: 10000 }).catch(async (e) => { await page.screenshot({ path: OUT + '/weather-fail.png' }); throw e })
await page.screenshot({ path: OUT + '/weather-picker.png' })
await page.click('[data-test=weather-place-row]:has-text("Wisconsin")')
await page.waitForFunction(() => /-3°/.test(document.querySelector('[data-test=weather-temp]')?.textContent || ''), null, { timeout: 15000 })
assert.equal((await page.textContent('[data-test=weather-place-open]'))?.trim(), 'Oulu')
assert(asked.some((u) => u.includes('latitude=46.33')), 'the server asked for Wisconsin\'s coordinates')
await page.locator('[data-test=widget-weather]').screenshot({ path: OUT + '/weather-widget-oulu.png' })
await page.reload()
await page.waitForFunction(() => /-3°/.test(document.querySelector('[data-test=weather-temp]')?.textContent || ''), null, { timeout: 15000 })

step('Default place goes back to Tampere')
await page.keyboard.press('Control+Alt+d') // the reload brought the Settings window back over the widgets
await page.click('[data-test=weather-place-open]')
await page.click('[data-test=weather-place-default]')
await page.waitForFunction(() => /12°/.test(document.querySelector('[data-test=weather-temp]')?.textContent || ''), null, { timeout: 15000 })

assert.deepEqual(errors, [], 'no page errors')
await browser.close(); up.close()
console.log('weather: all checks passed')
