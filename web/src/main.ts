// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { createApp } from 'vue'
import { Quasar, Dark } from 'quasar'
import iconSet from 'quasar/icon-set/material-symbols-outlined'
import './css/symbols.css'
import 'quasar/dist/quasar.css'
// The Etesian design language (vendored, see css/etesian/), then the app's tokens and rules on top of it.
import './css/etesian/tokens.css'
import './css/etesian/base.css'
import './css/etesian/fonts.css'
import './css/etesian/quasar-theme.css'
import './css/tokens.css'
import './css/app.css'
import App from './App.vue'
import router from './router'
import { loadCreds } from './cloud'
import { initTheme } from './theme'
import { brand } from './brand'

document.title = brand.name
loadCreds().then(() => {
  createApp(App).use(Quasar, { plugins: { Dark }, iconSet, config: { ripple: false } }).use(router).mount('#app')
  initTheme()
  if ('serviceWorker' in navigator) navigator.serviceWorker.register('./sw.js').catch(() => {})
})
