<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="column items-center q-pa-sm">
    <div class="row items-center no-wrap q-gutter-xs" style="width:100%;max-width:720px">
      <q-btn flat round dense icon="arrow_back" :disable="!state.page.back" data-test="back" @click="api.action('back')" />
      <q-btn flat round dense icon="arrow_forward" :disable="!state.page.fwd" data-test="forward" @click="api.action('forward')" />
      <q-btn flat round dense icon="refresh" data-test="reload" @click="api.action('reload')" />
      <q-btn flat round dense icon="zoom_out" data-test="zoom-out" @click="renderer && renderer.zoomBy(1 / 1.25)" />
      <q-btn flat round dense icon="zoom_in" data-test="zoom-in" @click="renderer && renderer.zoomBy(1.25)" />
      <q-input v-model="urlInput" dense outlined class="col" placeholder="Search or enter a web address" data-test="url"
               @keyup.enter="go" @focus="editing = true" @blur="editing = false" />
      <q-btn flat round dense icon="arrow_forward_ios" data-test="go" @click="go" />
      <q-spinner v-if="state.loading" size="22px" color="primary" data-test="spinner" />
    </div>
    <div class="text-caption text-grey-8 q-my-xs ellipsis" style="max-width:720px" data-test="title">{{ state.page.title }}</div>
    <q-banner v-if="state.error" dense class="bg-red-1 text-red-9 q-mb-xs" style="max-width:720px">
      {{ state.error }}
      <template #action><q-btn flat dense label="dismiss" @click="state.error = ''" /></template>
    </q-banner>
    <q-banner v-if="state.downloadNote" dense class="bg-amber-1 q-mb-xs" style="max-width:720px">
      {{ state.downloadNote }}
      <template #action><q-btn flat dense label="ok" @click="state.downloadNote = ''" /></template>
    </q-banner>

    <div class="viewport" :style="{ width: '360px', height: '640px' }">
      <canvas ref="cv" data-test="canvas" style="width:360px;height:640px;touch-action:none;display:block;border:1px solid #cfd8dc;background:#fff" />
      <textarea v-if="overlayRect && state.focus.kind === 'textarea'" ref="ov" v-model="ovValue" class="overlay" :style="overlayRect"
                data-test="keyboard-overlay" @input="onType" @keydown.enter="onEnter" />
      <input v-else-if="overlayRect" ref="ov" v-model="ovValue" :type="inputType" class="overlay" :style="overlayRect"
             :inputmode="state.focus.im || undefined" data-test="keyboard-overlay" @input="onType" @keydown.enter="onEnter" />
    </div>
    <div v-if="state.page.trunc" class="text-caption text-grey-7 q-mt-xs">The page continues on the desktop version.</div>

    <q-dialog :model-value="!!dlg" persistent>
      <q-card v-if="dlg" style="min-width:300px" data-test="dialog">
        <q-card-section>
          <div class="text-h6">{{ dlgTitle }}</div>
          <div class="q-mt-sm" style="white-space:pre-wrap">{{ dlg.message }}</div>
          <q-input v-if="dlg.kind === 'prompt'" v-model="dlgText" autofocus dense class="q-mt-sm" data-test="dialog-text" />
          <template v-if="dlg.kind === 'auth'">
            <q-input v-model="dlgText" label="User name" autofocus dense class="q-mt-sm" data-test="dialog-user" />
            <q-input v-model="dlgPass" label="Password" type="password" dense class="q-mt-sm" data-test="dialog-pass" />
          </template>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn v-if="dlg.kind !== 'alert'" flat :label="dlg.kind === 'cert' ? 'Stop' : 'Cancel'" data-test="dialog-cancel" @click="reply(false)" />
          <q-btn flat color="primary" :label="dlg.kind === 'cert' ? 'Continue anyway' : 'OK'" data-test="dialog-ok" @click="reply(true)" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog v-model="selOpen">
      <q-card v-if="selRegion" style="min-width:280px" data-test="select-dialog">
        <q-card-section class="q-pb-none"><div class="text-h6">{{ selRegion.label || 'Choose' }}</div></q-card-section>
        <q-card-section>
          <q-option-group v-model="selIndex" type="radio" :options="selOptions" />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat label="Cancel" v-close-popup />
          <q-btn flat color="primary" label="OK" v-close-popup data-test="select-ok" @click="pickSelect" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { state, api, connect, creds } from '../cloud.js'
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
const dlgTitle = computed(() => ({ alert: 'Alert', confirm: 'Confirm', prompt: 'Prompt', beforeunload: 'Leave this page?', auth: 'Sign in', cert: 'Certificate error' })[dlg.value?.kind] || 'Message')
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
</script>

<style scoped>
.viewport { position: relative; }
.overlay { position: absolute; box-sizing: border-box; font: 15px sans-serif; border: 2px solid #1976d2; border-radius: 3px; padding: 2px 6px; background: #fff; }
</style>
