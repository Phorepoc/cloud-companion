<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-browse">
    <!-- the Etesian browser frame around Firefox's picture: the tabs, address and buttons are ours and follow Firefox live -->
    <XBrowserFrame class="ph-bf" :address="address" :secure="secure" :back="true" :forward="true"
                   :loading="engine.busy ? 60 : undefined" :tabs="tabs" :current="current" :tabs-at="browserPrefs.tabsAt"
                   data-test="browser-frame"
                   @go="go" @back="act({ c: 'back' })" @forward="act({ c: 'forward' })" @reload="act({ c: 'reload' })"
                   @pick="(v: string) => act({ c: 'pick', id: Number(v) })" @close="(v: string) => command({ c: 'close', id: Number(v) })"
                   @add="newTab" @more="moreOpen = true">
      <template #more>
        <q-menu v-model="moreOpen" no-parent-event anchor="bottom right" self="top right" class="ph-bf-more" data-test="browser-more">
          <q-list dense style="min-width: 220px">
            <q-item clickable v-close-popup data-test="tab-new" @click="newTab"><q-item-section>New tab</q-item-section></q-item>
            <q-item clickable v-close-popup data-test="other-devices" @click="othersOpen = true"><q-item-section>Tabs on your devices</q-item-section></q-item>
            <q-item clickable v-close-popup :disable="!activeTab()?.url || shown(activeTab()?.url) === ''" data-test="send-device" @click="openSend"><q-item-section>Send to device</q-item-section></q-item>
            <q-item clickable v-close-popup role="menuitemcheckbox" :aria-checked="String(desktopSite)" :disable="!activeTab()" data-test="desktop-site" @click="toggleSite">
              <q-item-section>Desktop site</q-item-section>
              <q-item-section side><q-toggle :model-value="desktopSite" dense tabindex="-1" aria-hidden="true" @click.stop="toggleSite" /></q-item-section>
            </q-item>
            <q-item>
              <q-item-section>Zoom</q-item-section>
              <q-item-section side>
                <div class="ph-bf-zoom">
                  <q-btn flat round dense icon="sym_o_remove" aria-label="Zoom out" data-test="zoom-out" @click="zoomBy(-0.25)" />
                  <span class="ph-small" aria-live="polite" data-test="zoom-pct">{{ Math.round(engine.zoom * 100) }}%</span>
                  <q-btn flat round dense icon="sym_o_add" aria-label="Zoom in" data-test="zoom-in" @click="zoomBy(0.25)" />
                </div>
              </q-item-section>
            </q-item>
            <q-item clickable v-close-popup role="menuitemcheckbox" :aria-checked="String(browserPrefs.tabsAt === 'side')" data-test="tabs-side" @click="toggleSide">
              <q-item-section>Tabs on the side</q-item-section>
              <q-item-section side><q-toggle :model-value="browserPrefs.tabsAt === 'side'" dense tabindex="-1" aria-hidden="true" @click.stop="toggleSide" /></q-item-section>
            </q-item>
          </q-list>
        </q-menu>
      </template>
      <div class="ph-bf__page">
        <span class="ph-sr" data-test="title">{{ activeTab()?.title }}</span>
        <q-banner v-if="engine.note" class="ph-note" rounded role="status" data-test="browser-note">
          <template #avatar><q-icon name="sym_o_download" aria-hidden="true" /></template>
          {{ engine.note }}
          <template #action>
            <q-btn v-if="engine.saved" flat label="Show in Files" data-test="download-show" @click="showDownloads" />
            <q-btn flat label="OK" @click="engine.note = ''" />
          </template>
        </q-banner>
        <div ref="stage" class="ph-stage" data-test="stage">
          <HdStatusState v-if="engine.status === 'insecure'" state="error" title="The browser needs a secure address" text="Open Cloud Companion over https or at localhost." data-test="browser-insecure" />
          <HdStatusState v-else-if="engine.status === 'unconfigured'" state="empty" title="The browser is not set up" text="Turn on Browser in Settings, Features (or start Cloud Companion with docker-compose.browser.yml)." data-test="browser-off" />
          <HdStatusState v-else-if="engine.status === 'failed'" state="error" title="The browser could not be reached" text="It may still be starting." data-test="browser-failed" />
          <div v-show="engine.status === 'live' || engine.status === 'connecting'" ref="screen" class="ph-screen" data-test="screen" />
          <div v-if="engine.status === 'connecting'" class="ph-connecting" role="status" data-test="browser-connecting"><q-spinner size="32px" /> Connecting to the browser…</div>
        </div>
      </div>
    </XBrowserFrame>
    <!-- one browser for all your devices: every tab is here, whichever device opened it -->
    <q-dialog v-model="othersOpen">
      <q-card class="ph-dialog" data-test="others-dialog">
        <q-card-section>
          <h2>Tabs on your devices</h2>
          <p class="ph-muted ph-small">All your devices share one browser, so these are the same everywhere.</p>
          <ul class="ph-tablist">
            <li v-for="t in engine.tabs" :key="t.id" class="ph-trow" :data-test="'fx-tab-' + t.id">
              <span class="ph-ttext"><span class="ph-ttitle">{{ t.title || 'New tab' }}</span><span class="ph-small ph-muted ph-turl">{{ t.url }}</span></span>
              <q-btn flat round icon="sym_o_open_in_new" aria-label="Open here" v-close-popup @click="act({ c: 'pick', id: t.id })"><q-tooltip>Open here</q-tooltip></q-btn>
              <q-btn flat round icon="sym_o_close" :aria-label="'Close ' + (t.title || 'tab')" @click="command({ c: 'close', id: t.id })" />
            </li>
          </ul>
        </q-card-section>
        <q-card-actions align="right"><q-btn flat label="Close" v-close-popup /></q-card-actions>
      </q-card>
    </q-dialog>
    <!-- a page's "choose a file": the files of this device, uploaded to Cloud Companion and handed to the page -->
    <q-dialog :model-value="!!engine.pick" persistent>
      <q-card v-if="engine.pick" class="ph-dialog" data-test="pick-dialog">
        <q-card-section>
          <h2>{{ engine.pick.multiple ? 'Choose files' : 'Choose a file' }}</h2>
          <p class="ph-muted">{{ engine.pick.site || 'The page' }} asks for {{ engine.pick.multiple ? 'files' : 'a file' }} from this device.</p>
          <p v-if="pickError" class="ph-err" role="alert">{{ pickError }}</p>
          <input ref="picker" type="file" class="ph-sr" :accept="engine.pick.accept" :multiple="engine.pick.multiple" data-test="pick-input" @change="picked" />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat label="Cancel" :disable="uploading" data-test="pick-cancel" @click="cancelPick" />
          <q-btn unelevated color="primary" :loading="uploading" :label="engine.pick.multiple ? 'Choose files' : 'Choose a file'" data-test="pick-choose" @click="picker?.click()" />
        </q-card-actions>
      </q-card>
    </q-dialog>
    <!-- the page on another device: it asks there whether to open it in the Browser or in its own browser -->
    <q-dialog v-model="sendOpen">
      <q-card class="ph-dialog" data-test="send-dialog">
        <q-card-section>
          <h2>Send to device</h2>
          <p class="ph-muted ph-small ph-ttitle">{{ activeTab()?.title || activeTab()?.url }}</p>
          <HdStatusState v-if="!targets.length" state="empty" title="No other device is connected" text="Open Cloud Companion on the device you want to send this page to." />
          <ul v-else class="ph-tablist">
            <li v-for="s in targets" :key="s.sid" class="ph-trow" :data-test="'send-to-' + s.sid">
              <span class="ph-ttext"><span class="ph-ttitle">{{ s.name || 'Device' }}</span></span>
              <q-btn flat color="primary" label="Send" v-close-popup :data-test="'send-btn-' + s.sid" @click="sendTo(s)" />
            </li>
          </ul>
        </q-card-section>
        <q-card-actions align="right"><q-btn flat label="Close" v-close-popup /></q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { state, api, apiUpload, browserPrefs, setBrowserPref } from '../cloud'
