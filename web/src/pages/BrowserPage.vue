<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-browse" :class="{ 'is-phone': phoneSize }">
    <!-- the buttons live in the title bar (the window's, or the app's top bar): the page keeps its room for the page -->
    <Teleport v-if="titleActions" :to="titleActions">
      <div class="ph-tb" role="toolbar" aria-label="Browser controls">
        <q-btn flat round dense icon="sym_o_arrow_back" aria-label="Back" :disable="!state.page.back" data-test="back" @click="api.action('back')"><q-tooltip>Back</q-tooltip></q-btn>
        <q-btn flat round dense icon="sym_o_arrow_forward" aria-label="Forward" :disable="!state.page.fwd" data-test="forward" @click="api.action('forward')"><q-tooltip>Forward</q-tooltip></q-btn>
        <q-btn flat round dense icon="sym_o_refresh" aria-label="Reload" data-test="reload" @click="api.action('reload')"><q-tooltip>Reload</q-tooltip></q-btn>
        <q-btn flat round dense icon="sym_o_add" aria-label="New tab" :disable="state.tabs.length >= 3 || !state.features.includes('tabs')" data-test="tab-new" @click="api.tab('new')"><q-tooltip>New tab (up to 3)</q-tooltip></q-btn>
        <q-btn flat round dense icon="sym_o_devices" aria-label="Tabs on your other devices" data-test="other-devices" @click="openOthers"><q-tooltip>Tabs on your other devices</q-tooltip></q-btn>
        <q-btn v-if="!narrow" flat round dense icon="sym_o_zoom_out" aria-label="Zoom out" data-test="zoom-out" @click="renderer && renderer.zoomBy(1 / 1.25)" />
        <span v-if="!narrow" class="ph-tb-zoom ph-small" aria-live="polite" data-test="zoom-pct">{{ state.page.zoom || 100 }}%</span>
        <q-btn v-if="!narrow" flat round dense icon="sym_o_zoom_in" aria-label="Zoom in" data-test="zoom-in" @click="renderer && renderer.zoomBy(1.25)" />
        <q-btn v-if="canFull && !narrow" flat round dense :icon="phoneSize ? 'sym_o_open_in_full' : 'sym_o_smartphone'" :aria-pressed="String(phoneSize)"
               :aria-label="phoneSize ? 'Use the whole window' : 'Phone size'" data-test="phone-size" @click="togglePhone">
          <q-tooltip>{{ phoneSize ? 'Use the whole window' : 'Phone size (360 x 640)' }}</q-tooltip>
        </q-btn>
      </div>
    </Teleport>

    <div class="ph-address" role="search">
      <q-input v-model="urlInput" outlined dense square class="col ph-url" placeholder="Search or enter a web address" aria-label="Address" inputmode="url"
               autocomplete="off" data-test="url" @keyup.enter="go" @focus="editing = true" @blur="editing = false">
        <template #append><q-spinner v-if="state.loading" size="20px" color="primary" aria-label="Loading" data-test="spinner" /></template>
      </q-input>
    </div>
    <!-- the tabs: only when there is more than one (the new-tab button is in the title bar) -->
    <div v-if="state.tabs.length > 1" class="ph-tabs" data-test="tab-strip" role="tablist" aria-label="Open tabs">
      <div v-for="t in state.tabs" :key="t.v" class="ph-tab" :class="{ 'is-active': t.v === state.activeView }" role="presentation">
        <button type="button" class="ph-tab-main" role="tab" :aria-selected="t.v === state.activeView" :data-test="'tab-' + t.v" @click="api.tab('switch', t.v)">
          {{ t.title || t.url || 'New tab' }}
        </button>
        <button type="button" class="ph-tab-close" :data-test="'tab-close-' + t.v" :aria-label="'Close ' + (t.title || 'tab')" @click="api.tab('close', t.v)">
          <q-icon name="sym_o_close" size="20px" aria-hidden="true" />
        </button>
      </div>
    </div>
    <span class="ph-sr" data-test="title">{{ state.page.title }}</span>

    <q-banner v-if="state.error" class="ph-note is-error" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ state.error }}
      <template #action><q-btn flat label="Dismiss" @click="state.error = ''" /></template>
    </q-banner>
    <q-banner v-if="state.downloadNote" class="ph-note" rounded role="status">
      <template #avatar><q-icon name="sym_o_download" aria-hidden="true" /></template>
      {{ state.downloadNote }}
      <template #action><q-btn flat label="OK" @click="state.downloadNote = ''" /></template>
    </q-banner>

    <div ref="stage" class="ph-stage" data-test="stage">
      <div class="viewport" :style="vpStyle">
        <canvas ref="cv" data-test="canvas" class="ph-canvas" aria-label="Web page" />
        <textarea v-if="overlayRect && state.focus.kind === 'textarea'" ref="ov" v-model="ovValue" class="overlay" :style="overlayRect"
                  aria-label="Text field" data-test="keyboard-overlay" @input="onType" @keydown.enter="onEnter" />
        <input v-else-if="overlayRect" ref="ov" v-model="ovValue" :type="inputType" class="overlay" :style="overlayRect"
               aria-label="Text field" :inputmode="inputMode" data-test="keyboard-overlay" @input="onType" @keydown.enter="onEnter" />
      </div>
    </div>
    <div v-if="state.page.trunc" class="ph-small ph-muted">The rest of this page is not shown. Open it on a larger screen to read on.</div>

    <q-dialog :model-value="!!dlg" persistent>
      <q-card v-if="dlg" class="ph-dialog" data-test="dialog" role="alertdialog" :aria-label="dlgTitle">
        <q-card-section>
          <h2>{{ dlgTitle }}</h2>
          <p class="ph-dialog-text">{{ dlg.message }}</p>
          <q-input v-if="dlg.kind === 'prompt'" v-model="dlgText" autofocus outlined aria-label="Your answer" data-test="dialog-text" />
          <template v-if="dlg.kind === 'auth'">
            <q-input v-model="dlgText" label="User name" autofocus outlined class="q-mb-sm" autocomplete="username" data-test="dialog-user" />
            <q-input v-model="dlgPass" label="Password" type="password" outlined autocomplete="current-password" data-test="dialog-pass" />
          </template>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn v-if="dlg.kind !== 'alert'" flat :label="dlg.kind === 'cert' ? 'Go back' : 'Cancel'" data-test="dialog-cancel" @click="reply(false)" />
          <q-btn unelevated color="primary" :label="dlg.kind === 'cert' ? 'Continue anyway' : dlg.kind === 'auth' ? 'Sign in' : 'OK'" data-test="dialog-ok" @click="reply(true)" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog v-model="selOpen">
      <q-card v-if="selRegion" class="ph-dialog" data-test="select-dialog">
        <q-card-section class="q-pb-none"><h2>{{ selRegion.label || 'Choose an option' }}</h2></q-card-section>
        <q-card-section>
          <q-option-group v-model="selIndex" type="radio" :options="selOptions" />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat label="Cancel" v-close-popup />
          <q-btn unelevated color="primary" label="OK" v-close-popup data-test="select-ok" @click="pickSelect" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog v-model="othersOpen" @hide="stopPoll">
      <q-card class="ph-dialog" data-test="others-dialog">
        <q-card-section>
          <h2>Tabs on your devices</h2>
          <p class="ph-muted ph-small">Browser tabs that are open right now on the devices that are connected. Closing a tab here closes it on that device.</p>
          <EmptyState v-if="!state.sessions.length" state="loading" title="Looking for devices" text="" />
          <section v-for="s in state.sessions" :key="s.sid" class="ph-dev" :data-test="'dev-' + s.sid">
            <h3>{{ s.name || 'Device' }} <q-badge v-if="s.self" color="positive" text-color="white" label="This browser" /></h3>
            <p v-if="!s.tabs || !s.tabs.length" class="ph-muted ph-small">No tabs open.</p>
            <ul v-else class="ph-tablist">
              <li v-for="t in s.tabs" :key="t.v" class="ph-trow" :data-test="'dev-tab-' + s.sid + '-' + t.v">
                <span class="ph-ttext"><span class="ph-ttitle">{{ t.title || t.url || 'New tab' }}</span><span class="ph-small ph-muted ph-turl">{{ t.url }}</span></span>
                <q-btn v-if="!s.self && t.url" flat round icon="sym_o_open_in_new" aria-label="Open here" :data-test="'dev-open-' + s.sid + '-' + t.v" @click="openHere(t)">
                  <q-tooltip>Open here</q-tooltip>
                </q-btn>
                <q-btn flat round icon="sym_o_close" :aria-label="'Close ' + (t.title || 'tab')" :disable="s.self && s.tabs.length === 1 && false" :data-test="'dev-close-' + s.sid + '-' + t.v" @click="closeRemote(s, t)" />
              </li>
            </ul>
            <form v-if="!s.self" class="ph-send" @submit.prevent="sendTo(s)">
              <q-input v-model="sendUrl[s.sid]" dense outlined placeholder="Open a page on this device" aria-label="Web address to open on this device" :data-test="'dev-url-' + s.sid" />
              <q-btn flat color="primary" label="Open" type="submit" :data-test="'dev-send-' + s.sid" />
            </form>
          </section>
        </q-card-section>
        <q-card-actions align="right"><q-btn flat label="Close" v-close-popup /><q-btn flat icon="sym_o_refresh" label="Refresh" @click="api.sessions()" /></q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount, nextTick, type HTMLAttributes } from 'vue'
