<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <h1>Devices</h1>
    <p class="ph-muted">The phones, computers and browsers connected to your account.</p>
    <EmptyState v-if="!state.admin" icon="sym_o_lock" title="Only an admin device can manage devices"
                text="The first device you connect is the admin. You can also create an admin device with the server command paircode --admin." />
    <q-banner v-if="state.adminError" class="bg-negative text-white q-mb-md" rounded role="alert">{{ state.adminError }}</q-banner>
    <EmptyState v-if="state.admin && !state.deviceList.length" state="loading" title="Loading devices" text="This takes a moment." />
    <ul v-if="state.admin && state.deviceList.length" class="ph-list ph-card" data-test="device-list">
      <li v-for="d in state.deviceList" :key="d.id" class="q-item ph-row" :data-test="'device-' + d.id">
        <div class="ph-row-main">
          <div class="ph-row-title">
            {{ d.name || d.id }}
            <q-badge v-if="d.id === state.dev" color="positive" text-color="white" label="This browser" />
            <q-badge v-if="d.admin" outline color="primary" label="Admin" />
            <q-badge v-if="d.revoked" outline color="grey-8" label="Signed out" />
          </div>
          <div class="ph-small ph-muted">Connected {{ fmt(d.created) }}. Last seen {{ d.seen ? fmt(d.seen) : 'never' }}.</div>
        </div>
        <q-btn flat color="negative" label="Sign out" :disable="d.revoked || d.id === state.dev" :data-test="'revoke-' + d.id"
               :aria-label="'Sign out ' + (d.name || d.id)" @click="confirm(d)" />
      </li>
    </ul>
    <q-btn v-if="state.admin" flat icon="sym_o_refresh" label="Refresh" class="q-mt-sm" @click="api.devices()" />
  </q-page>
</template>

<script setup>
import { onMounted } from 'vue'
import { state, api, connect } from '../cloud.js'
import EmptyState from '../components/EmptyState.vue'

const fmt = (t) => new Date(t * 1000).toLocaleString()
function confirm(d) {
  if (window.confirm('Sign out ' + (d.name || d.id) + '? It will need a new code to connect again.')) {
    api.revoke(d.id)
    setTimeout(() => api.devices(), 300)
  }
}
onMounted(async () => {
  try { await connect() } catch (e) { /* the status in the top bar shows it */ }
  api.devices()
})
</script>

<style scoped>
.ph-list { list-style: none; padding: 0; margin: var(--ph-s4) 0 0; }
.ph-row { display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s3) var(--ph-s4); border-bottom: 1px solid var(--ph-divider); }
.ph-row:last-child { border-bottom: 0; }
.ph-row-main { flex: 1; min-width: 0; }
.ph-row-title { font-weight: 600; display: flex; flex-wrap: wrap; gap: var(--ph-s2); align-items: center; overflow-wrap: anywhere; }
</style>
