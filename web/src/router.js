// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { createRouter, createWebHashHistory } from 'vue-router'
import { creds } from './cloud.js'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/browse' },
    { path: '/pair', component: () => import('./pages/PairPage.vue') },
    { path: '/browse', component: () => import('./pages/BrowserPage.vue') },
    { path: '/devices', component: () => import('./pages/DevicesPage.vue') },
    { path: '/settings', component: () => import('./pages/SettingsPage.vue') },
  ],
})

router.beforeEach((to) => {
  if (!creds.value && to.path !== '/pair') return '/pair'
  if (creds.value && to.path === '/pair') return '/browse'
})

export default router
