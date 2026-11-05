// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The service worker (TypeScript; public/sw.js is compiled from it by `npm run emit`). Minimal: cache the app shell so the installed PWA starts offline; the cloud itself needs the network.

const sw = self as unknown as ServiceWorkerGlobalScope // a script, not a module: tsconfig.sw.json gives it the worker's types only
const CACHE = 'phecloud-shell-v1'
sw.addEventListener('install', () => { sw.skipWaiting() })
sw.addEventListener('activate', (e) => { e.waitUntil(sw.clients.claim()) })
sw.addEventListener('fetch', (e) => {
  const u = new URL(e.request.url)
  if (e.request.method !== 'GET' || u.pathname.endsWith('/ws')) return
  e.respondWith(
    fetch(e.request).then((r) => {
      const copy = r.clone()
      caches.open(CACHE).then((c) => c.put(e.request, copy)).catch(() => {})
      return r
    }).catch(async () => (await caches.match(e.request)) || Response.error()) // offline and not kept: a network error, as without us
  )
})
