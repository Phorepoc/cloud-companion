<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- A page another of your devices sent here (Browser, More, Send to device): open it in the Browser, in this device's own
     browser, or not at all. One at a time, oldest first. -->
<template>
  <q-dialog :model-value="!!page" persistent @update:model-value="(v: boolean) => { if (!v) done() }">
    <q-card v-if="page" class="ph-sent" data-test="sent-dialog">
      <q-card-section>
        <h2>A page from {{ page.from }}</h2>
        <p class="ph-sent__title">{{ page.title || page.url }}</p>
        <p class="ph-small ph-muted ph-sent__url">{{ page.url }}</p>
      </q-card-section>
      <q-card-actions align="right">
        <q-btn flat label="Not now" data-test="sent-dismiss" @click="done" />
        <q-btn flat label="This device's browser" type="a" :href="page.url" target="_blank" rel="noopener noreferrer" data-test="sent-native" @click="done" />
        <q-btn unelevated color="primary" label="Open in Browser" data-test="sent-open" @click="openHere" />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { state } from '../cloud'
import { openPage } from '../browserEngine'

const router = useRouter()
const page = computed(() => state.sent[0])
function done() { state.sent.shift() }
function openHere() {
  const u = page.value?.url
  done()
  if (!u) return
  openPage(u)
  void router.push('/browse')
}
</script>

<style scoped>
.ph-sent { width: min(92vw, 440px); }
.ph-sent h2 { margin-bottom: var(--ph-s2); }
.ph-sent__title, .ph-sent__url { margin: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
