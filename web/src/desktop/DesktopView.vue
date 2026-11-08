<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- The desktop view (1024 px and up): the app's sections as windows, as if the server were a computer you are sitting at. It is the
     shared Etesian desktop (@etesian-design/shell XShell, the one on etesian-design.com): a menu bar, the dock (Overview, the pinned
     and running sections, the recycle bin), overlapping windows that snap and fill the screen, the full-screen Overview (Events | the
     open windows | the sections) and Events. Cloud Companion brings the sections, its Events panel and its widgets; each page puts
     its title, views and actions in its window's header (useChrome). No Super or Ctrl+Alt shortcuts: the browser's system takes
     them. The windows, the dock and the widgets are kept on the server (prefs.ts), so every browser shows the same desktop; the
     layout choice (desktop or classic) stays with this device. -->
<template>
  <div class="ph-desk" data-test="desktop">
    <XShell v-if="ready" ref="shell" :apps="apps" :name="brand.name" :menus="menus" storage-key="phecloud-shell" :storage="shellStorage" :events-count="unread || undefined" :bin="false"
            dock-preview @menu="onMenu">
      <template #mark><BrandLogo :size="18" /></template>
      <template #indicators>
        <span class="ph-desk__status" :class="'is-' + state.status" role="status" data-test="status">{{ statusText }}</span>
      </template>
      <template #window="{ id }">
        <WinPage :id="id" :page="pages[id]" />
      </template>
      <template #events="{ embedded, close }">
        <EventsPanel :now="now" :embedded="embedded" @open="(id: string) => { close(); open(id) }" @close="close" @unread="(n: number) => (unread = n)" />
      </template>
      <template #widgets="{ editing, done, area }">
        <div class="ph-desk__wg" :style="{ left: area.x + 'px', top: area.y + 'px' }"><DesktopWidgets :now="now" :area="area" :editing="editing" @open="open" @done="done" /></div>
      </template>
    </XShell>
    <ToastHost />
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, defineComponent, h, nextTick, onBeforeUnmount, onMounted, ref, type Component } from 'vue'
import { QLayout, QPageContainer } from 'quasar'
import { useRoute, useRouter } from 'vue-router'
import { XShell, type ShellApp, type ShellMenu } from '@etesian-design/shell'
import '@etesian-design/vue/style.css'
import '@etesian-design/shell/style.css'
import { state } from '../cloud'
import { sections } from '../nav'
import { brand } from '../brand'
import BrandLogo from '../components/BrandLogo.vue'
import { HdTheme } from '@etesian-design/vue'
import { pageMode } from '../theme'
import ToastHost from '../components/ToastHost.vue'
import EventsPanel from './EventsPanel.vue'
import DesktopWidgets from './DesktopWidgets.vue'
import { loadUnread } from './glance'
import { getLayout, setLayout } from './layout'
import { settingsPage, settingsPageIds } from '../settingsNav'
import { loadPrefs, shellStorage } from '../prefs'

const route = useRoute()
const router = useRouter()
const shell = ref<InstanceType<typeof XShell> | null>(null)
const visible = computed(() => sections.filter((s) => !s.admin || state.admin))
// the dock pins the main sections (the phone's bottom bar ones); the others show there while they are open
const apps = computed<ShellApp[]>(() => visible.value.map((s) => ({ id: s.id, title: s.label, icon: s.glyph, color: s.color, pinned: !!s.primary })))

// each section's page, as the router knows it (lazy), plus the section id for the placeholder page
const pages: Record<string, { component: Component | undefined; props: Record<string, unknown> }> = Object.fromEntries(sections.map((s) => {
  const rec = router.getRoutes().find((r) => r.path === s.path || r.path.startsWith(s.path + '/:')) // /settings/:page?
  const c = rec?.components?.default
  return [s.id, { component: typeof c === 'function' ? defineAsyncComponent(c as () => Promise<Component>) : c, props: rec?.meta?.id ? { sectionId: s.id } : {} }]
}))

/** a page in its window (its title, views and actions reach the window's header through useChrome) */
const WinPage = defineComponent({
  props: { id: { type: String, required: true }, page: { type: Object, required: true } },
  setup(p) {
    // a page is a QPage: it needs a layout around it, here one that fills the window
    return () => {
      if (!p.page.component) return null
      const page = h(QLayout, { view: 'hhh lpr fff', container: true, class: 'ph-win__layout' }, () => h(QPageContainer, null, () => h(p.page.component!, p.page.props)))
      const mode = pageMode(p.id)
      return mode ? h(HdTheme, { mode, class: 'ph-page-mode' }, () => page) : page // the N9's dark media apps, in Hybrid
    }
  },
})

const menus = computed<ShellMenu[]>(() => [{ label: brand.name, items: [{ id: 'classic', label: 'Classic view', icon: 'window' }] }])
function onMenu(id: string) { if (id === 'classic') leave() }
let leaving = false
function leave() {
  leaving = true
  setLayout('classic')
  const f = shell.value?.wm.focused.value
  router.push(f === 'settings' ? '/settings/' + settingsPage.value : sections.find((s) => s.id === f)?.path ?? '/browse')
}
function open(id: string) { shell.value?.open(id) }
/** a page that links to another section opens it as a window instead of leaving the desktop */
const stopGuard = router.beforeEach((to, from) => {
  if (leaving || !from.meta.desktop || getLayout() !== 'desktop') return
  const s = sections.find((x) => x.path === to.path)
  if (s) { open(s.id); return false }
  const sp = to.path.match(/^\/(?:settings\/)?(\w+)$/)?.[1] // a page of Settings, also by its old address (/connections)
  if (sp && settingsPageIds.includes(sp)) { settingsPage.value = sp; open('settings'); return false }
})

const statusText = computed(() => ({ online: 'Connected', connecting: 'Connecting', offline: 'Offline' } as Record<string, string>)[state.status] || state.status)
const now = ref(new Date())
const unread = ref(0)
async function checkUnread() { try { unread.value = (await loadUnread(0)).unread } catch { unread.value = 0 } }

let clock: ReturnType<typeof setInterval> | undefined, mailTimer: ReturnType<typeof setInterval> | undefined
// the desktop starts once its windows, dock and widgets are read from the server (offline: as new, and kept from here)
const ready = ref(false)
onMounted(async () => {
  clock = setInterval(() => { now.value = new Date() }, 15000)
  checkUnread()
  mailTimer = setInterval(checkUnread, 120000)
  await loadPrefs().catch(() => { /* not reachable: start empty */ })
  ready.value = true
  await nextTick()
  const want = String(route.query.open || '')
  const sp = want.match(/^settings\/(\w+)$/)?.[1]
  if (sp && settingsPageIds.includes(sp)) { settingsPage.value = sp; open('settings') }
  else if (sections.some((s) => s.id === want)) open(want)
  else if (!shell.value?.wm.shown.value.length) open('browse')
})
onBeforeUnmount(() => { stopGuard(); clearInterval(clock); clearInterval(mailTimer) })
</script>

<style scoped>
.ph-desk { position: fixed; inset: 0; display: flex; flex-direction: column; }
.ph-desk > :deep(.x-desktop) { flex: 1; min-height: 0; }
.ph-desk__status { font-size: 12px; color: var(--hd-c-fg-secondary); margin-right: 6px; }
.ph-desk__status.is-online { color: #7BD34A; }
.ph-desk__wg { position: absolute; top: 0; right: 0; bottom: 0; left: 0; }
.ph-desk :deep(.ph-win__layout) { height: 100%; width: 100%; }
</style>
