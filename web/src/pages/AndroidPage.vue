<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-android" :class="{ 'is-phone-mode': kind === 'phone' }">
    <!-- the controls live in the title bar (the window's, or the app's top bar); the screen gets everything else -->
    <Teleport v-if="titleActions && configured" :to="titleActions">
      <div class="ph-tb" role="toolbar" aria-label="Android controls">
        <span class="ph-conn" :class="'is-' + conn" role="status" data-test="android-state" :title="connText">
          <q-icon :name="conn === 'live' ? 'sym_o_cloud_done' : conn === 'connecting' || conn === 'starting' ? 'sym_o_sync' : 'sym_o_cloud_off'" size="20px" aria-hidden="true" />
          <span class="ph-conn-text">{{ connText }}</span>
        </span>
        <q-btn flat round dense icon="sym_o_arrow_back" aria-label="Back" data-test="android-back" @click="stream.key(4)"><q-tooltip>Back</q-tooltip></q-btn>
        <q-btn flat round dense icon="sym_o_home" aria-label="Home" data-test="android-home" @click="stream.key(3)"><q-tooltip>Home</q-tooltip></q-btn>
        <q-btn flat round dense icon="sym_o_crop_square" aria-label="Recent" data-test="android-recent" @click="stream.key(187)"><q-tooltip>Recent apps</q-tooltip></q-btn>
        <q-btn flat round dense :icon="sound ? 'sym_o_volume_up' : 'sym_o_volume_off'" :aria-label="sound ? 'Sound on' : 'Sound off'" :aria-pressed="String(sound)" data-test="android-sound" @click="toggleSound"><q-tooltip>{{ sound ? 'Sound on' : 'Sound off' }}</q-tooltip></q-btn>
        <template v-if="!narrow">
          <q-btn flat round dense icon="sym_o_content_copy" aria-label="Copy from phone" data-test="android-copy" @click="stream.copyFromPhone()"><q-tooltip>Copy from phone</q-tooltip></q-btn>
          <q-btn flat round dense icon="sym_o_content_paste" aria-label="Paste to phone" data-test="android-paste" @click="stream.pasteToPhone()"><q-tooltip>Paste to phone</q-tooltip></q-btn>
          <q-btn flat round dense icon="sym_o_download" aria-label="Install apps" data-test="android-install" @click="installOpen = true"><q-tooltip>Install apps</q-tooltip></q-btn>
          <q-btn flat round dense icon="sym_o_refresh" aria-label="Reconnect" :loading="busy" data-test="android-reconnect" @click="connect"><q-tooltip>Reconnect</q-tooltip></q-btn>
        </template>
        <!-- on a phone-width screen the bar has room for the essentials; the rest is one tap away -->
        <q-btn v-else flat round dense icon="sym_o_more_vert" aria-label="More" data-test="android-more">
          <q-menu>
            <q-list dense style="min-width: 190px">
              <q-item v-close-popup clickable data-test="android-copy" @click="stream.copyFromPhone()"><q-item-section avatar><q-icon name="sym_o_content_copy" /></q-item-section><q-item-section>Copy from phone</q-item-section></q-item>
              <q-item v-close-popup clickable data-test="android-paste" @click="stream.pasteToPhone()"><q-item-section avatar><q-icon name="sym_o_content_paste" /></q-item-section><q-item-section>Paste to phone</q-item-section></q-item>
              <q-item v-close-popup clickable data-test="android-install" @click="installOpen = true"><q-item-section avatar><q-icon name="sym_o_download" /></q-item-section><q-item-section>Install apps</q-item-section></q-item>
              <q-item v-close-popup clickable data-test="android-reconnect" @click="connect"><q-item-section avatar><q-icon name="sym_o_refresh" /></q-item-section><q-item-section>Reconnect</q-item-section></q-item>
            </q-list>
          </q-menu>
        </q-btn>
        <!-- Android has two modes, whatever the window: a phone (portrait, the phone launcher) and a desktop (landscape, windows and a taskbar) -->
        <span class="ph-seg" role="group" aria-label="Android mode">
          <q-btn flat dense no-caps icon="sym_o_smartphone" aria-label="Phone mode" :aria-pressed="String(kind === 'phone')" :class="{ 'is-on': kind === 'phone' }" data-test="android-mode-phone" @click="setMode('phone')"><q-tooltip>Phone mode</q-tooltip></q-btn>
          <q-btn flat dense no-caps icon="sym_o_desktop_windows" aria-label="Desktop mode" :aria-pressed="String(kind === 'desktop')" :class="{ 'is-on': kind === 'desktop' }" data-test="android-mode-desktop" @click="setMode('desktop')"><q-tooltip>Desktop mode</q-tooltip></q-btn>
        </span>
      </div>
    </Teleport>

    <h1 class="ph-sr">Android</h1>
    <p v-if="state.admin === false" class="ph-note" role="note">Only an admin device can open Android.</p>
    <p v-if="loaded && !configured" class="ph-note" role="note" data-test="android-off">
      <q-icon name="sym_o_info" size="20px" aria-hidden="true" /> Android is not set up on this server. See docs/android.md for the two extra containers it needs.
    </p>
    <q-banner v-if="problem" class="bg-negative text-white" role="alert" data-test="android-error">{{ problem }}</q-banner>

    <div v-show="configured" ref="stage" class="ph-stage" data-test="android-stage">
      <div class="ph-frame" :style="frameStyle">
        <div v-if="conn === 'starting'" class="ph-starting" role="status" data-test="android-starting">
          <q-spinner size="32px" color="white" aria-hidden="true" />
          <span>Starting Android…</span>
          <span class="ph-small">It was switched off to save resources. This takes about half a minute.</span>
        </div>
        <canvas ref="cv" width="360" height="640" tabindex="0" role="application" data-test="android-screen"
                aria-label="Android screen: click or touch to tap, scroll with the wheel, type to enter text"></canvas>
      </div>
      <p v-if="configured && (note || statsText)" class="ph-line ph-small" aria-live="polite" data-test="android-note">{{ note || statsText }}</p>
    </div>

    <q-dialog v-model="installOpen">
      <q-card class="ph-dialog" data-test="android-install-dialog">
        <q-card-section class="ph-form">
          <h2 class="ph-h3">Install apps</h2>
          <p class="ph-muted">Apps run on your server and keep running when you close this page. Only apps that include x86_64 code install.</p>
          <input ref="file" type="file" accept=".apk,application/vnd.android.package-archive" hidden data-test="android-apk" @change="pickApk" />
          <q-btn outline color="primary" no-caps icon="sym_o_upload_file" label="Install an APK file" @click="file?.click()" />
          <q-input v-model="pkg" outlined dense label="F-Droid package name" hint="For example org.fossify.notes" autocomplete="off" data-test="android-pkg" @keyup.enter="fdroid" />
          <q-btn unelevated color="primary" no-caps icon="sym_o_download" label="Install from F-Droid" :disable="!pkg.trim()" data-test="android-fdroid" @click="fdroid" />
          <p class="ph-small" aria-live="polite" data-test="android-install-note">{{ note }}</p>
        </q-card-section>
        <q-card-actions align="right"><q-btn flat no-caps label="Close" @click="installOpen = false" /></q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, shallowRef, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { useQuasar } from 'quasar'
