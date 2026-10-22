<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="q-pa-md" style="max-width:720px">
    <div class="text-h6 q-mb-sm">Paired devices</div>
    <q-banner v-if="!state.admin" class="bg-amber-1">Only an admin device can manage devices. The first paired device is the admin, or pair with <code>phecloud paircode --admin</code>.</q-banner>
    <q-banner v-if="state.adminError" class="bg-red-1 text-red-9">{{ state.adminError }}</q-banner>
    <q-list v-if="state.admin" bordered separator data-test="device-list">
      <q-item v-for="d in state.deviceList" :key="d.id" :data-test="'device-' + d.id">
        <q-item-section>
          <q-item-label>{{ d.name || d.id }} <q-badge v-if="d.admin" color="primary" label="admin" /> <q-badge v-if="d.id === state.dev" color="green" label="this browser" /> <q-badge v-if="d.revoked" color="grey" label="revoked" /></q-item-label>
          <q-item-label caption>{{ d.id }} - paired {{ fmt(d.created) }} - last seen {{ d.seen ? fmt(d.seen) : 'never' }}</q-item-label>
        </q-item-section>
        <q-item-section side>
          <q-btn flat color="negative" label="Revoke" :disable="d.revoked || d.id === state.dev" :data-test="'revoke-' + d.id" @click="confirm(d)" />
        </q-item-section>
      </q-item>
    </q-list>
    <q-btn v-if="state.admin" flat icon="refresh" label="Refresh" class="q-mt-sm" @click="api.devices()" />
  </q-page>
</template>

<script setup>
import { onMounted } from 'vue'
import { state, api, connect } from '../cloud.js'

const fmt = (t) => new Date(t * 1000).toLocaleString()
function confirm(d) {
  if (window.confirm('Revoke ' + (d.name || d.id) + '? It will be signed out at its next connection.')) {
    api.revoke(d.id)
    setTimeout(() => api.devices(), 300)
  }
}
onMounted(async () => {
  try { await connect() } catch (e) { /* the status chip shows it */ }
  api.devices()
})
</script>
