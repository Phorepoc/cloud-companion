<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page ph-android" :style="{ '--ar': w / h }">
    <div class="ph-head">
      <h1>Android</h1>
      <q-space />
      <span class="ph-conn" :class="'is-' + conn" role="status" data-test="android-state">
        <q-icon :name="conn === 'live' ? 'sym_o_cloud_done' : conn === 'connecting' || conn === 'starting' ? 'sym_o_sync' : 'sym_o_cloud_off'" size="20px" aria-hidden="true" />
        {{ connText }}
      </span>
      <q-btn v-if="configured" flat no-caps icon="sym_o_refresh" label="Reconnect" :loading="busy" data-test="android-reconnect" @click="connect" />
    </div>

    <p v-if="state.admin === false" class="ph-note" role="note">Only an admin device can open Android.</p>
    <p v-if="loaded && !configured" class="ph-note" role="note" data-test="android-off">
      <q-icon name="sym_o_info" size="20px" aria-hidden="true" /> Android is not set up on this server. See docs/android.md for the two extra containers it needs.
    </p>
    <q-banner v-if="problem" class="bg-negative text-white q-mb-md" rounded role="alert" data-test="android-error">{{ problem }}</q-banner>

    <div v-show="configured" class="ph-toolbar2" :class="{ 'is-compact': phone }" role="toolbar" aria-label="Android controls">
      <q-btn flat no-caps icon="sym_o_arrow_back" :label="lbl('Back')" aria-label="Back" :title="phone ? 'Back' : undefined" data-test="android-back" @click="stream.key(4)" />
      <q-btn flat no-caps icon="sym_o_home" :label="lbl('Home')" aria-label="Home" :title="phone ? 'Home' : undefined" data-test="android-home" @click="stream.key(3)" />
      <q-btn flat no-caps icon="sym_o_crop_square" :label="lbl('Recent')" aria-label="Recent" :title="phone ? 'Recent' : undefined" data-test="android-recent" @click="stream.key(187)" />
      <q-btn flat no-caps :icon="sound ? 'sym_o_volume_up' : 'sym_o_volume_off'" :label="lbl(sound ? 'Sound on' : 'Sound off')" :aria-label="sound ? 'Sound on' : 'Sound off'" :aria-pressed="String(sound)" data-test="android-sound" @click="toggleSound" />
      <q-btn flat no-caps icon="sym_o_content_copy" :label="lbl('Copy from phone')" aria-label="Copy from phone" :title="phone ? 'Copy from phone' : undefined" data-test="android-copy" @click="stream.copyFromPhone()" />
      <q-btn flat no-caps icon="sym_o_content_paste" :label="lbl('Paste to phone')" aria-label="Paste to phone" :title="phone ? 'Paste to phone' : undefined" data-test="android-paste" @click="stream.pasteToPhone()" />
      <q-btn flat no-caps icon="sym_o_download" :label="lbl('Install apps')" aria-label="Install apps" :title="phone ? 'Install apps' : undefined" data-test="android-install" @click="installOpen = true" />
    </div>

    <div v-show="configured" class="ph-stage">
      <div class="ph-frame" :class="{ 'is-wide': w > h }">
        <div v-if="conn === 'starting'" class="ph-starting" role="status" data-test="android-starting">
          <q-spinner size="32px" color="white" aria-hidden="true" />
          <span>Starting Android…</span>
          <span class="ph-small">It was switched off to save resources. This takes about half a minute.</span>
        </div>
        <canvas ref="cv" width="360" height="640" tabindex="0" role="application" data-test="android-screen"
                aria-label="Android screen: click or touch to tap, scroll with the wheel, type to enter text"></canvas>
      </div>
    </div>
    <p v-if="configured" class="ph-muted ph-small ph-line" aria-live="polite" data-test="android-note">{{ note || statsText }}</p>

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
import { ref, shallowRef, computed, inject, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { useQuasar } from 'quasar'
import { apiCall, state } from '../cloud'
import { AndroidStream, type StreamState, type StreamStats } from '../android/stream.js'

const route = useRoute()
const $q = useQuasar()
// on a phone, and in a (phone-shaped) desktop window, the controls are one row of icons (each named for screen readers and on
// hover), so the screen keeps the room
const inWindow = inject('phInWindow', false)
const phone = computed(() => inWindow || $q.screen.lt.sm)
const lbl = (t: string) => (phone.value ? undefined : t)
const cv = ref<HTMLCanvasElement | null>(null), file = ref<HTMLInputElement | null>(null)
const configured = ref(false), loaded = ref(false), busy = ref(false), problem = ref(''), conn = ref<StreamState>('idle'), connMsg = ref('')
const w = ref(360), h = ref(640), note = ref(''), stats = ref<StreamStats | null>(null), sound = ref(false), installOpen = ref(false), pkg = ref('')
const stream = shallowRef(new AndroidStream(document.createElement('canvas'), { base: '/android/' })) // replaced once the canvas exists

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
      stream.value.connect(j.ticket, { codec: q.codec, size: q.size, fps: q.fps, kbps: q.kbps, kind: q.kind, audio: q.audio !== '0' })
    }
  } catch (e) { problem.value = (e as Error).message } finally { busy.value = false; loaded.value = true }
}
function toggleSound() { sound.value = !sound.value; stream.value.setSound(sound.value) }
async function pickApk(e: Event) { const input = e.target as HTMLInputElement; const f = input.files?.[0]; if (f) await stream.value.installApk(f); input.value = '' }
async function fdroid() { if (pkg.value.trim()) await stream.value.installFdroid(pkg.value.trim()) }