import { state, api, connect, creds, openInNewTab } from '../cloud'
import type { Region, SessionInfo, TabInfo } from '../proto'
import EmptyState from '../components/EmptyState.vue'
import { Renderer } from '../renderer'
import { useTitleActions } from '../components/titlebar'
import { useQuasar } from 'quasar'

const titleActions = useTitleActions()
// on a phone-width screen the bar has room for the essentials only (pinch zooms, and the page already is phone sized)
const $q = useQuasar()
const narrow = computed(() => $q.screen.lt.sm)
const stage = ref<HTMLElement | null>(null)
const cv = ref<HTMLCanvasElement | null>(null)
const ov = ref<HTMLInputElement | null>(null)
const urlInput = ref(state.page.url)
const editing = ref(false)
const ovValue = ref('')
const dlgText = ref('')
const dlgPass = ref('')
const selOpen = ref(false)
const selRegion = ref<Region | null>(null)
const selIndex = ref(0)
let renderer: Renderer | null = null

// ---- the size of the remote window ----
// By default the cloud lays the page out at the size of this window (no phone-sized column, no padding); "Phone size" goes back to 360 x 640 and back again.
// A resize reaches the cloud (debounced), which lays the page out again and sends tiles for the new size.
const wantPhone = ref(localStorage.getItem('phecloud-browser-phone') === '1')
const canFull = computed(() => state.features.includes('viewport'))
const phoneSize = computed(() => wantPhone.value || !canFull.value)
const vpStyle = computed(() => (phoneSize.value
  ? { width: '360px', height: '640px', maxWidth: '100%', maxHeight: '100%', position: 'relative' as const }
  : { width: state.viewport.w + 'px', height: state.viewport.h + 'px', position: 'absolute' as const, left: '0', top: '0' }))
