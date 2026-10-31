// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The desktop's weather (the Weather widget), from the server's /phe/weather (internal/webui/weather.go), whichever source the
// owner picked in Settings. The place is this device's own pick (kept here, like the widgets); without one the server's default
// place is used. Conditions are WMO weather codes, drawn with the Material weather symbols.
import { ref } from 'vue'
import { apiCall } from '../cloud'

export interface Place { name: string; detail?: string; lat: number; lon: number }
export interface Day { date: string; high: number; low: number; code: number }
export interface Forecast { place: Place; temperature: number; unit: string; code: number; description: string; day: boolean; days: Day[]; source: string }

const KEY = 'phecloud-weather-place'
export function myPlace(): Place | null {
  try { const p = JSON.parse(localStorage.getItem(KEY) || 'null'); return p && typeof p.lat === 'number' && typeof p.lon === 'number' ? p : null } catch { return null }
}
/** null: back to the server's default place */
export function setMyPlace(p: Place | null) {
  try { if (p) localStorage.setItem(KEY, JSON.stringify(p)); else localStorage.removeItem(KEY) } catch { /* private window: this session only */ }
}

export const forecast = ref<Forecast | null>(null)
export const noPlace = ref(false) // the server has no default place and this device picked none
export const weatherError = ref('')

export async function loadWeather() {
  const p = myPlace()
  const q = p ? `?lat=${p.lat}&lon=${p.lon}&name=${encodeURIComponent(p.name)}&detail=${encodeURIComponent(p.detail || '')}` : ''
  try {
    const j = await apiCall('GET', '/phe/weather' + q)
    forecast.value = j.forecast; noPlace.value = !j.place; weatherError.value = ''
  } catch (e) { weatherError.value = (e as Error).message }
}

export async function searchPlaces(q: string): Promise<Place[]> {
  const j = await apiCall('GET', '/phe/weather/places?q=' + encodeURIComponent(q))
  return j.places || []
}

/** A WMO code as a Material symbol (day or night). */
export function weatherIcon(code: number, day = true): string {
  if (code === 0 || code === 1) return day ? 'sym_o_sunny' : 'sym_o_clear_night'
  if (code === 2) return day ? 'sym_o_partly_cloudy_day' : 'sym_o_partly_cloudy_night'
  if (code === 3) return 'sym_o_cloud'
  if (code === 45 || code === 48) return 'sym_o_foggy'
  if (code >= 95) return 'sym_o_thunderstorm'
  if ((code >= 71 && code <= 77) || code === 85 || code === 86) return 'sym_o_weather_snowy'
  if (code === 66 || code === 67) return 'sym_o_weather_mix'
  if (code >= 51 && code <= 82) return 'sym_o_rainy'
  return 'sym_o_cloud'
}
