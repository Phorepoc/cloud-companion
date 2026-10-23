<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <h1>Settings</h1>

    <section class="ph-card ph-section" aria-labelledby="set-appearance">
      <h2 id="set-appearance">Appearance</h2>
      <p class="ph-muted">Choose how the app looks. "Match system" follows your device's light or dark setting.</p>
      <q-btn-toggle v-model="choice" no-caps unelevated spread toggle-color="primary" toggle-text-color="on-accent" color="surface-2" text-color="ph"
                    :options="[{ label: 'Match system', value: 'system', icon: 'sym_o_contrast' }, { label: 'Light', value: 'light', icon: 'sym_o_light_mode' }, { label: 'Dark', value: 'dark', icon: 'sym_o_dark_mode' }]"
                    data-test="theme" @update:model-value="applyTheme" />
    </section>

    <section class="ph-card ph-section" aria-labelledby="set-data">
      <h2 id="set-data">Data use</h2>
      <p class="ph-muted">Lower picture quality and grayscale send fewer bytes. Text always stays sharp. This applies to this device only.</p>
      <div id="q-label" class="ph-small">Picture quality: {{ q }}</div>
      <q-slider v-model="q" :min="10" :max="95" :step="5" label color="primary" aria-labelledby="q-label" data-test="quality" />
      <q-toggle v-model="gray" label="Grayscale pictures" data-test="gray" />
      <div class="ph-actions">
        <q-btn unelevated color="primary" label="Save" data-test="apply" @click="apply" />
        <span v-if="saved" class="ph-ok" role="status" data-test="saved"><q-icon name="sym_o_check_circle" size="20px" aria-hidden="true" /> Saved</span>
      </div>
    </section>

    <section v-if="state.admin" class="ph-card ph-section" aria-labelledby="set-security" data-test="security">
      <h2 id="set-security">Security</h2>
      <p v-if="blocker" class="ph-note" role="note" data-test="passkey-blocker"><q-icon name="sym_o_info" size="20px" aria-hidden="true" /> {{ blocker }}</p>
      <h3 class="ph-h3">Passkeys</h3>
      <p v-if="!keys.length" class="ph-muted" data-test="no-passkeys">No passkey yet. Add one to sign in to this app without a pairing code.</p>
      <ul v-else class="ph-keys" aria-label="Your passkeys">
        <li v-for="k in keys" :key="k.id" data-test="passkey-row">
          <div>
            <strong>{{ k.name || 'Passkey' }}</strong>
            <div class="ph-muted ph-small">Added {{ when(k.created) }} · {{ k.last_used ? 'last used ' + when(k.last_used) : 'never used' }}</div>
          </div>
          <q-btn flat round color="negative" icon="sym_o_delete" :aria-label="'Remove ' + (k.name || 'passkey')" data-test="passkey-remove" @click="askRemove = k" />
        </li>
      </ul>
      <q-input v-model="keyName" label="Name for the new passkey" outlined dense data-test="passkey-name" :disable="!!blocker" />
      <div class="ph-actions">
        <q-btn outline color="primary" icon="sym_o_passkey" label="Add a passkey" :disable="!!blocker" :loading="busy === 'add'" data-test="passkey-add" @click="addKey" />
      </div>
      <h3 class="ph-h3">Recovery code</h3>
      <p class="ph-muted">The recovery code signs you in if you lose your passkeys. For safety the server only keeps a fingerprint of it, so it cannot be shown again: make a new one (the old one stops working) and save it.</p>
      <div class="ph-actions">
        <q-btn outline color="primary" icon="sym_o_autorenew" label="Make a new recovery code" :disable="!owner" :loading="busy === 'rec'" data-test="recovery-regen" @click="regen" />
      </div>
      <p v-if="error" class="ph-error" role="alert" data-test="security-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</p>
    </section>

    <q-dialog :model-value="!!askRemove" @update:model-value="askRemove = null">
      <q-card class="ph-dialog" data-test="remove-dialog">
        <q-card-section>
          <h2 class="ph-h3">Remove this passkey?</h2>
          <p>{{ askRemove && (askRemove.name || 'Passkey') }} will no longer sign in. Browsers already signed in stay signed in.</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" @click="askRemove = null" />
          <q-btn unelevated color="negative" label="Remove" data-test="remove-confirm" @click="doRemove" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog v-model="recDialog" persistent>
      <q-card class="ph-dialog" data-test="recovery-dialog">
        <q-card-section>
          <h2 class="ph-h3">Save your recovery code</h2>
          <p>It is shown once. The previous recovery code no longer works.</p>
          <p class="ph-code" data-test="recovery-shown">{{ shownCode }}</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn unelevated color="primary" label="I saved it" data-test="recovery-saved" @click="recDialog = false; shownCode = ''" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <section class="ph-card ph-section" aria-labelledby="set-device">
      <h2 id="set-device">This browser</h2>
      <p class="ph-muted">Device ID: <span class="ph-mono">{{ state.dev || 'not connected' }}</span></p>
      <q-btn outline color="negative" label="Disconnect this browser" @click="forgetIt" />
    </section>
  </q-page>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { state, api, forget, connect, passkeyList, passkeyRemove, passkeyAdd, recoveryRegenerate, passkeyBlocker } from '../cloud.js'
