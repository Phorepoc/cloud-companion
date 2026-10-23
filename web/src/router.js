// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { createRouter, createWebHashHistory } from 'vue-router'
import { creds } from './cloud.js'

const section = (id) => ({ path: '/' + id, component: () => import('./pages/SectionPage.vue'), meta: { id } })

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/browse' },
    { path: '/pair', component: () => import('./pages/PairPage.vue') },
    { path: '/browse', component: () => import('./pages/BrowserPage.vue') },
    section('chats'),
    { path: '/mail', component: () => import('./pages/MailPage.vue') },
    { path: '/calendar', component: () => import('./pages/CalendarPage.vue') },
    { path: '/contacts', component: () => import('./pages/ContactsPage.vue') },
    section('files'),
    { path: '/devices', component: () => import('./pages/DevicesPage.vue') },
    { path: '/settings', component: () => import('./pages/SettingsPage.vue') },
  ],
})

router.beforeEach((to) => {
  if (!creds.value && to.path !== '/pair') return '/pair'
  if (creds.value && to.path === '/pair') return '/browse'
})

export default router