let resizeObs: ResizeObserver | null = null
let vpTimer: ReturnType<typeof setTimeout> | undefined
function applySize(now = false) {
  if (!stage.value) return
  const r = stage.value.getBoundingClientRect()
  const w = phoneSize.value ? 360 : Math.max(240, Math.floor(r.width))
  const h = phoneSize.value ? 640 : Math.max(240, Math.floor(r.height))
  const same = state.viewport.w === w && state.viewport.h === h && state.viewport.phone === phoneSize.value
  state.viewport = { w, h, phone: phoneSize.value }
  renderer?.setSize(w, h)
  if (same) return
  clearTimeout(vpTimer)
  const send = () => { if (canFull.value && state.page.pid !== 0) api.viewport(w, h) }
  if (now) send()
  else vpTimer = setTimeout(send, 250)
}
function togglePhone() {
  wantPhone.value = !wantPhone.value
  localStorage.setItem('phecloud-browser-phone', wantPhone.value ? '1' : '0')
  nextTick(() => applySize(true))
}
watch(canFull, () => nextTick(() => applySize(true)))
// the page's keyboard hint for the focused field (text, numeric, email…)
const inputMode = computed(() => (state.focus.im || undefined) as HTMLAttributes['inputmode'])
let typeTimer: ReturnType<typeof setTimeout> | undefined

const dlg = computed(() => state.dialogs[0] || null)
const dlgTitle = computed(() => ({ alert: 'This page says', confirm: 'This page asks', prompt: 'This page asks', beforeunload: 'Leave this page?', auth: 'Sign in to continue', cert: 'This connection is not secure' })[dlg.value?.kind] || 'Message')
watch(dlg, (d) => { dlgText.value = d ? d.def || '' : ''; dlgPass.value = '' })

