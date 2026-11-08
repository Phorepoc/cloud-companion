<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- System (owner only, on a board set up by the host manager): what the board is, and remote login (SSH) for looking into it,
     off by default (internal/host/ssh.go). The first time it goes on, the owner chooses its password; after that the switch
     alone. A server set up by hand has its own login: the page says so. -->
<template>
  <q-page class="ph-page ph-sys" data-test="system">
    <h1>System</h1>
    <HdStatusState v-if="!state.admin" icon="lock" title="Only an admin device can see the system" text="Open this page on the first device you connected." />
    <HdStatusState v-else-if="available === null" state="loading" title="Looking for the board" text="This takes a moment." />
    <p v-else-if="!available" class="ph-muted" data-test="system-by-hand">This server was set up by hand: it is looked after with your
      own login and tools.</p>
    <template v-else>
      <section class="ph-card ph-sys__list" data-test="system-board">
        <HdListItem title="Board" :subtitle="board.model || 'A computer (' + board.arch + ')'" />
        <HdListItem title="Memory" :subtitle="memory" />
        <HdListItem v-if="board.storage" title="Runs from" :subtitle="storageText" />
      </section>

      <section v-if="ssh.available" class="ph-card ph-sys__list" data-test="system-ssh">
        <HdListItem title="Remote login (SSH)" :subtitle="ssh.on ? 'On' : 'Off'" data-test="system-ssh-item">
          <template #trailing>
            <HdSwitch :model-value="ssh.on || asking" :disabled="busy" aria-label="Remote login (SSH)" data-test="system-ssh-switch" @update:model-value="flip" />
          </template>
        </HdListItem>
        <form v-if="asking" class="ph-sys__ask" data-test="system-ssh-form" @submit.prevent="turnOn">
          <p class="ph-muted">{{ ssh.hasPassword ? 'A new password for' : 'Choose a user name and a password for logging in. You need them each time; the board does not show the password again.' }}{{ ssh.hasPassword ? ' ' + ssh.user + '.' : '' }}</p>
          <HdTextField v-if="!ssh.hasPassword" v-model="user" label="User name" helper="Small letters and digits, for example anna" autocomplete="off" data-test="system-ssh-user" />
          <HdTextField v-model="pass1" type="password" autocomplete="new-password" label="Password (10 characters or more)" data-test="system-ssh-password" />
          <HdTextField v-model="pass2" type="password" autocomplete="new-password" label="The same again" data-test="system-ssh-password2" />
          <div class="ph-sys__actions">
            <HdButton type="submit" variant="accent" :disabled="busy" data-test="system-ssh-on">Turn on</HdButton>
            <HdButton variant="plain" :disabled="busy" @click="asking = false; error = ''">Cancel</HdButton>
          </div>
        </form>
        <div v-if="ssh.on" class="ph-sys__how" data-test="system-ssh-how">
          <p>From a computer on the same network:</p>
          <code>ssh {{ ssh.user }}@{{ hostName }}</code>
          <p class="ph-muted">The password is the one you chose. Turn this off when you are done: anyone with the password can change
            everything on the board.</p>
          <button v-if="!asking" type="button" class="ph-sys__link" data-test="system-ssh-change" @click="asking = true">Change the password</button>
        </div>
        <p v-if="error" class="ph-err" role="alert" data-test="system-error">{{ error }}</p>
      </section>
    </template>
  </q-page>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { HdButton, HdListItem, HdStatusState, HdSwitch, HdTextField } from '@etesian-design/vue'
import { state, apiCall } from '../cloud'

interface SSH { available: boolean; on: boolean; user: string; hasPassword: boolean }
const available = ref<boolean | null>(null)
const board = reactive({ model: '', arch: '', memory: 0, storage: '' })
const ssh = reactive<SSH>({ available: false, on: false, user: 'cloud', hasPassword: false })
const asking = ref(false)
const busy = ref(false)
const error = ref('')
const user = ref('')
const pass1 = ref('')
const pass2 = ref('')
const hostName = location.hostname.endsWith('.local') ? location.hostname : 'cloud-companion.local'

const memory = computed(() => (board.memory ? Math.round(board.memory / 2 ** 30) + ' GB' : 'Unknown'))
const storageText = computed(() => ({ sd: 'A memory card', nvme: 'An NVMe drive', usb: 'A USB drive' } as Record<string, string>)[board.storage] || board.storage)

function takeSSH(r: { available: boolean; ssh: Omit<SSH, 'available'> }) { Object.assign(ssh, { available: r.available }, r.ssh) }
async function load() {
  try {
    const h = await apiCall('GET', '/phe/host')
    available.value = !!h.available
    if (!h.available) return
    Object.assign(board, (await apiCall('GET', '/phe/host/system')).board)
    takeSSH(await apiCall('GET', '/phe/host/ssh'))
  } catch (e) {
    error.value = (e as Error).message
    if (available.value === null) available.value = false
  }
}
async function put(on: boolean, password = '', name = '') {
  busy.value = true
  error.value = ''
  try {
    takeSSH(await apiCall('PUT', '/phe/host/ssh', { on, user: name, password }))
    asking.value = false
    pass1.value = pass2.value = ''
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
function flip(on: boolean) {
  if (!on) { asking.value ? (asking.value = false) : put(false); return }
  if (ssh.hasPassword) put(true)
  else asking.value = true
}
function turnOn() {
  if (!ssh.hasPassword && !/^[a-z][a-z0-9_-]{0,31}$/.test(user.value.trim())) { error.value = 'The user name: small letters and digits, starting with a letter.'; return }
  if (pass1.value.length < 10) { error.value = 'The password needs 10 characters or more.'; return }
  if (pass1.value !== pass2.value) { error.value = 'The two passwords are not the same.'; return }
  put(true, pass1.value, user.value.trim())
}

// the page can open before the connection says this device is the owner (a reload, a link): load once it does
watch(() => state.admin, (a) => { if (a && available.value === null) load() }, { immediate: true })
</script>

<style scoped>
.ph-sys__list { padding: var(--ph-s2) 0; margin-bottom: var(--ph-s3); }
.ph-sys__ask, .ph-sys__how { display: flex; flex-direction: column; gap: var(--ph-s3); padding: var(--ph-s2) var(--ph-s4) var(--ph-s3); }
.ph-sys__ask p, .ph-sys__how p { margin: 0; }
.ph-sys__how code { padding: var(--ph-s3); border-radius: 12px; background: var(--ph-surface-2, rgba(127, 127, 127, 0.12)); overflow-wrap: anywhere; }
.ph-sys__actions { display: flex; gap: var(--ph-s2); flex-wrap: wrap; }
.ph-sys__link { align-self: flex-start; border: 0; background: none; padding: 0; color: var(--ph-accent, #1C51AF); font: inherit; cursor: pointer; }
.ph-err { color: var(--ph-error, #D6222A); padding: 0 var(--ph-s4); }
</style>
