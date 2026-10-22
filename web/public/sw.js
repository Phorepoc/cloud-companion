// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// Minimal service worker: cache the app shell so the installed PWA starts offline; the cloud itself needs the network.
const CACHE = 'phecloud-shell-v1'
self.addEventListener('install', (e) => { self.skipWaiting() })
self.addEventListener('activate', (e) => { e.waitUntil(self.clients.claim()) })
self.addEventListener('fetch', (e) => {
  const u = new URL(e.request.url)
  if (e.request.method !== 'GET' || u.pathname.endsWith('/ws')) return
  e.respondWith(
    fetch(e.request).then((r) => {
      const copy = r.clone()
      caches.open(CACHE).then((c) => c.put(e.request, copy)).catch(() => {})
      return r
    }).catch(() => caches.match(e.request))
  )
})
