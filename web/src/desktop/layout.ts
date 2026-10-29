// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Layout choice: "classic" (pages with navigation, the default) or "desktop" (the sections as windows on a desktop, from 1024 px).
// Stored on this device only, like the theme; the window arrangement is stored next to it (DesktopView.vue).
const KEY = 'phecloud-layout'
export const DESKTOP_MIN_WIDTH = 1024

export type Layout = 'classic' | 'desktop'

export function getLayout(): Layout {
  try { return localStorage.getItem(KEY) === 'desktop' ? 'desktop' : 'classic' } catch (e) { return 'classic' }
}

export function setLayout(choice: Layout) {
  try { localStorage.setItem(KEY, choice) } catch (e) { /* private mode: the choice lasts for this visit */ }
}

/** The desktop needs room for windows: on a smaller screen the classic layout is used whatever the choice. */
export const desktopFits = () => window.innerWidth >= DESKTOP_MIN_WIDTH
