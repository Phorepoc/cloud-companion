// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The product name and logo live HERE and nowhere else (see STYLE.md, "Brand token"). The name is undecided (candidates:
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

export function iconSvg() {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="${brand.colors.icon}"/><path d="${brand.logoPath}" fill="#fff"/></svg>`
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