import { toast } from '../toast'
import { useRouter } from 'vue-router'
import type { SessionInfo } from '../proto'
import { HdStatusState } from '@etesian-design/vue'
import { XBrowserFrame } from '@etesian-design/shell'
import { engine, activeTab, act, command, focusPage, fitScreen, start, stop } from '../browserEngine'

const router = useRouter()
const picker = ref<HTMLInputElement | null>(null)
const uploading = ref(false), pickError = ref('')
async function picked() {
  const p = engine.pick, files = picker.value?.files
  if (!p || !files?.length) return
  const form = new FormData()
  for (const f of files) form.append('file', f, f.name)
  uploading.value = true; pickError.value = ''
  try { await apiUpload('/phe/browser/upload?id=' + encodeURIComponent(p.id), form); engine.pick = null; focusPage() }
  catch (e) { pickError.value = (e as Error).message }
  finally { uploading.value = false }
}
function cancelPick() { const p = engine.pick; if (p) command({ c: 'files', id: p.id, files: [] }); engine.pick = null; pickError.value = ''; focusPage() }
function showDownloads() { engine.note = ''; void router.push({ path: '/files', query: { dir: 'Downloads' } }) }
const stage = ref<HTMLElement | null>(null)
const screen = ref<HTMLElement | null>(null)
const moreOpen = ref(false)
const othersOpen = ref(false)
const typed = ref('')
const tabs = computed(() => engine.tabs.map((t) => ({ id: String(t.id), title: t.title || t.url || 'New tab', address: t.url })))
const current = computed(() => String(activeTab()?.id ?? ''))
const shown = (u?: string) => (!u || u.startsWith('about:') ? '' : u)
const address = computed(() => typed.value || shown(activeTab()?.url))
watch(() => activeTab()?.url, () => { typed.value = '' })
const secure = computed(() => !address.value || address.value.startsWith('https:'))
function toggleSide() { setBrowserPref('tabsAt', browserPrefs.tabsAt === 'side' ? 'top' : 'side') }
// a phone-sized window opens tabs with the phone version of sites, as a phone's browser does; "Desktop site" changes one tab
const narrow = () => (stage.value?.getBoundingClientRect().width ?? window.innerWidth) < 600
function newTab() { act({ c: 'newTab', mobile: narrow() }); focusPage() }
const desktopSite = computed(() => !activeTab()?.mobile)
function toggleSite() { const t = activeTab(); if (t) act({ c: 'site', id: t.id, mobile: !t.mobile }) }
// one entry per other device (a device with two windows open is still one place to send to)
const sendOpen = ref(false)
const targets = computed(() => {
  const seen = new Set<string>(state.sessions.filter((s) => s.self).map((s) => s.dev))
  return state.sessions.filter((s) => !seen.has(s.dev) && (seen.add(s.dev), true))
})
function openSend() { sendOpen.value = true; api.sessions() }
function sendTo(s: SessionInfo) {
  const t = activeTab()
  if (!t?.url) return
  api.remote({ sid: s.sid, action: 'send', url: t.url, title: t.title })
  toast('Sent to ' + (s.name || 'the device'))
}
function zoomBy(d: number) { command({ c: 'zoom', factor: Math.min(3, Math.max(0.3, engine.zoom + d)) }) }
function go(a: string) {
  let u = a.trim()
  if (!u) return
  if (!/^[a-z][a-z0-9+.-]*:/i.test(u)) u = /\s/.test(u) || !u.includes('.') ? 'https://duckduckgo.com/?q=' + encodeURIComponent(u) : 'https://' + u
  typed.value = u
  act({ c: 'go', url: u })
  focusPage()
}
let ro: ResizeObserver | null = null
let sizeTimer: ReturnType<typeof setTimeout> | undefined
function measure() {
  clearTimeout(sizeTimer)
  sizeTimer = setTimeout(() => { const r = stage.value?.getBoundingClientRect(); if (r) fitScreen(Math.floor(r.width), Math.floor(r.height)) }, 250)
}
watch(() => engine.status, (s) => { if (s === 'live') measure() })
onMounted(() => {
  void start(screen.value!)
  ro = new ResizeObserver(measure)
  if (stage.value) ro.observe(stage.value)
})
onBeforeUnmount(() => { ro?.disconnect(); clearTimeout(sizeTimer); stop() })
</script>

