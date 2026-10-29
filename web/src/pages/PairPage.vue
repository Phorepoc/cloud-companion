<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-pair">
    <main class="ph-card ph-pair-card">
      <BrandLogo :size="48" />
      <h1>Connect this browser</h1>
      <p class="ph-muted">{{ brand.tagline }}</p>

      <p v-if="blocker" class="ph-note" role="note" data-test="passkey-blocker"><q-icon name="sym_o_info" size="20px" aria-hidden="true" /> {{ blocker }}</p>
      <section v-if="status.owner" class="ph-signin" aria-labelledby="signin-h">
        <h2 id="signin-h" class="ph-h2">Sign in</h2>
        <q-btn v-if="supported" unelevated color="primary" icon="sym_o_passkey" label="Sign in with a passkey" class="full-width"
               :loading="busy === 'login'" data-test="passkey-login" @click="doLogin" />
        <p v-else class="ph-muted">This browser cannot use passkeys here (they need https or localhost). Use the recovery code or a pairing code below.</p>
        <q-btn flat no-caps color="primary" :label="showRecovery ? 'Hide the recovery code' : 'Use the recovery code instead'" data-test="recovery-toggle" @click="showRecovery = !showRecovery" />
        <div v-if="showRecovery" class="ph-stack">
          <q-input v-model="recovery" label="Recovery code" outlined autocomplete="off" autocapitalize="characters" spellcheck="false"
                   placeholder="ABCD-EFGH-JKLM-NPQR" data-test="recovery-code" @keyup.enter="doRecover" />
          <q-btn outline color="primary" label="Sign in with the recovery code" :loading="busy === 'recover'" data-test="recovery-go" @click="doRecover" />
        </div>
        <q-separator class="ph-sep" />
        <h2 class="ph-h2">Or pair with a code</h2>
      </section>

      <p v-else>To connect, ask your server for a code and enter it below. The code works once and expires after 10 minutes.</p>
      <q-input v-model="code" label="Pairing code" outlined :autofocus="!status.owner" autocomplete="off" autocapitalize="characters" spellcheck="false"
               placeholder="ABCD-EFGH" data-test="code" @keyup.enter="doPair" />
      <q-input v-model="name" label="Name for this browser" outlined data-test="name" hint="Shown in your device list." @keyup.enter="doPair" />
      <p v-if="message" class="ph-error" role="alert" data-test="pair-error">
        <q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ friendly(message) }}
      </p>
      <q-btn unelevated color="primary" label="Connect" :loading="state.status === 'connecting' && !busy" class="full-width" data-test="pair" @click="doPair" />

      <section v-if="supported" class="ph-signin" aria-labelledby="setup-h">
        <q-separator class="ph-sep" />
        <h2 id="setup-h" class="ph-h2">{{ status.owner ? 'Add a passkey' : 'Sign in with a passkey next time' }}</h2>
        <p class="ph-muted ph-small">
          Enter an <strong>admin</strong> code above (<code>phecloud paircode --admin</code> on the server), then register a passkey on this device.
          {{ status.owner ? '' : 'You also get a recovery code, shown once.' }}
        </p>
        <q-btn outline color="primary" icon="sym_o_passkey" :label="status.owner ? 'Add a passkey' : 'Set up a passkey'" :loading="busy === 'register'" data-test="passkey-register" @click="doRegister" />
      </section>
    </main>

    <q-dialog v-model="recoveryDialog" persistent>
      <q-card class="ph-dialog" data-test="recovery-dialog">
        <q-card-section>
          <h2 class="ph-h2">Save your recovery code</h2>
          <p>If you lose your passkeys, this code signs you in. It works once; a new one is shown each time you use it. Keep it somewhere safe, it is not shown again.</p>
          <p class="ph-code" data-test="recovery-shown">{{ shownCode }}</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Copy" icon="sym_o_content_copy" data-test="recovery-copy" @click="copyCode" />
          <q-btn unelevated color="primary" label="I saved it" data-test="recovery-saved" @click="savedIt" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { state, pair, authStatus, passkeysSupported, passkeyRegister, passkeyLogin, recoverLogin, passkeyBlocker } from '../cloud'
import { brand } from '../brand'
import BrandLogo from '../components/BrandLogo.vue'

const router = useRouter()
const code = ref('')
const name = ref('Web browser')
const status = ref({ owner: false, passkeys: 0 })
const blocker = passkeyBlocker()
const supported = passkeysSupported() && !blocker
const busy = ref('')
const message = ref('')
const showRecovery = ref(false)
const recovery = ref('')
const recoveryDialog = ref(false)
const shownCode = ref('')

onMounted(async () => { try { status.value = await authStatus() } catch (e) { /* offline: the pairing form still works */ } })

// plain words for what went wrong, and what to do next
function friendly(msg: string) {
  if (/invalid or expired/i.test(msg)) return 'That code did not work. It may have been used already or expired. Ask your server for a new one.'
  if (/connection/i.test(msg)) return 'Could not reach the server. Check your network and try again.'
  if (/NotAllowedError|cancel|timed out/i.test(msg)) return 'The passkey request was cancelled or timed out. Try again.'
  return msg
}

async function run<T>(kind: string, fn: () => Promise<T>) {
  message.value = ''
  busy.value = kind
  try { return await fn() } catch (e) { message.value = (e as Error).message || String(e); return null } finally { busy.value = '' }
}

async function doPair() {
  message.value = ''
  try {
    await pair(code.value, name.value)
    router.replace('/browse')
  } catch (e) { message.value = (e as Error).message }
}

function afterSignIn(rec: string | void | null) {
  if (rec) { shownCode.value = rec; recoveryDialog.value = true } else router.replace('/browse')
}

async function doLogin() {
  const ok = await run('login', async () => { await passkeyLogin(name.value); return true })
  if (ok) router.replace('/browse')
}

async function doRegister() {
  if (!code.value.trim()) { message.value = 'Enter an admin pairing code first.'; return }
  const r = await run('register', async () => ({ rec: await passkeyRegister(code.value.trim().toUpperCase(), name.value) }))
  if (r) afterSignIn(r.rec)
}

async function doRecover() {
  const r = await run('recover', async () => ({ rec: await recoverLogin(recovery.value, name.value) }))
  if (r) afterSignIn(r.rec)
}

async function copyCode() { try { await navigator.clipboard.writeText(shownCode.value) } catch (e) { /* the code is on screen */ } }
function savedIt() { recoveryDialog.value = false; shownCode.value = ''; router.replace('/browse') }
</script>

<style scoped>
.ph-pair { display: flex; align-items: center; justify-content: center; padding: var(--ph-s4); }
.ph-pair-card { width: min(100%, 420px); display: flex; flex-direction: column; gap: var(--ph-s4); padding: var(--ph-s6); }
.ph-pair-card p { margin: 0; }
.ph-signin { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-stack { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-h2 { font-size: var(--ph-text-h3, 1.1rem); margin: 0; }
.ph-sep { margin: var(--ph-s2) 0; }
.ph-note { display: flex; gap: var(--ph-s2); align-items: flex-start; padding: var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); }
.ph-error { display: flex; gap: var(--ph-s2); align-items: flex-start; color: var(--ph-negative); }
.ph-dialog { width: min(92vw, 440px); background: var(--ph-surface); color: var(--ph-text); }
.ph-dialog p { margin: var(--ph-s3) 0 0; }
.ph-code { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 1.15rem; letter-spacing: 0.04em; padding: var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); word-break: break-all; user-select: all; }
</style>