import { apiCall, state } from '../cloud'
import { useTitleActions } from '../components/titlebar'
import { AndroidStream, type StreamState, type StreamStats } from '../android/stream.js'

const route = useRoute()
const titleActions = useTitleActions()
const $q = useQuasar()
const narrow = computed(() => $q.screen.lt.sm)
const cv = ref<HTMLCanvasElement | null>(null), file = ref<HTMLInputElement | null>(null), stage = ref<HTMLElement | null>(null)
const configured = ref(false), loaded = ref(false), busy = ref(false), problem = ref(''), conn = ref<StreamState>('idle'), connMsg = ref('')
const w = ref(360), h = ref(640), note = ref(''), stats = ref<StreamStats | null>(null), sound = ref(false), installOpen = ref(false), pkg = ref('')
const stream = shallowRef(new AndroidStream(document.createElement('canvas'), { base: '/android/' })) // replaced once the canvas exists

// ---- the two Android modes ----
// "auto" picks by the size of the window the first time; the buttons choose, and the choice is kept for this browser. The display on the server follows the choice AND the window's size.
type Mode = 'auto' | 'phone' | 'desktop'
const saved = ((): Mode => { try { const v = localStorage.getItem('phecloud-android-mode'); return v === 'phone' || v === 'desktop' ? v : 'auto' } catch { return 'auto' } })()
const mode = ref<Mode>(saved)
const stageSize = ref({ w: 0, h: 0 })
/** What is on screen now: the mode, or (auto) what suits the window. */
const kind = computed<'phone' | 'desktop'>(() => (mode.value !== 'auto' ? mode.value : stageSize.value.w >= 700 && stageSize.value.h >= 420 ? 'desktop' : 'phone'))
const wire = (k: string) => (k === 'desktop' ? 'large' : 'phone')
function setMode(m: 'phone' | 'desktop') {
  mode.value = m
  try { localStorage.setItem('phecloud-android-mode', m) } catch { /* private window */ }
  stream.value.setKind(wire(m))
}

// the screen fills the window: the stream's own aspect ratio, as large as the stage allows (in desktop mode the display was made for this window, so that is all of it)
const frameStyle = computed(() => {
  const sw = stageSize.value.w, sh = stageSize.value.h
  if (!sw || !sh) return {}
  const k = Math.min(sw / w.value, sh / h.value)
  return { width: Math.floor(w.value * k) + 'px', height: Math.floor(h.value * k) + 'px' }
})

