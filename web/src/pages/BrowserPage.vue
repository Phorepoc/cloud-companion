<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-browse">
    <div class="ph-tabs" data-test="tab-strip" role="tablist" aria-label="Open tabs">
      <div v-for="t in state.tabs" :key="t.v" class="ph-tab" :class="{ 'is-active': t.v === state.activeView }" role="presentation">
        <button type="button" class="ph-tab-main" role="tab" :aria-selected="t.v === state.activeView" :data-test="'tab-' + t.v" @click="api.tab('switch', t.v)">
          {{ t.title || t.url || 'New tab' }}
        </button>
        <button type="button" class="ph-tab-close" :data-test="'tab-close-' + t.v" :aria-label="'Close ' + (t.title || 'tab')" @click="api.tab('close', t.v)">
          <q-icon name="sym_o_close" size="20px" aria-hidden="true" />
        </button>
      </div>
      <q-btn flat round icon="sym_o_add" aria-label="New tab" :disable="state.tabs.length >= 3 || !state.features.includes('tabs')" data-test="tab-new" @click="api.tab('new')">
        <q-tooltip>New tab (up to 3)</q-tooltip>
      </q-btn>
      <q-btn flat round icon="sym_o_devices" aria-label="Tabs on your other devices" data-test="other-devices" @click="openOthers">
        <q-tooltip>Tabs on your other devices</q-tooltip>
      </q-btn>
    </div>

    <div class="ph-address" role="search">
      <q-btn flat round icon="sym_o_arrow_back" aria-label="Back" :disable="!state.page.back" data-test="back" @click="api.action('back')" />
      <q-btn flat round icon="sym_o_arrow_forward" aria-label="Forward" :disable="!state.page.fwd" data-test="forward" @click="api.action('forward')" />
      <q-btn flat round icon="sym_o_refresh" aria-label="Reload" data-test="reload" @click="api.action('reload')" />
      <q-input v-model="urlInput" outlined class="col ph-url" placeholder="Search or enter a web address" aria-label="Address" inputmode="url"
               autocomplete="off" data-test="url" @keyup.enter="go" @focus="editing = true" @blur="editing = false">
        <template #append><q-spinner v-if="state.loading" size="20px" color="primary" aria-label="Loading" data-test="spinner" /></template>
      </q-input>
    </div>
    <div class="ph-page-title ph-small ph-muted" data-test="title">{{ state.page.title }}</div>

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

    <div class="viewport">
      <canvas ref="cv" data-test="canvas" class="ph-canvas" aria-label="Web page" />
      <textarea v-if="overlayRect && state.focus.kind === 'textarea'" ref="ov" v-model="ovValue" class="overlay" :style="overlayRect"
                aria-label="Text field" data-test="keyboard-overlay" @input="onType" @keydown.enter="onEnter" />
      <input v-else-if="overlayRect" ref="ov" v-model="ovValue" :type="inputType" class="overlay" :style="overlayRect"
             aria-label="Text field" :inputmode="state.focus.im || undefined" data-test="keyboard-overlay" @input="onType" @keydown.enter="onEnter" />
    </div>
    <div class="ph-zoom" role="group" aria-label="Zoom">
      <q-btn flat round icon="sym_o_zoom_out" aria-label="Zoom out" data-test="zoom-out" @click="renderer && renderer.zoomBy(1 / 1.25)" />
      <span class="ph-small ph-muted" aria-live="polite">{{ state.page.zoom || 100 }}%</span>
      <q-btn flat round icon="sym_o_zoom_in" aria-label="Zoom in" data-test="zoom-in" @click="renderer && renderer.zoomBy(1.25)" />
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

<script setup>
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { state, api, connect, creds, openInNewTab } from '../cloud.js'
import EmptyState from '../components/EmptyState.vue'
import { Renderer } from '../renderer.js'

const cv = ref(null)
const ov = ref(null)
const urlInput = ref(state.page.url)
const editing = ref(false)
const ovValue = ref('')
const dlgText = ref('')
const dlgPass = ref('')
const selOpen = ref(false)
const selRegion = ref(null)
const selIndex = ref(0)
let renderer = null
let typeTimer = 0

const dlg = computed(() => state.dialogs[0] || null)
const dlgTitle = computed(() => ({ alert: 'This page says', confirm: 'This page asks', prompt: 'This page asks', beforeunload: 'Leave this page?', auth: 'Sign in to continue', cert: 'This connection is not secure' })[dlg.value?.kind] || 'Message')
watch(dlg, (d) => { dlgText.value = d ? d.def || '' : ''; dlgPass.value = '' })

