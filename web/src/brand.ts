// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The product name and logo live HERE and nowhere else (see STYLE.md, "Brand token"). The name is decided: Phorepoc Cloud Companion (earlier candidates:
// Cloud Companion, Cloud Pal, Porter): change `name`, `short`, `logoPath` and `colors`, rebuild, done. The page title, the web app
// manifest, the app bar and the pairing page all read this file.
export const brand = {
  name: 'Cloud Companion',
  short: 'Companion',
  tagline: 'Your contacts, calendar, mail, chats and browser, on every device.',
  // the colours of the app icon and the browser chrome (the UI palette itself is in css/tokens.css)
  colors: { light: '#0b5cad', dark: '#101317', icon: '#0b5cad' },
  // a 64 x 64 path drawn in currentColor; replace with the real logo
  logoPath: 'M20 42a9 9 0 0 1 1-18 12 12 0 0 1 23 3 7.5 7.5 0 0 1 0 15z',
}

// the N9 squircle at 64 px (the Etesian icon shape: @etesian-design/vue squirclePath(64), n = 2.8), inlined because the build reads this file
export const SQUIRCLE_64 = 'M64.00 32.00L63.95 36.56L63.80 39.47L63.56 41.96L63.22 44.19L62.78 46.23L62.24 48.11L61.61 49.87L60.88 51.50L60.05 53.03L59.12 54.45L58.10 55.77L56.98 56.98L55.77 58.10L54.45 59.12L53.03 60.05L51.50 60.88L49.87 61.61L48.11 62.24L46.23 62.78L44.19 63.22L41.96 63.56L39.47 63.80L36.56 63.95L32.00 64.00L27.44 63.95L24.53 63.80L22.04 63.56L19.81 63.22L17.77 62.78L15.89 62.24L14.13 61.61L12.50 60.88L10.97 60.05L9.55 59.12L8.23 58.10L7.02 56.98L5.90 55.77L4.88 54.45L3.95 53.03L3.12 51.50L2.39 49.87L1.76 48.11L1.22 46.23L0.78 44.19L0.44 41.96L0.20 39.47L0.05 36.56L0.00 32.00L0.05 27.44L0.20 24.53L0.44 22.04L0.78 19.81L1.22 17.77L1.76 15.89L2.39 14.13L3.12 12.50L3.95 10.97L4.88 9.55L5.90 8.23L7.02 7.02L8.23 5.90L9.55 4.88L10.97 3.95L12.50 3.12L14.13 2.39L15.89 1.76L17.77 1.22L19.81 0.78L22.04 0.44L24.53 0.20L27.44 0.05L32.00 0.00L36.56 0.05L39.47 0.20L41.96 0.44L44.19 0.78L46.23 1.22L48.11 1.76L49.87 2.39L51.50 3.12L53.03 3.95L54.45 4.88L55.77 5.90L56.98 7.02L58.10 8.23L59.12 9.55L60.05 10.97L60.88 12.50L61.61 14.13L62.24 15.89L62.78 17.77L63.22 19.81L63.56 22.04L63.80 24.53L63.95 27.44Z'

export function iconSvg() {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><path d="${SQUIRCLE_64}" fill="${brand.colors.icon}"/><path d="${brand.logoPath}" fill="#fff"/></svg>`
}

export function manifest() {
  return JSON.stringify({
    name: brand.name,
    short_name: brand.short,
    description: brand.tagline,
    start_url: './',
    display: 'standalone',
    background_color: brand.colors.dark,
    theme_color: brand.colors.light,
    icons: [{ src: 'icon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any' }],
  }, null, 2)
}