function reply(ok: boolean) {
  const d = dlg.value
  if (!d) return
  api.answer(d, ok, d.kind === 'prompt' || d.kind === 'auth' ? dlgText.value : '', d.kind === 'auth' ? dlgPass.value : '')
}

watch(() => state.page.url, (u) => { if (!editing.value) urlInput.value = u })

function go() {
  let u = urlInput.value.trim()
  if (!u) return
  if (!/^[a-z][a-z0-9+.-]*:/i.test(u)) u = /\s/.test(u) || !u.includes('.') ? 'https://duckduckgo.com/?q=' + encodeURIComponent(u) : 'https://' + u
  if (state.page.pid === 0) api.open(u)
  else api.navigate(u)
}

// the keyboard overlay: a real <input> over the focused field
const overlayRect = computed(() => {
  const f = state.focus
  if (f.kind !== 'text' && f.kind !== 'textarea') return null
  const r = state.regions.find((q) => q.id === f.id)
  void renderer?.view.scrollY
  const sr = r && renderer ? renderer.screenRect(r) : null
  if (!sr) return { left: '8px', bottom: '8px', width: '344px', height: '34px' }
  return { left: sr.x + 'px', top: sr.y + 'px', width: Math.max(sr.w, 60) + 'px', height: Math.max(sr.h, 24) + 'px' }
})
const inputType = computed(() => (state.focus.kind === 'text' && ['password', 'email', 'url', 'tel', 'number', 'search'].includes(state.focus.type) ? state.focus.type : 'text'))
watch(() => [state.focus.kind, state.focus.id], async ([k]) => {
  if (k === 'text' || k === 'textarea') {
    ovValue.value = state.focus.value
    await nextTick()
    ov.value?.focus()
  }
})
function onType() {
  clearTimeout(typeTimer)
  typeTimer = setTimeout(() => api.text(state.focus.id, ovValue.value, true), 120)
}
function onEnter(e: KeyboardEvent) {
  if (state.focus.kind === 'textarea') return
  e.preventDefault()
  clearTimeout(typeTimer)
  api.text(state.focus.id, ovValue.value, true)
  api.key('Enter')
}

const selOptions = computed(() => (selRegion.value ? (selRegion.value.opts || []).map((o, i) => ({ label: o.l || '(empty)', value: i })) : []))
function pickSelect() {
  if (selRegion.value) api.select(selRegion.value.id, selIndex.value)
}

onMounted(async () => {
  // measure first: the canvas is made at the size the cloud will lay the page out at
  if (stage.value) {
    const r = stage.value.getBoundingClientRect()
    state.viewport = phoneSize.value ? { w: 360, h: 640, phone: true } : { w: Math.max(240, Math.floor(r.width)), h: Math.max(240, Math.floor(r.height)), phone: false }
  }
  renderer = new Renderer(cv.value!)
  renderer.onSelect = (r) => {
    selRegion.value = r
    selIndex.value = Math.max(0, (r.opts || []).findIndex((o) => o.s))
    selOpen.value = true
  }
  renderer.attach()
  applySize(true)
  resizeObs = new ResizeObserver(() => applySize())
  if (stage.value) resizeObs.observe(stage.value)
  // test/debug hook: screen position (client coordinates) of a region by label
  window.__phe = Object.assign(window.__phe || {}, {
    renderer,
    // client coordinates of a region's centre; match by label text (or name / placeholder), optionally restricted to a kind
    regionClientPos(q: string | null, kind?: string) {
      const r = state.regions.find((g) => (!kind || g.kind === kind) && (!q || (g.label || '').includes(q) || g.name === q || g.ph === q))
      if (!r || !renderer) return null
      const sr = renderer.screenRect(r)
      if (!sr) return null
      const b = cv.value!.getBoundingClientRect()
      const k = b.width / renderer.vw
      return { x: b.left + (sr.x + sr.w / 2) * k, y: b.top + (sr.y + sr.h / 2) * k, scrollY: renderer.view.scrollY, zoom: renderer.view.zoom }
    },
  })
  if (creds.value && state.status === 'offline') {
    try { await connect() } catch (e) { /* shown by the status banner */ }
  }
})
onBeforeUnmount(() => { resizeObs?.disconnect(); clearTimeout(vpTimer); if (renderer) renderer.detach() })