onMounted(() => {
  stream.value.dispose()
  stream.value = new AndroidStream(cv.value!, {
    base: '/android/',
    onState: (s, m) => { conn.value = s; connMsg.value = m; if (s === 'error') problem.value = m },
    onStats: (m) => { stats.value = m },
    onNote: (t) => { note.value = t },
    onSize: (a, b) => { w.value = a; h.value = b },
  })
  connect()
})
onBeforeUnmount(() => stream.value.dispose())
</script>

<style scoped>
.ph-head { display: flex; align-items: center; gap: var(--ph-s2); flex-wrap: wrap; }
.ph-conn { display: inline-flex; align-items: center; gap: var(--ph-s1); font-size: var(--ph-text-small); color: var(--ph-text-muted); }
.ph-conn.is-live { color: var(--ph-positive); }
.ph-conn.is-closed, .ph-conn.is-error { color: var(--ph-negative); }
.ph-toolbar2 { display: flex; flex-wrap: wrap; gap: var(--ph-s1); margin: var(--ph-s2) 0; }
.ph-stage { display: flex; justify-content: center; }
/* the frame is the phone's shape (the stream's own aspect ratio, so it follows rotation) and as large as the content area allows */
.ph-frame {
  --chrome: 250px;                                   /* bars, heading, toolbar, note: what is not the screen */
  /* cqh is the desktop window's height inside a window, and the viewport's height elsewhere (no size container) */
  width: min(100%, calc((100cqh - var(--chrome)) * var(--ar)));
  min-width: min(100%, 220px);
  background: #000; border: 1px solid var(--ph-divider); border-radius: var(--ph-r-card); box-shadow: var(--ph-elev-1); overflow: hidden;
}
@media (max-width: 599px) { .ph-frame { --chrome: 270px; } }
/* in a desktop window: no app bars around the page, the heading is in the window bar */
.ph-in-window .ph-frame { --chrome: 160px; }
.ph-toolbar2.is-compact { flex-wrap: nowrap; justify-content: space-between; gap: 0; }
.ph-frame { position: relative; }
.ph-starting { position: absolute; inset: 0; z-index: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: var(--ph-s2); padding: var(--ph-s4); text-align: center; color: #fff; background: rgba(0, 0, 0, 0.78); }
.ph-frame canvas { display: block; width: 100%; height: auto; touch-action: none; cursor: pointer; }
.ph-line { text-align: center; min-height: 1.5em; margin: var(--ph-s2) 0 0; }
</style>
