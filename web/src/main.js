// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { createApp } from 'vue'
import { Quasar, Dark } from 'quasar'
import iconSet from 'quasar/icon-set/material-symbols-outlined'
import './css/symbols.css'
import 'quasar/dist/quasar.css'
import './css/tokens.css'
import './css/app.css'
import App from './App.vue'
import router from './router.js'
import { loadCreds } from './cloud.js'
import { initTheme } from './theme.js'
import { brand } from './brand.js'

document.title = brand.name
loadCreds().then(() => {
  createApp(App).use(Quasar, { plugins: { Dark }, iconSet }).use(router).mount('#app')
  initTheme()
  if ('serviceWorker' in navigator) navigator.serviceWorker.register('./sw.js').catch(() => {})
})
