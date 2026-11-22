// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
import { createApp } from 'vue'
import { Quasar, Dark } from 'quasar'
import iconSet from 'quasar/icon-set/material-symbols-outlined'
import './css/symbols.css'
import 'quasar/dist/quasar.css'
// The Etesian design language: tokens and base from the package (here first, so the library's own import of them lands
// before the overrides below), the Quasar theme from @etesian-design/quasar, then the app's fonts, tokens and rules on top of it.
import '@etesian-design/tokens/styles/tokens.css'
import '@etesian-design/tokens/styles/base.css'
import '@etesian-design/vue/style.css'
import '@etesian-design/shell/style.css'
import './css/etesian/fonts.css'
import '@etesian-design/quasar/theme.css'
import './css/tokens.css'
import './css/app.css'
import './css/panes.css'
import App from './App.vue'
import router from './router'
import { connectAtStart, loadCreds } from './cloud'
import { initTheme } from './theme'
import { brand } from './brand'

document.title = brand.name

// a pane that scrolls shows its thin bar for a moment, then hides it again (css/panes.css): no scrollbar at rest
const scrolling = new WeakMap<Element, number>()
document.addEventListener('scroll', (e) => {
  const el = e.target
  if (!(el instanceof HTMLElement)) return
  el.classList.add('is-scrolling')
  clearTimeout(scrolling.get(el))
  scrolling.set(el, window.setTimeout(() => el.classList.remove('is-scrolling'), 900))
}, { capture: true, passive: true })
loadCreds().then(() => {
  createApp(App).use(Quasar, { plugins: { Dark }, iconSet, config: { ripple: false } }).use(router).mount('#app')
  initTheme()
  connectAtStart()
  if ('serviceWorker' in navigator) navigator.serviceWorker.register('./sw.js').catch(() => {})
})
