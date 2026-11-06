// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
import { createRouter, createWebHashHistory } from 'vue-router'
import { creds } from './cloud'
import { getLayout, desktopFits } from './desktop/layout'

const section = (id: string) => ({ path: '/' + id, component: () => import('./pages/SectionPage.vue'), meta: { id } })

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: () => (getLayout() === 'desktop' && desktopFits() ? '/desktop' : '/browse') },
    // the desktop view: App.vue draws it instead of the page layout, so the route itself renders nothing
    { path: '/desktop', component: { render: () => null }, meta: { desktop: true } },
    { path: '/pair', component: () => import('./pages/PairPage.vue') },
    { path: '/browse', component: () => import('./pages/BrowserPage.vue') },
    { path: '/chats', component: () => import('./pages/ChatsPage.vue') },
    { path: '/mail', component: () => import('./pages/MailPage.vue') },
    { path: '/calendar', component: () => import('./pages/CalendarPage.vue') },
    { path: '/contacts', component: () => import('./pages/ContactsPage.vue') },
    { path: '/files', component: () => import('./pages/FilesPage.vue') },
    { path: '/photos', component: () => import('./pages/PhotosPage.vue') },
    { path: '/feeds', component: () => import('./pages/FeedsPage.vue') },
    { path: '/music', component: () => import('./pages/MusicPage.vue') },
    { path: '/android', component: () => import('./pages/AndroidPage.vue') },
    // Connections, Devices and Catalogue are pages of Settings; their old addresses open them there
    { path: '/connections', redirect: '/settings/connections' },
    { path: '/catalogue', redirect: '/settings/catalogue' },
    { path: '/devices', redirect: '/settings/devices' },
    { path: '/settings/:page?', component: () => import('./pages/SettingsPage.vue') },
  ],
})

router.beforeEach((to) => {
  if (!creds.value && to.path !== '/pair') return '/pair'
  if (creds.value && to.path === '/pair') return '/browse'
  if (to.meta.desktop && !desktopFits()) return '/browse'
})

export default router
