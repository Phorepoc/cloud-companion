// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Theme choice, stored on this device only: "hybrid" (default, the N9 look), "light", "dark" or "system" (light or dark, following
// the operating system). Quasar's Dark plugin tracks dark; the Etesian tokens follow data-hd-mode on <html> (docs/15-colour-modes.md
// in etesian-design). Hybrid is the N9's mix: light content pages framed by dark chrome, menus and dialogs, and the media apps
// (Music, Photos, Android) dark all the way, as on the phone. The `hd-quasar` class on <body> switches on the Etesian Quasar theme.
import { Dark } from 'quasar'
import { ref, watch } from 'vue'

const KEY = 'phecloud-theme'
export type ThemeChoice = 'hybrid' | 'system' | 'light' | 'dark'
export const theme = ref<ThemeChoice>('hybrid')

// the sections the N9 drew inverted (black): its Music, Gallery and Camera/Video apps
const MEDIA = new Set(['music', 'photos', 'android'])

// pageMode is the colour mode of one section's page: dark for a media app in Hybrid, otherwise the page follows <html>
export function pageMode(id: string | undefined): 'dark' | undefined {
  return theme.value === 'hybrid' && id && MEDIA.has(id) ? 'dark' : undefined
}

function syncMode() {
  const strict = theme.value === 'light' || theme.value === 'system'
  document.documentElement.dataset.hdMode = Dark.isActive ? 'dark' : strict ? 'light' : 'hybrid'
  document.documentElement.style.colorScheme = Dark.isActive ? 'dark' : 'light'
}

export function applyTheme(choice: ThemeChoice) {
  theme.value = choice
  Dark.set(choice === 'dark' ? true : choice === 'system' ? 'auto' : false)
  syncMode()
  try { localStorage.setItem(KEY, choice) } catch (e) { /* private mode: the choice lasts for this visit */ }
}

export function initTheme() {
  document.body.classList.add('hd-quasar')
  watch(() => Dark.isActive, syncMode)
  let choice = 'hybrid'
  try { choice = localStorage.getItem(KEY) || 'hybrid' } catch (e) { /* ignore */ }
  applyTheme((['hybrid', 'system', 'light', 'dark'] as string[]).includes(choice) ? (choice as ThemeChoice) : 'hybrid')
}
