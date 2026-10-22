<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="flex flex-center">
    <q-card style="width:min(92vw,380px)">
      <q-card-section>
        <div class="text-h6">Pair this browser</div>
        <div class="text-body2 text-grey-8 q-mt-xs">
          Run <code>phecloud paircode</code> on the server and enter the code. The device key is kept in this browser (IndexedDB).
        </div>
      </q-card-section>
      <q-card-section class="q-gutter-md">
        <q-input v-model="code" label="Pairing code" outlined autofocus data-test="code" placeholder="ABCD-EFGH" @keyup.enter="doPair" />
        <q-input v-model="name" label="Device name" outlined data-test="name" />
        <div v-if="state.error" class="text-negative" data-test="pair-error">{{ state.error }}</div>
      </q-card-section>
      <q-card-actions align="right">
        <q-btn color="primary" label="Pair" :loading="state.status === 'connecting'" data-test="pair" @click="doPair" />
      </q-card-actions>
    </q-card>
  </q-page>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { state, pair } from '../cloud.js'

const router = useRouter()
const code = ref('')
const name = ref('Web browser')
async function doPair() {
  try {
    await pair(code.value, name.value)
    router.replace('/browse')
  } catch (e) { /* the error is shown */ }
}
</script>
