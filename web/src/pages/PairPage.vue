<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-pair">
    <main class="ph-card ph-pair-card">
      <BrandLogo :size="48" />
      <h1>Connect this browser</h1>
      <p class="ph-muted">{{ brand.tagline }}</p>
      <p>To connect, ask your server for a code and enter it below. The code works once and expires after 10 minutes.</p>
      <q-input v-model="code" label="Pairing code" outlined autofocus autocomplete="off" autocapitalize="characters" spellcheck="false"
               placeholder="ABCD-EFGH" data-test="code" @keyup.enter="doPair" />
      <q-input v-model="name" label="Name for this browser" outlined data-test="name" hint="Shown in your device list." @keyup.enter="doPair" />
      <p v-if="state.error" class="ph-error" role="alert" data-test="pair-error">
        <q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ friendly(state.error) }}
      </p>
      <q-btn unelevated color="primary" label="Connect" :loading="state.status === 'connecting'" class="full-width" data-test="pair" @click="doPair" />
    </main>
  </q-page>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { state, pair } from '../cloud.js'
import { brand } from '../brand.js'
import BrandLogo from '../components/BrandLogo.vue'

const router = useRouter()
const code = ref('')
const name = ref('Web browser')

// plain words for what went wrong, and what to do next
function friendly(msg) {
  if (/invalid or expired/i.test(msg)) return 'That code did not work. It may have been used already or expired. Ask your server for a new one.'
  if (/connection/i.test(msg)) return 'Could not reach the server. Check your network and try again.'
  return msg
}

async function doPair() {
  try {
    await pair(code.value, name.value)
    router.replace('/browse')
  } catch (e) { /* the message is shown above the button */ }
}
</script>

<style scoped>
.ph-pair { display: flex; align-items: center; justify-content: center; padding: var(--ph-s4); }
.ph-pair-card { width: min(100%, 420px); display: flex; flex-direction: column; gap: var(--ph-s4); padding: var(--ph-s6); }
.ph-pair-card p { margin: 0; }
.ph-error { display: flex; gap: var(--ph-s2); align-items: flex-start; color: var(--ph-negative); }
</style>
