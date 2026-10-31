<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The app shell, in the Etesian style for web pages. Phone (under 600 px): a slim top bar and the N9 dark toolbar at the bottom with
     four sections and "More". Tablet (600-1023 px): an icon rail. Desktop (1024 px and up): a labelled navigation drawer, and a way
     into the desktop view (DesktopView.vue), where the sections open as windows. The sections come from nav.ts. -->
<template>
  <DesktopView v-if="route.meta.desktop && creds" />
  <q-layout v-else view="hHh LpR lFf" class="ph-shell">
    <a href="#main" class="ph-skip" @click.prevent="focusMain">Skip to content</a>
    <q-header class="ph-bar hd-plain" bordered>
      <q-toolbar class="ph-toolbar">
        <BrandLogo :size="28" />
        <span class="ph-brand">{{ brand.name }}</span>
        <q-space />
        <div ref="titleActions" class="ph-title-actions" data-test="title-actions" />
        <span class="ph-status" :class="'is-' + state.status" data-test="status" role="status">
          <q-icon :name="state.status === 'online' ? 'sym_o_cloud_done' : state.status === 'connecting' ? 'sym_o_sync' : 'sym_o_cloud_off'" size="20px" aria-hidden="true" />
          {{ statusText }}
        </span>
        <q-btn v-if="creds && wide" flat no-caps icon="sym_o_desktop_windows" label="Desktop view" class="ph-desk-btn" data-test="desktop-view" @click="openDesktop" />
      </q-toolbar>
    </q-header>

    <q-drawer v-if="creds && !phone" show-if-above :mini="tablet" :width="240" :mini-width="72" bordered class="ph-drawer" :breakpoint="0">
      <nav aria-label="Sections">
        <q-list padding>
          <q-item v-for="s in visible" :key="s.id" :to="s.path" clickable v-ripple :active="isActive(s)" active-class="ph-nav-active"
                  :data-test="'nav-' + s.id" :aria-label="s.label">
            <q-item-section avatar><SectionTile :section="s" :size="32" /></q-item-section>
            <q-item-section>{{ s.label }}</q-item-section>
            <q-tooltip v-if="tablet" anchor="center right" self="center left">{{ s.label }}</q-tooltip>
          </q-item>
        </q-list>
      </nav>
    </q-drawer>

    <q-page-container id="main" tabindex="-1">
      <router-view />
    </q-page-container>

    <q-footer v-if="creds && phone" class="ph-bottom hd-dark">
      <nav aria-label="Sections" class="ph-bottom-nav">
        <router-link v-for="s in primary" :key="s.id" :to="s.path" class="ph-bn" :class="{ 'is-active': isActive(s) }" :data-test="'nav-' + s.id"
                     :aria-label="s.label" :aria-current="isActive(s) ? 'page' : undefined">
          <q-icon :name="s.icon" size="24px" aria-hidden="true" />
          <span>{{ s.label }}</span>
        </router-link>
        <button type="button" class="ph-bn" :class="{ 'is-active': moreActive }" data-test="nav-more" aria-haspopup="dialog" @click="moreOpen = true">
          <q-icon name="sym_o_more_horiz" size="24px" aria-hidden="true" />
          <span>More</span>
        </button>
      </nav>
    </q-footer>

    <ToastHost />

    <q-dialog v-model="moreOpen" position="bottom">
      <q-card class="ph-sheet" role="dialog" aria-label="More sections">
        <q-list>
          <q-item v-for="s in secondary" :key="s.id" :to="s.path" clickable v-ripple v-close-popup :data-test="'nav-' + s.id">
            <q-item-section avatar><SectionTile :section="s" :size="32" /></q-item-section>
            <q-item-section>{{ s.label }}</q-item-section>
          </q-item>
        </q-list>
      </q-card>
    </q-dialog>
  </q-layout>
</template>

