// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// What the routes carry in `meta` (router.ts).
export {}
declare module 'vue-router' {
  interface RouteMeta {
    id?: string       // a placeholder section: SectionPage shows its empty state
    desktop?: boolean // the desktop view (App.vue draws it instead of the page layout)
  }
}