const connText = computed(() => ({ idle: 'Not connected', connecting: 'Connecting', starting: 'Starting Android…', live: 'Connected', closed: connMsg.value || 'Disconnected', error: connMsg.value || 'Problem' }[conn.value]))
const statsText = computed(() => {
  const s = stats.value
  if (conn.value !== 'live' || !s) return ''
  const bits = [s.w + ' x ' + s.h, s.fps.toFixed(0) + ' fps', s.kbps.toFixed(0) + ' kbit/s']
  if (s.i2f_n) bits.push('input to picture ' + s.i2f_ms.toFixed(0) + ' ms')
  bits.push('round trip ' + s.rtt.toFixed(0) + ' ms')
  return bits.join(' · ')
})

async function connect() {
  busy.value = true; problem.value = ''; note.value = ''
  try {
    const j = await apiCall('GET', '/phe/android/ticket') // a fresh two-minute ticket, from this signed-in device
    configured.value = j.configured
    if (j.configured) {
      const q = route.query as Record<string, string | undefined> // single values: ?codec=vp8&fps=30
      stream.value.connect(j.ticket, { codec: q.codec, size: q.size, fps: q.fps, kbps: q.kbps, kind: q.kind || (mode.value === 'auto' ? undefined : wire(mode.value)), audio: q.audio !== '0' })
    }
  } catch (e) { problem.value = (e as Error).message } finally { busy.value = false; loaded.value = true }
}
function toggleSound() { sound.value = !sound.value; stream.value.setSound(sound.value) }
async function pickApk(e: Event) { const input = e.target as HTMLInputElement; const f = input.files?.[0]; if (f) await stream.value.installApk(f); input.value = '' }
async function fdroid() { if (pkg.value.trim()) await stream.value.installFdroid(pkg.value.trim()) }

let ro: ResizeObserver | null = null
function measure() { const r = stage.value?.getBoundingClientRect(); if (r) stageSize.value = { w: Math.floor(r.width), h: Math.floor(r.height) } }
onMounted(() => {
  measure()
  stream.value.dispose()
  stream.value = new AndroidStream(cv.value!, {
    base: '/android/',
    viewportEl: stage.value,
    onState: (s, m) => { conn.value = s; connMsg.value = m; if (s === 'error') problem.value = m },
    onStats: (m) => { stats.value = m },
    onNote: (t) => { note.value = t },
    onSize: (a, b) => { w.value = a; h.value = b },
  })
  ro = new ResizeObserver(measure)
  if (stage.value) ro.observe(stage.value)
  connect()
})
onBeforeUnmount(() => { ro?.disconnect(); stream.value.dispose() })
</script>

<style scoped>
/* the screen fills the content area; no padding, no heading row, the controls are in the title bar */
.ph-android { display: flex; flex-direction: column; padding: 0; }
.ph-sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
.ph-note { margin: var(--ph-s3); }
.ph-stage { flex: 1 1 auto; min-height: 0; position: relative; display: flex; align-items: center; justify-content: center; overflow: hidden; background: #000; }
.ph-frame { position: relative; background: #000; overflow: hidden; }
.is-phone-mode .ph-frame { border-radius: var(--ph-r-card); box-shadow: 0 0 0 1px var(--ph-divider); }
.ph-frame canvas { display: block; width: 100%; height: 100%; touch-action: none; cursor: pointer; }
.ph-starting { position: absolute; inset: 0; z-index: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: var(--ph-s2); padding: var(--ph-s4); text-align: center; color: #fff; background: rgba(0, 0, 0, 0.78); }
.ph-line { position: absolute; left: var(--ph-s2); bottom: var(--ph-s1); margin: 0; padding: 0 var(--ph-s2); border-radius: var(--ph-r-control); color: #fff; background: rgba(0, 0, 0, 0.55); pointer-events: none; }
.ph-tb { display: flex; align-items: center; gap: 0; }
.ph-conn { display: inline-flex; align-items: center; gap: var(--ph-s1); font-size: var(--ph-text-small); margin-right: var(--ph-s1); }
.ph-conn.is-live { color: var(--ph-positive); }
.ph-conn.is-closed, .ph-conn.is-error { color: var(--ph-negative); }
/* the top bar already says Connected: this chip is its icon (the text stays for assistive technology) */
.ph-conn-text { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
.ph-seg { display: inline-flex; margin-left: var(--ph-s1); border: 1px solid currentColor; border-radius: var(--ph-r-control); overflow: hidden; }
.ph-seg .q-btn { border-radius: 0; min-height: 32px; padding: 0 6px; }
.ph-seg .q-btn.is-on { background: var(--ph-accent); color: var(--ph-on-accent); }
</style>