<script setup lang="ts">
import { computed, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuasar } from 'quasar'
import { state, creds } from './cloud'
import { sections, type Section } from './nav'
import { brand } from './brand'
import BrandLogo from './components/BrandLogo.vue'
import ToastHost from './components/ToastHost.vue'
import SectionTile from './components/SectionTile.vue'
import DesktopView from './desktop/DesktopView.vue'
import { setLayout } from './desktop/layout'
import { TITLE_ACTIONS } from './components/titlebar'
import { listenForRemote } from './music'
import { loadTracks } from './files'

const $q = useQuasar()
const route = useRoute()
const router = useRouter()
const moreOpen = ref(false)
const titleActions = ref<HTMLElement | null>(null)
provide(TITLE_ACTIONS, titleActions)
const phone = computed(() => $q.screen.lt.sm)
const tablet = computed(() => $q.screen.sm)
const wide = computed(() => $q.screen.gt.sm)
const visible = computed(() => sections.filter((s) => !s.admin || state.admin))
const primary = computed(() => visible.value.filter((s) => s.primary))
const secondary = computed(() => visible.value.filter((s) => !s.primary))
const isActive = (s: Section) => route.path === s.path
const moreActive = computed(() => secondary.value.some(isActive))
const statusText = computed(() => ({ online: 'Connected', connecting: 'Connecting', offline: 'Offline' })[state.status] || state.status)

function openDesktop() {
  setLayout('desktop')
  router.push({ path: '/desktop', query: { open: route.path.slice(1) } })
}

function focusMain() {
  document.getElementById('main')?.focus()
}

// music sent from another device ("Play on") starts here
watch(() => !!creds.value, (on) => { if (on) listenForRemote(loadTracks) }, { immediate: true })
</script>

<style scoped>
.ph-bar { background: var(--ph-surface); color: var(--ph-text); }
.ph-toolbar { min-height: 56px; gap: var(--ph-s2); }
/* Etesian site header: the brand in bold capitals, quiet links and status */
.ph-bar { background: var(--ph-bg); }
.ph-brand { font-family: var(--ph-font); font-weight: var(--hd-font-weight-bold); font-size: var(--ph-text-body); text-transform: uppercase; letter-spacing: 0.02em; }
.ph-desk-btn { color: var(--ph-text); font-weight: var(--hd-font-weight-regular); }
.ph-title-actions { display: flex; align-items: center; gap: 2px; min-width: 0; overflow-x: auto; }
.ph-title-actions:empty { display: none; }
/* a page that puts buttons in the bar (Browser, Android) gets the room the brand name took */
@media (max-width: 599px) { .ph-toolbar:has(.ph-title-actions:not(:empty)) .ph-brand { display: none; } }
.ph-status { display: inline-flex; align-items: center; gap: var(--ph-s1); font-size: var(--ph-text-small); color: var(--ph-text-muted); }
.ph-status.is-online { color: var(--ph-positive); }
.ph-drawer { background: var(--ph-surface); }
.ph-drawer :deep(.q-item) { margin: 2px var(--ph-s2); border-radius: var(--ph-r-control); color: var(--ph-text); }
.ph-drawer :deep(.ph-nav-active) { background: var(--ph-accent-soft); color: var(--ph-accent); font-weight: var(--hd-font-weight-regular); }
.ph-drawer :deep(.q-item__section--avatar) { min-width: 44px; }
/* the N9 toolbar: dark gradient, icons with tiny labels; the current section at full strength */
.ph-bottom { padding-bottom: env(safe-area-inset-bottom); }
.ph-bottom-nav { display: flex; }
.ph-bn {
  flex: 1 1 0; min-width: 0; min-height: 56px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px;
  background: none; border: 0; color: var(--ph-chrome-fg-muted); font: inherit; font-size: var(--ph-text-caption); text-decoration: none; cursor: pointer;
}
.ph-bn.is-active { color: var(--ph-chrome-fg); font-weight: var(--hd-font-weight-bold); }
.ph-sheet { width: 100%; max-width: 480px; border-radius: var(--ph-r-sheet) var(--ph-r-sheet) 0 0 !important; padding-bottom: env(safe-area-inset-bottom); }
</style>
