// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { createApp } from 'vue'
import { Quasar } from 'quasar'
import '@quasar/extras/material-icons/material-icons.css'
import 'quasar/dist/quasar.css'
import App from './App.vue'
import router from './router.js'
import { loadCreds } from './cloud.js'

loadCreds().then(() => {
  createApp(App).use(Quasar, { plugins: {} }).use(router).mount('#app')
  if ('serviceWorker' in navigator) navigator.serviceWorker.register('./sw.js').catch(() => {})
})