// ---- tabs on other devices ----
const othersOpen = ref(false)
const sendUrl = reactive<Record<number, string>>({})
let poll: ReturnType<typeof setInterval> | undefined
function openOthers() { othersOpen.value = true; api.sessions(); clearInterval(poll); poll = setInterval(() => api.sessions(), 4000) }
function stopPoll() { clearInterval(poll) }
function closeRemote(s: SessionInfo, t: TabInfo) {
  if (s.self) api.tab('close', t.v)
  else api.remote({ sid: s.sid, action: 'close', view: t.v })
  setTimeout(() => api.sessions(), 500)
}
function sendTo(s: SessionInfo) {
  let u = (sendUrl[s.sid] || '').trim()
  if (!u) return
  if (!/^[a-z][a-z0-9+.-]*:/i.test(u)) u = 'https://' + u
  api.remote({ sid: s.sid, action: 'open', url: u })
  sendUrl[s.sid] = ''
  setTimeout(() => api.sessions(), 1500)
}
async function openHere(t: TabInfo) { othersOpen.value = false; await openInNewTab(t.url || '') }
onBeforeUnmount(() => clearInterval(poll))
</script>

<style scoped>
/* the page fills the whole content area: the buttons are in the title bar, the address bar is the first row, the web page gets everything else */
.ph-browse { display: flex; flex-direction: column; padding: 0; gap: 0; }
.ph-address { display: flex; align-items: center; flex: none; padding: 0; }
.ph-url { flex: 1 1 0; min-width: 0; }
.ph-url :deep(.q-field__control) { border-radius: 0; }
.ph-tabs { flex: none; display: flex; align-items: center; gap: 0; overflow-x: auto; }
.ph-tab { flex: 1 1 0; min-width: 0; display: flex; align-items: center; height: 36px; border: 0; border-bottom: 2px solid transparent; background: var(--ph-surface); }
.ph-tab.is-active { border-bottom-color: var(--ph-accent); background: var(--ph-accent-soft); }
.ph-tab-main { flex: 1; min-width: 0; height: 100%; padding: 0 var(--ph-s2) 0 var(--ph-s3); background: none; border: 0; text-align: left; color: var(--ph-text); font: inherit; font-size: var(--ph-text-small); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: pointer; }
.ph-tab.is-active .ph-tab-main { font-weight: 600; }
.ph-tab-close { flex: none; width: 36px; height: 36px; display: grid; place-items: center; background: none; border: 0; color: var(--ph-text-muted); cursor: pointer; }
.ph-sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
.ph-note { flex: none; border-radius: 0; background: var(--ph-surface-2); color: var(--ph-text); border-bottom: 1px solid var(--ph-divider); }
.ph-note.is-error { border-color: var(--ph-negative); }
.ph-note.is-error :deep(.q-icon) { color: var(--ph-negative); }
.ph-stage { flex: 1 1 auto; min-height: 0; position: relative; overflow: hidden; display: flex; align-items: center; justify-content: center; background: var(--ph-bg); }
.ph-canvas { width: 100%; height: 100%; display: block; touch-action: none; background: #fff; }
.is-phone .ph-canvas { border: 1px solid var(--ph-divider); border-radius: var(--ph-r-control); box-shadow: var(--ph-elev-1); }
.ph-tb { display: flex; align-items: center; gap: 0; }
.ph-tb-zoom { min-width: 3.2em; text-align: center; }
.overlay { position: absolute; box-sizing: border-box; font: inherit; font-size: var(--ph-text-body); color: #1a1d21; border: 2px solid var(--ph-focus); border-radius: var(--ph-r-control); padding: 2px 8px; background: #fff; }
.ph-dialog { width: min(92vw, 420px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); }
.ph-dialog-text { white-space: pre-wrap; margin: 0 0 var(--ph-s3); overflow-wrap: anywhere; }
.ph-dev { margin-top: var(--ph-s4); }
.ph-dev h3 { margin-bottom: var(--ph-s2); }
.ph-tablist { list-style: none; padding: 0; margin: 0 0 var(--ph-s2); }
.ph-trow { display: flex; align-items: center; gap: var(--ph-s2); min-height: var(--ph-tap); border-bottom: 1px solid var(--ph-divider); }
.ph-ttext { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.ph-ttitle, .ph-turl { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-send { display: flex; gap: var(--ph-s2); align-items: center; }
.ph-send .q-field { flex: 1; }
</style>