<style scoped>
/* the page fills the whole content area: the buttons are in the title bar, the address bar is the first row, the web page gets everything else */
.ph-browse { display: flex; flex-direction: column; padding: 0; gap: 0; }
/* the frame fills the page; the page area (banners, the rendered page) is a column inside it */
.ph-bf { flex: 1 1 auto; min-height: 0; display: flex; flex-direction: column; }
.ph-bf__page { flex: 1 1 auto; min-height: 0; display: flex; flex-direction: column; height: 100%; }
.ph-url { flex: 1 1 0; min-width: 0; }
.ph-url :deep(.q-field__control) { border-radius: 0; }
.ph-sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
.ph-note { flex: none; border-radius: 0; background: var(--ph-surface-2); color: var(--ph-text); border-bottom: 1px solid var(--ph-divider); }
.ph-note.is-error { border-color: var(--ph-negative); }
.ph-note.is-error :deep(.q-icon) { color: var(--ph-negative); }
.ph-stage { flex: 1 1 auto; min-height: 0; position: relative; overflow: hidden; display: flex; align-items: center; justify-content: center; background: var(--ph-bg); }
.ph-dialog { width: min(92vw, 420px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); }
.ph-tablist { list-style: none; padding: 0; margin: 0 0 var(--ph-s2); }
.ph-trow { display: flex; align-items: center; gap: var(--ph-s2); min-height: var(--ph-tap); border-bottom: 1px solid var(--ph-divider); }
.ph-ttext { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.ph-ttitle, .ph-turl { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-screen { position: absolute; inset: 0; background: #000; }
.ph-connecting { position: absolute; inset: 0; display: flex; gap: var(--ph-s2); align-items: center; justify-content: center; background: #000; color: #fff; }
</style>

<style>
/* the More menu is drawn outside the page (a popup): its Zoom row on one line */
.ph-bf-zoom { display: flex; align-items: center; gap: 2px; white-space: nowrap; }
</style>