import { theme, applyTheme } from '../theme.js'

const router = useRouter()
const choice = ref(theme.value)
const q = ref(state.settings.q)
const gray = ref(state.settings.gray)
const saved = ref(false)
async function apply() {
  try { await connect() } catch (e) { /* ignore */ }
  api.settings(q.value, gray.value)
  saved.value = true
}
// ---- Security (admin devices) ----
const keys = ref([])
const owner = ref(false)
const blocker = passkeyBlocker()
const keyName = ref('This browser')
const busy = ref('')
const error = ref('')
const askRemove = ref(null)
const recDialog = ref(false)
const shownCode = ref('')
const when = (t) => new Date(t * 1000).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
async function loadKeys() {
  if (!state.admin) return
  try { const j = await passkeyList(); keys.value = j.passkeys; owner.value = j.owner } catch (e) { error.value = e.message }
}
async function guard(kind, fn) {
  error.value = ''
  busy.value = kind
  try { await fn() } catch (e) { error.value = /NotAllowed|cancel|timed out/i.test(e.message) ? 'The passkey request was cancelled or timed out.' : e.message } finally { busy.value = '' }
}
const addKey = () => guard('add', async () => {
  const rec = await passkeyAdd(keyName.value.trim() || 'Passkey')
  await loadKeys()
  if (rec) { shownCode.value = rec; recDialog.value = true }
})
const regen = () => guard('rec', async () => { shownCode.value = (await recoveryRegenerate()).recovery; recDialog.value = true })
async function doRemove() {
  const k = askRemove.value
  askRemove.value = null
  await guard('rm', async () => { await passkeyRemove(k.id); await loadKeys() })
}
onMounted(loadKeys)

async function forgetIt() {
  await forget()
  router.replace('/pair')
}
</script>

<style scoped>
.ph-section { margin-top: var(--ph-s4); display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-section p { margin: 0; }
.ph-h3 { font-size: var(--ph-text-h3, 1.1rem); margin: var(--ph-s2) 0 0; }
.ph-keys { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--ph-s2); }
.ph-keys li { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); padding: var(--ph-s3); border: 1px solid var(--ph-border); border-radius: var(--ph-r-control); }
.ph-note { display: flex; gap: var(--ph-s2); align-items: flex-start; padding: var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); }
.ph-error { display: flex; gap: var(--ph-s2); align-items: flex-start; color: var(--ph-negative); }
.ph-dialog { width: min(92vw, 440px); background: var(--ph-surface); color: var(--ph-text); }
.ph-dialog p { margin: var(--ph-s3) 0 0; }
.ph-code { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 1.15rem; letter-spacing: 0.04em; padding: var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); word-break: break-all; user-select: all; }
.ph-actions { display: flex; align-items: center; gap: var(--ph-s4); }
.ph-ok { display: inline-flex; align-items: center; gap: var(--ph-s1); color: var(--ph-positive); }
.ph-mono { font-family: var(--ph-font-mono); font-size: var(--ph-text-small); overflow-wrap: anywhere; }

</style>
