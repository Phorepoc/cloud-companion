// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Theme choice: "system" (default, follows the operating system), "light" or "dark". Stored on this device only.
import { Dark } from 'quasar'
import { ref } from 'vue'

const KEY = 'phecloud-theme'
export const theme = ref('system')

export function applyTheme(choice) {
  theme.value = choice
  Dark.set(choice === 'dark' ? true : choice === 'light' ? false : 'auto')
  try { localStorage.setItem(KEY, choice) } catch (e) { /* private mode: the choice lasts for this visit */ }
}

export function initTheme() {
  let choice = 'system'
  try { choice = localStorage.getItem(KEY) || 'system' } catch (e) { /* ignore */ }
  applyTheme(['system', 'light', 'dark'].includes(choice) ? choice : 'system')
}
