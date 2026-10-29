// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Theme choice: "system" (default, follows the operating system), "light" or "dark". Stored on this device only.
// Quasar's Dark plugin tracks the choice; the Etesian tokens follow it through data-hd-mode on <html>: "hybrid" (the N9 look: light
// pages, dark chrome) when light, "dark" when dark. The `hd-quasar` class on <body> switches on the Etesian Quasar theme.
import { Dark } from 'quasar'
import { ref, watch } from 'vue'

const KEY = 'phecloud-theme'
export type ThemeChoice = 'system' | 'light' | 'dark'
export const theme = ref<ThemeChoice>('system')

function syncMode() {
  document.documentElement.dataset.hdMode = Dark.isActive ? 'dark' : 'hybrid'
  document.documentElement.style.colorScheme = Dark.isActive ? 'dark' : 'light'
}

export function applyTheme(choice: ThemeChoice) {
  theme.value = choice
  Dark.set(choice === 'dark' ? true : choice === 'light' ? false : 'auto')
  syncMode()
  try { localStorage.setItem(KEY, choice) } catch (e) { /* private mode: the choice lasts for this visit */ }
}

export function initTheme() {
  document.body.classList.add('hd-quasar')
  watch(() => Dark.isActive, syncMode)
  let choice = 'system'
  try { choice = localStorage.getItem(KEY) || 'system' } catch (e) { /* ignore */ }
  applyTheme(choice === 'light' || choice === 'dark' ? choice : 'system')
}
