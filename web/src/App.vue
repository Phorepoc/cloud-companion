<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The app shell, in the Etesian style for web pages. Phone (under 600 px): a slim top bar and the N9 dark toolbar at the bottom with
     four sections and "More". Tablet (600-1023 px): an icon rail. Desktop (1024 px and up): a labelled navigation drawer, and a way
     into the desktop view (DesktopView.vue), where the sections open as windows. The sections come from nav.ts. -->
<template>
  <DesktopView v-if="route.meta.desktop && creds" />
  <!-- the classic layout: one section at a time, the efficient single-task view. The Etesian classic bar (@etesian-design/shell
       XClassic): the apps button (a grid of the sections with search and the recent ones, and Desktop on a wide screen), the section's
       squircle and title (a menu of its views), its actions (as many as fit, the rest in ⋮; below 600 px a bottom bar), the status. -->
  <!-- a media app in Hybrid is dark all the way, its bar included, as on the N9 -->
  <XClassic v-else-if="creds && route.path !== '/pair'" class="ph-classic" :class="{ 'hd-mode-dark': pageMode(current) }" :data-hd-mode="pageMode(current)" :brand="brand.name" :apps="apps" :app="current" :recent="recent"
            :status="statusText" :online="state.status === 'online'" data-test="classic" @open="openApp">
    <template #logo><BrandLogo :size="22" /></template>
    <q-layout view="hHh lpR fFf" container class="ph-classic__layout">
      <q-page-container id="main" tabindex="-1">
        <router-view />
      </q-page-container>
    </q-layout>
    <ToastHost />
  </XClassic>
  <!-- pairing (and its recovery-code dialog, until it moves on): just the page -->
  <q-layout v-else view="hHh lpR fFf" class="ph-shell">
    <q-page-container id="main" tabindex="-1">
      <router-view />
    </q-page-container>
    <ToastHost />
  </q-layout>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuasar } from 'quasar'
import { XClassic } from '@etesian-design/shell'
import '@etesian-design/vue/style.css'
import '@etesian-design/shell/style.css'
import { state, creds } from './cloud'
import { sections } from './nav'
import { brand } from './brand'
import BrandLogo from './components/BrandLogo.vue'
import { pageMode } from './theme'
import ToastHost from './components/ToastHost.vue'
import DesktopView from './desktop/DesktopView.vue'
import { setLayout, DESKTOP_MIN_WIDTH } from './desktop/layout'
import { listenForRemote } from './music'
import { loadTracks } from './files'

const $q = useQuasar()
const route = useRoute()
const router = useRouter()
const visible = computed(() => sections.filter((s) => !s.admin || state.admin))
// the apps menu: the sections, and on a screen big enough the desktop (the sections as windows)
const apps = computed(() => [
  ...visible.value.map((s) => ({ id: s.id, title: s.label, icon: s.glyph, color: s.color })),
  ...($q.screen.width >= DESKTOP_MIN_WIDTH ? [{ id: 'desktop', title: 'Desktop', icon: 'window', color: '#3E4A5C' }] : []),
])
const current = computed(() => visible.value.find((s) => route.path === s.path || route.path.startsWith(s.path + '/'))?.id ?? '')
const statusText = computed(() => ({ online: 'Connected', connecting: 'Connecting', offline: 'Offline' } as Record<string, string>)[state.status] || state.status)

// the recently used sections, newest first, kept on this device
const RECENT = 'phecloud-recent'
const recent = ref<string[]>((() => { try { return JSON.parse(localStorage.getItem(RECENT) || '[]') } catch { return [] } })())
watch(current, (id) => {
  if (!id) return
  recent.value = [id, ...recent.value.filter((r) => r !== id)].slice(0, 4)
  try { localStorage.setItem(RECENT, JSON.stringify(recent.value)) } catch { /* private mode */ }
}, { immediate: true })

function openApp(id: string) {
  if (id === 'desktop') { setLayout('desktop'); router.push({ path: '/desktop', query: { open: route.path.slice(1) } }); return }
  const s = sections.find((x) => x.id === id)
  if (s) router.push(s.path)
}

// music sent from another device ("Play on") starts here
watch(() => !!creds.value, (on) => { if (on) listenForRemote(loadTracks) }, { immediate: true })
// signed out from another device (cloud.ts removed()): back to pairing
watch(() => !!creds.value, (on, was) => { if (was && !on && route.path !== '/pair') router.replace('/pair') })
</script>

<style scoped>
.ph-classic { position: fixed; inset: 0; }
.ph-classic__layout { flex: 1; min-width: 0; height: 100%; }
</style>

<style>
/* outline and flat accent buttons read as the light accent on a dark surface (Dark, or a media app in Hybrid) */
[data-hd-mode="dark"] .q-btn--outline.text-primary, [data-hd-mode="dark"] .q-btn--flat.text-primary { color: var(--hd-c-accent-text) !important; }
/* a desktop window in its own colour mode (a media app in Hybrid) fills its area, as the page itself would */
.ph-page-mode { min-height: 100%; }
.ph-win__layout .ph-page-mode, .ph-page-mode > .q-layout { height: 100%; }
</style>