function reply(ok) {
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
function onEnter(e) {
  if (state.focus.kind === 'textarea') return
  e.preventDefault()
  clearTimeout(typeTimer)
  api.text(state.focus.id, ovValue.value, true)
  api.key('Enter')
}

const selOptions = computed(() => (selRegion.value ? selRegion.value.opts.map((o, i) => ({ label: o.l || '(empty)', value: i })) : []))
function pickSelect() {
  api.select(selRegion.value.id, selIndex.value)
}

onMounted(async () => {
  renderer = new Renderer(cv.value)
  renderer.onSelect = (r) => {
    selRegion.value = r
    selIndex.value = Math.max(0, r.opts.findIndex((o) => o.s))
    selOpen.value = true
  }
  renderer.attach()
  // test/debug hook: screen position (client coordinates) of a region by label
  window.__phe = Object.assign(window.__phe || {}, {
    renderer,
    // client coordinates of a region's centre; match by label text (or name / placeholder), optionally restricted to a kind
    regionClientPos(q, kind) {
      const r = state.regions.find((g) => (!kind || g.kind === kind) && (!q || (g.label || '').includes(q) || g.name === q || g.ph === q))
      if (!r) return null
      const sr = renderer.screenRect(r)
      const b = cv.value.getBoundingClientRect()
      const k = b.width / 360
      return { x: b.left + (sr.x + sr.w / 2) * k, y: b.top + (sr.y + sr.h / 2) * k, scrollY: renderer.view.scrollY, zoom: renderer.view.zoom }
    },
  })
  if (creds.value && state.status === 'offline') {
    try { await connect() } catch (e) { /* shown by the status banner */ }
  }
})
onBeforeUnmount(() => renderer && renderer.detach())

// ---- tabs on other devices ----
const othersOpen = ref(false)
const sendUrl = reactive({})
let poll = 0
function openOthers() { othersOpen.value = true; api.sessions(); clearInterval(poll); poll = setInterval(() => api.sessions(), 4000) }
function stopPoll() { clearInterval(poll) }
function closeRemote(s, t) {
  if (s.self) api.tab('close', t.v)
  else api.remote({ sid: s.sid, action: 'close', view: t.v })
  setTimeout(() => api.sessions(), 500)
}
function sendTo(s) {
  let u = (sendUrl[s.sid] || '').trim()
  if (!u) return
  if (!/^[a-z][a-z0-9+.-]*:/i.test(u)) u = 'https://' + u
  api.remote({ sid: s.sid, action: 'open', url: u })
  sendUrl[s.sid] = ''
  setTimeout(() => api.sessions(), 1500)
}
async function openHere(t) { othersOpen.value = false; await openInNewTab(t.url) }
onBeforeUnmount(() => clearInterval(poll))
</script>

<style scoped>
.ph-browse { display: flex; flex-direction: column; align-items: center; gap: var(--ph-s2); padding: var(--ph-s3); }
.ph-tabs, .ph-address { width: 100%; max-width: 720px; display: flex; align-items: center; gap: var(--ph-s1); }
.ph-address { gap: var(--ph-s1); }
.ph-url { flex: 1 1 0; min-width: 0; }
.ph-tabs { overflow-x: auto; }
.ph-tab { flex: 1 1 0; min-width: 0; display: flex; align-items: center; height: var(--ph-tap); border: 1px solid var(--ph-divider); border-radius: var(--ph-r-control); background: var(--ph-surface); }
.ph-tab.is-active { border-color: var(--ph-accent); background: var(--ph-accent-soft); }
.ph-tab-main { flex: 1; min-width: 0; height: 100%; padding: 0 var(--ph-s2) 0 var(--ph-s3); background: none; border: 0; text-align: left; color: var(--ph-text); font: inherit; font-size: var(--ph-text-small); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; cursor: pointer; }
.ph-tab.is-active .ph-tab-main { font-weight: 600; }
.ph-tab-close { flex: none; width: var(--ph-tap); height: var(--ph-tap); display: grid; place-items: center; background: none; border: 0; color: var(--ph-text-muted); cursor: pointer; border-radius: var(--ph-r-control); }
.ph-page-title { max-width: 720px; width: 100%; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-height: 1.5em; }
.ph-note { width: 100%; max-width: 720px; background: var(--ph-surface-2); color: var(--ph-text); border: 1px solid var(--ph-divider); }
.ph-note.is-error { border-color: var(--ph-negative); }
.ph-note.is-error :deep(.q-icon) { color: var(--ph-negative); }
.viewport { position: relative; width: 360px; height: 640px; max-width: 100%; }
.ph-canvas { width: 360px; height: 640px; display: block; touch-action: none; background: #fff; border: 1px solid var(--ph-divider); border-radius: var(--ph-r-control); box-shadow: var(--ph-elev-1); }
.ph-zoom { display: flex; align-items: center; gap: var(--ph-s2); }
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
