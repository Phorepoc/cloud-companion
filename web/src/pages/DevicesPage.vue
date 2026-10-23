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

    <section v-if="state.admin" class="ph-apppw" aria-labelledby="apppw-h">
      <div class="ph-head2">
        <h2 id="apppw-h">App passwords</h2>
        <q-btn unelevated color="primary" icon="sym_o_add" label="Add a device" data-test="apppw-new" @click="addOpen = true; addName = ''; addDays = 0; addError = ''" />
      </div>
      <p class="ph-muted">Phones and apps that cannot sign in the usual way (mail, calendar and contacts) each get their own password, which you can switch off here at any time.</p>
      <q-banner v-if="apppwError" class="bg-negative text-white q-mb-md" rounded role="alert">{{ apppwError }}</q-banner>
      <EmptyState v-if="apppwLoaded && !apppws.length && !apppwError" icon="sym_o_key" title="No app passwords yet" text="Add a device to get a password for it." />
      <ul v-if="apppws.length" class="ph-list ph-card" data-test="apppw-list">
        <li v-for="p in apppws" :key="p.id" class="ph-row" :data-test="'apppw-row-' + p.id">
          <div class="ph-row-main">
            <div class="ph-row-title">{{ p.name }}</div>
            <div class="ph-small ph-muted">Created {{ p.created ? new Date(p.created).toLocaleDateString() : 'earlier' }}. {{ p.expires ? 'Expires ' + new Date(p.expires).toLocaleDateString() + '.' : 'Does not expire.' }}</div>
          </div>
          <q-btn flat color="negative" label="Switch off" :aria-label="'Switch off the password of ' + p.name" :data-test="'apppw-revoke-' + p.id" @click="revokePw(p)" />
        </li>
      </ul>
    </section>

    <q-dialog v-model="addOpen" persistent>
      <q-card class="ph-dialog" data-test="apppw-form">
        <form @submit.prevent="createPw">
          <q-card-section class="ph-formcol">
            <h2>Add a device</h2>
            <q-input v-model="addName" label="Name of the device" outlined autofocus hint="For example: Old phone." data-test="apppw-name" />
            <q-select v-model="addDays" :options="[{ label: 'Never expires', value: 0 }, { label: 'In 30 days', value: 30 }, { label: 'In a year', value: 365 }]" label="Expires" outlined emit-value map-options />
            <p v-if="addError" class="ph-err" role="alert">{{ addError }}</p>
          </q-card-section>
          <q-card-actions align="right">
            <q-btn flat label="Cancel" :disable="adding" @click="addOpen = false" />
            <q-btn unelevated color="primary" label="Create password" type="submit" :loading="adding" data-test="apppw-create" />
          </q-card-actions>
        </form>
      </q-card>
    </q-dialog>

    <q-dialog v-model="secretOpen" persistent>
      <q-card class="ph-dialog" data-test="apppw-secret-dialog">
        <q-card-section class="ph-formcol">
          <h2>Password for {{ created.name }}</h2>
          <p><strong>Copy it now.</strong> It is shown only once. If you lose it, switch it off and make a new one.</p>
          <dl class="ph-facts">
            <dt>User name</dt><dd class="ph-mono">{{ created.owner }}</dd>
            <dt>Password</dt><dd class="ph-mono" data-test="apppw-secret">{{ created.secret }}</dd>
            <dt>Server</dt><dd class="ph-mono">{{ created.domain || 'your hub address' }}</dd>
          </dl>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat icon="sym_o_content_copy" label="Copy password" @click="copySecret" />
          <q-btn unelevated color="primary" label="Done" data-test="apppw-done" @click="secretOpen = false" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { state, api, connect, hubCall } from '../cloud.js'
import { toast } from '../toast.js'
import EmptyState from '../components/EmptyState.vue'

const fmt = (t) => new Date(t * 1000).toLocaleString()
function confirm(d) {
  if (window.confirm('Sign out ' + (d.name || d.id) + '? It will need a new code to connect again.')) {
    api.revoke(d.id)
    setTimeout(() => api.devices(), 300)
  }
}

// ---- app passwords (the hub's per-device passwords) ----
const apppws = ref([])
const apppwLoaded = ref(false)
const apppwError = ref('')
const addOpen = ref(false)
const addName = ref('')
const addDays = ref(0)
const addError = ref('')
const adding = ref(false)
const secretOpen = ref(false)
const created = ref({})

async function loadPw() {
  if (!state.admin) return
  apppwError.value = ''
  try {
    apppws.value = (await hubCall({ op: 'apppw.list' })).apppws || []
  } catch (e) {
    apppwError.value = /not set up/i.test(e.message) ? 'The hub is not set up on this server, so there are no app passwords.' : e.message
  } finally {
    apppwLoaded.value = true
  }
}
async function createPw() {
  adding.value = true
  addError.value = ''
  try {
    const r = await hubCall({ op: 'apppw.add', name: addName.value, days: addDays.value || undefined })
    created.value = { name: addName.value, secret: r.secret, owner: r.owner, domain: r.domain }
    addOpen.value = false
    secretOpen.value = true
    await loadPw()
  } catch (e) {
    addError.value = e.message
  } finally {
    adding.value = false
  }
}
async function revokePw(p) {
  if (!window.confirm('Switch off the password of ' + p.name + '? That device can no longer read your mail, calendar or contacts.')) return
  try {
    await hubCall({ op: 'apppw.revoke', id2: p.id })
    toast('Password switched off')
  } catch (e) {
    apppwError.value = e.message
  }
  await loadPw()
}
async function copySecret() {
  try { await navigator.clipboard.writeText(created.value.secret); toast('Password copied') } catch (e) { toast('Could not copy: select the password and copy it by hand') }
}
onMounted(async () => {
  try { await connect() } catch (e) { /* the status in the top bar shows it */ }
  api.devices()
  loadPw()
})
</script>

<style scoped>
.ph-list { list-style: none; padding: 0; margin: var(--ph-s4) 0 0; }
.ph-row { display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s3) var(--ph-s4); border-bottom: 1px solid var(--ph-divider); }
.ph-row:last-child { border-bottom: 0; }
.ph-row-main { flex: 1; min-width: 0; }
.ph-row-title { font-weight: 600; display: flex; flex-wrap: wrap; gap: var(--ph-s2); align-items: center; overflow-wrap: anywhere; }
.ph-apppw { margin-top: var(--ph-s6); }
.ph-head2 { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s2); }
.ph-dialog { width: min(94vw, 480px); }
.ph-formcol { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-err { color: var(--ph-negative); margin: 0; }
.ph-facts { display: grid; grid-template-columns: auto 1fr; gap: var(--ph-s1) var(--ph-s3); margin: 0; }
.ph-facts dt { color: var(--ph-text-muted); }
.ph-facts dd { margin: 0; overflow-wrap: anywhere; }
.ph-mono { font-family: var(--ph-font-mono); font-size: var(--ph-text-small); }
</style>
