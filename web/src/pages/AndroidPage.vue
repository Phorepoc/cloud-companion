<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <h1>Android</h1>
    <p class="ph-muted">An Android phone that runs on your server, shown here. Tap and type on the screen below; the apps keep running when you close this page.</p>
    <q-banner v-if="error" class="bg-negative text-white q-mb-md" rounded role="alert" data-test="android-error">{{ error }}</q-banner>
    <p v-if="state.admin === false" class="ph-note" role="note">Only an admin device can open Android.</p>
    <p v-if="loaded && !configured" class="ph-note" role="note" data-test="android-off">
      <q-icon name="sym_o_info" size="20px" aria-hidden="true" /> Android is not set up on this server. See docs/android.md for the two extra containers it needs.
    </p>
    <div v-if="src" class="ph-android">
      <iframe :src="src" title="Android screen" allow="clipboard-read; clipboard-write" data-test="android-frame"></iframe>
    </div>
    <q-btn v-if="configured" unelevated color="primary" :label="src ? 'Reconnect' : 'Open Android'" icon="sym_o_smartphone" :loading="busy" data-test="android-open" @click="open" />
  </q-page>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { apiCall, state } from '../cloud.js'

const configured = ref(false), loaded = ref(false), busy = ref(false), error = ref(''), src = ref('')
async function open() {
  busy.value = true; error.value = ''
  try {
    const j = await apiCall('GET', '/phe/android/ticket')
    configured.value = j.configured
    if (j.configured) src.value = j.path + '?t=' + encodeURIComponent(j.ticket) + '&_=' + Date.now()
  } catch (e) { error.value = e.message } finally { busy.value = false; loaded.value = true }
}
onMounted(open)
</script>

<style scoped>
.ph-android { margin: 12px 0; }
.ph-android iframe { width: 100%; max-width: 480px; height: min(820px, calc(100vh - 260px)); min-height: 480px; border: 1px solid var(--ph-line, #d5dae0); border-radius: 12px; background: #000; }
</style>
