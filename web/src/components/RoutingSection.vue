<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Messaging routing (docs/messaging.md): the logins your phones use (SIP and XMPP fronts), which connection an address goes to (first rule that
     matches wins; numbers go out as SMS and logins to that device when no rule says otherwise), and who gets each connection's
     messages. -->
<template>
  <section class="ph-card ph-section" aria-labelledby="route-h" data-test="routing">
    <h2 id="route-h">Messaging</h2>
    <p class="ph-muted">
      Phones whose chat app speaks SIP or XMPP (the N9, Symbian, softphones, Jabber apps) sign in with a login of their own and chat
      through your connections. A message goes to the
      first rule that matches its address; without a rule, a number goes out as SMS through your phones and a login goes to that device.
      Answering a message always goes back the way it came.
    </p>

    <h3 class="ph-h3">Phone logins</h3>
    <ul v-if="logins.length" class="ph-list" aria-label="Phone logins">
      <li v-for="l in logins" :key="l.name" data-test="sip-login">
        <div class="ph-grow"><strong>{{ l.name }}</strong></div>
        <q-btn flat no-caps color="negative" icon="sym_o_delete" :label="'Remove'" :aria-label="'Remove ' + l.name" data-test="sip-login-remove" @click="removeLogin(l.name)" />
      </li>
    </ul>
    <p v-else class="ph-muted" data-test="no-sip-logins">No login yet.</p>
    <div class="ph-row ph-row--start">
      <q-input v-model="newLogin" dense outlined label="New login, e.g. n9" data-test="sip-login-name" @keyup.enter="addLogin" />
      <q-btn unelevated color="primary" no-caps icon="sym_o_add" label="Add login" :disable="!newLogin.trim()" data-test="sip-login-add" @click="addLogin" />
    </div>
    <p v-if="made" class="ph-note" role="status" data-test="sip-login-made">
      <q-icon name="sym_o_key" size="20px" aria-hidden="true" />
      Login <strong>{{ made.name }}</strong>, password <code data-test="sip-login-password">{{ made.password }}</code>, realm <code>{{ made.realm }}</code>.
      On an XMPP (Jabber) app the address is <code data-test="sip-login-jid">{{ made.name }}@{{ made.realm }}</code>, with this machine as
      the server. Write the password down now: it is not shown again.
    </p>

    <h3 class="ph-h3">Rules</h3>
    <ol v-if="routes.length" class="ph-list" aria-label="Rules, first match wins">
      <li v-for="(r, i) in routes" :key="i" data-test="route-row">
        <q-select v-model="r.kind" dense outlined emit-value map-options :options="KINDS" label="Addresses" class="ph-route__kind" data-test="route-kind" />
        <q-input v-if="r.kind === 'prefix'" v-model="r.value" dense outlined label="Starting with" placeholder="signal." class="ph-route__value" data-test="route-value" />
        <q-select v-model="r.conn" dense outlined emit-value map-options :options="connOptions" label="Go to" class="ph-route__conn" data-test="route-conn" @update:model-value="r.opt = ''" />
        <q-select v-if="optionsOf(r.conn).length" v-model="r.opt" dense outlined emit-value map-options :options="optionsOf(r.conn)" label="Through" class="ph-route__opt" data-test="route-opt" />
        <q-btn flat icon="sym_o_arrow_upward" :disable="i === 0" :aria-label="'Move rule ' + (i + 1) + ' up'" data-test="route-up" @click="move(i, -1)" />
        <q-btn flat color="negative" icon="sym_o_delete" :aria-label="'Remove rule ' + (i + 1)" data-test="route-remove" @click="routes.splice(i, 1)" />
      </li>
    </ol>
    <p v-else class="ph-muted" data-test="no-routes">No rules: numbers go out as SMS, logins to that device.</p>
    <q-btn outline color="primary" no-caps icon="sym_o_add" label="Add a rule" data-test="route-add" @click="routes.push({ kind: 'prefix', value: '', conn: conns[0]?.id || 'sms', opt: '' })" />

    <h3 class="ph-h3">Who gets what arrives</h3>
    <ul class="ph-list" aria-label="Who gets each connection's messages">
      <li v-for="c in conns" :key="c.id" data-test="deliver-row">
        <div class="ph-grow"><strong>{{ c.label }}</strong></div>
        <q-select v-model="deliver[c.id]" dense outlined multiple emit-value map-options use-chips :options="loginOptions" label="Every login" class="ph-route__who" :data-test="'deliver-' + c.id" />
      </li>
    </ul>

    <div class="ph-row ph-row--start">
      <q-btn unelevated color="primary" no-caps icon="sym_o_save" label="Save" :loading="saving" data-test="routing-save" @click="save" />
      <span v-if="saved" class="ph-ok" role="status" data-test="routing-saved"><q-icon name="sym_o_check_circle" size="20px" aria-hidden="true" /> Saved</span>
    </div>
    <p v-if="error" class="ph-error" role="alert" data-test="routing-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</p>
  </section>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { apiCall } from '../cloud'

interface Opt { value: string; label: string }
interface Conn { id: string; label: string; options?: Opt[] }
interface Route { kind: 'number' | 'prefix'; value: string; conn: string; opt: string }

const KINDS = [{ value: 'prefix', label: 'Starting with…' }, { value: 'number', label: 'Phone numbers' }]
const conns = ref<Conn[]>([])
const routes = ref<Route[]>([])
const deliver = ref<Record<string, string[]>>({})
const logins = ref<{ name: string }[]>([])
const newLogin = ref('')
const made = ref<{ name: string; password: string; realm: string } | null>(null)
const saving = ref(false)
const saved = ref(false)
const error = ref('')

const connOptions = computed(() => conns.value.map((c) => ({ value: c.id, label: c.label })))
const loginOptions = computed(() => logins.value.map((l) => ({ value: l.name, label: l.name })))
const optionsOf = (id: string) => conns.value.find((c) => c.id === id)?.options ?? []

async function load() {
  try {
    const j = await apiCall('GET', '/phe/routing')
    conns.value = j.connections
    routes.value = j.routes
    logins.value = j.logins
    const d: Record<string, string[]> = {}
    for (const c of j.connections as Conn[]) d[c.id] = j.deliver[c.id] ?? []
    deliver.value = d
  } catch (e) { error.value = (e as Error).message }
}
function move(i: number, by: number) {
  const r = routes.value.splice(i, 1)[0]
  routes.value.splice(i + by, 0, r)
}
async function save() {
  saving.value = true; saved.value = false; error.value = ''
  try {
    await apiCall('PUT', '/phe/routing', { routes: routes.value.map((r) => ({ ...r, value: r.kind === 'prefix' ? r.value.trim() : '' })), deliver: deliver.value })
    saved.value = true
    await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function addLogin() {
  const name = newLogin.value.trim().toLowerCase()
  if (!name) return
  error.value = ''
  try { made.value = await apiCall('POST', '/phe/sip/logins', { name }); newLogin.value = ''; await load() } catch (e) { error.value = (e as Error).message }
}
async function removeLogin(name: string) {
  error.value = ''
  try { await apiCall('POST', '/phe/sip/logins/remove', { name }); if (made.value?.name === name) made.value = null; await load() } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
</script>

<style scoped>
.ph-list li { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.ph-row--start { justify-content: flex-start; gap: 8px; margin-top: 8px; }
.ph-route__kind { min-width: 170px; }
.ph-route__value { width: 140px; }
.ph-route__conn, .ph-route__opt { min-width: 200px; }
.ph-route__who { min-width: 220px; }
</style>
