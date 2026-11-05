<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <h1>Connections</h1>
    <p class="ph-muted">
      Connect the accounts you already have. Cloud Companion copies your mail, contacts and calendars from them into your own server and sends
      mail through each account's own outgoing server. It does not host your mail.
    </p>

    <p v-if="!hubOn" class="ph-note" role="note" data-test="no-hub"><q-icon name="sym_o_info" size="20px" aria-hidden="true" /> The hub is not set up on this server, so accounts can be added and tested but nothing is copied yet.</p>

    <section class="ph-card ph-section" aria-labelledby="acc-h">
      <div class="ph-row">
        <h2 id="acc-h">Your accounts</h2>
        <q-btn unelevated color="primary" icon="sym_o_add" label="Add an account" data-test="add-account" @click="openAdd" />
      </div>
      <div v-if="providers.length" class="ph-oauth" data-test="oauth-box">
        <q-btn v-for="p in providers" :key="p.id" outline color="primary" no-caps :icon="'sym_o_login'" :label="'Sign in with ' + p.label" :data-test="'oauth-' + p.id" @click="openOAuth(p)" />
      </div>
      <p v-if="oauthMsg" :class="oauthMsg.ok ? 'ph-ok' : 'ph-error'" role="status" data-test="oauth-result"><q-icon :name="oauthMsg.ok ? 'sym_o_check_circle' : 'sym_o_error'" size="20px" aria-hidden="true" /> {{ oauthMsg.text }}</p>
      <p v-if="!accounts.length" class="ph-muted" data-test="no-accounts">No account connected yet.</p>
      <ul v-else class="ph-list" aria-label="Connected accounts">
        <li v-for="a in accounts" :key="a.id" data-test="account-row">
          <div class="ph-grow">
            <strong>{{ a.name }}</strong>
            <span class="ph-chip hd-sqm" :class="'ph-' + a.status" data-test="account-status">{{ statusText(a) }}</span>
            <div class="ph-muted ph-small">{{ kindText(a.kind) }} · {{ a.last_sync ? 'last sync ' + ago(a.last_sync) : 'not synced yet' }}</div>
            <div v-if="a.last_error" class="ph-error ph-small" role="alert">{{ a.last_error }}</div>
          </div>
          <q-btn flat round icon="sym_o_sync" :aria-label="'Sync ' + a.name" :loading="syncing === a.id" data-test="account-sync" @click="sync(a)" />
          <q-btn flat round color="negative" icon="sym_o_delete" :aria-label="'Remove ' + a.name" data-test="account-remove" @click="askRemove = a" />
        </li>
      </ul>
      <p v-if="error" class="ph-error" role="alert" data-test="conn-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</p>
    </section>

    <RoutingSection />

    <section class="ph-card ph-section" aria-labelledby="dup-h">
      <div class="ph-row">
        <h2 id="dup-h">Duplicate contacts</h2>
        <q-btn outline color="primary" icon="sym_o_search" label="Look for duplicates" :loading="finding" data-test="find-dups" @click="findDups" />
      </div>
      <p class="ph-muted">Entries that look like the same person (same number, same email, same name). Merging keeps one contact with everything from both; the sources stay linked to it.</p>
      <p v-if="dups !== null && !dups.length" class="ph-muted" data-test="no-dups">No duplicates found.</p>
      <ul v-if="dups && dups.length" class="ph-list" aria-label="Possible duplicates">
        <li v-for="d in dups" :key="d.a + d.b" data-test="dup-row">
          <div class="ph-grow">
            <strong>{{ d.name_a }}</strong> and <strong>{{ d.name_b }}</strong>
            <div class="ph-muted ph-small">{{ d.reason }}</div>
          </div>
          <q-btn flat no-caps color="primary" :label="'Keep ' + short(d.name_a)" data-test="dup-keep-a" @click="merge(d, d.a, d.b)" />
          <q-btn flat no-caps color="primary" :label="'Keep ' + short(d.name_b)" data-test="dup-keep-b" @click="merge(d, d.b, d.a)" />
          <q-btn v-if="d.same_source" flat no-caps color="warning" icon="sym_o_merge" :label="'Merge on ' + d.source_name + '…'" data-test="dup-device-merge" @click="previewDevice(d)" />
          <q-btn flat no-caps label="Not the same" data-test="dup-dismiss" @click="dismiss(d)" />
        </li>
      </ul>
      <p v-if="undoable" class="ph-note" role="status" data-test="undo-bar">
        <q-icon name="sym_o_check_circle" size="20px" aria-hidden="true" /> Merged on {{ undoable.source }}. You can undo this for 10 minutes.
        <q-btn flat no-caps color="primary" label="Undo" data-test="devmerge-undo" @click="undoDevice" />
      </p>
      <p v-if="dupError" class="ph-error" role="alert" data-test="dup-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ dupError }}</p>
    </section>

    <q-dialog v-model="adding" persistent>
      <q-card class="ph-dialog" data-test="add-dialog">
        <q-card-section class="ph-form">
          <h2 class="ph-h3">Add an account</h2>
          <q-select v-model="f.kind" :options="kinds" emit-value map-options outlined dense label="Type" data-test="f-kind" />
          <q-input v-model="f.name" outlined dense label="Name" hint="For example: Work mail" data-test="f-name" />
          <template v-if="f.kind === 'imap'">
            <div class="ph-two">
              <q-input v-model="f.imapHost" outlined dense label="IMAP server" data-test="f-imap-host" />
              <q-input v-model.number="f.imapPort" outlined dense type="number" label="Port" data-test="f-imap-port" />
            </div>
            <q-select v-model="f.imapTLS" :options="securities" emit-value map-options outlined dense label="Security" data-test="f-imap-tls" />
            <q-input v-model="f.user" outlined dense label="User name" autocomplete="off" data-test="f-user" />
            <q-input v-model="f.secret" outlined dense type="password" label="Password or app password" autocomplete="new-password" data-test="f-secret" />
            <q-input v-model="f.pin" outlined dense label="Server certificate fingerprint (only for your own server)" hint="SHA-256, for a self-signed certificate such as Proton Mail Bridge. Leave empty otherwise: the Test button shows the fingerprint to paste." data-test="f-pin" />
            <div class="ph-two">
              <q-input v-model="f.smtpHost" outlined dense label="Outgoing (SMTP) server" data-test="f-smtp-host" />
              <q-input v-model.number="f.smtpPort" outlined dense type="number" label="Port" data-test="f-smtp-port" />
            </div>
            <q-select v-model="f.smtpTLS" :options="securities" emit-value map-options outlined dense label="Outgoing security" data-test="f-smtp-tls" />
            <q-input v-model="f.from" outlined dense label="Send as (address)" data-test="f-from" />
            <q-input v-model="f.folders" outlined dense label="Folders to copy" hint="Comma separated. Default: INBOX" data-test="f-folders" />
            <q-input v-model.number="f.months" outlined dense type="number" label="Months back" data-test="f-months" />
          </template>
          <template v-else-if="f.kind === 'matrix'">
            <q-input v-model="f.url" outlined dense label="Homeserver" hint="For example https://matrix.example.org" data-test="f-url" />
            <q-input v-model="f.user" outlined dense label="User" hint="@you:example.org, or just your name on that homeserver" autocomplete="off" data-test="f-user" />
            <q-input v-model="f.secret" outlined dense type="password" label="Password" autocomplete="new-password" data-test="f-secret" />
            <p class="ph-muted ph-small">Cloud Companion signs in as a device of its own, encryption included. Your other Matrix apps may ask you to verify it.</p>
          </template>
          <template v-else-if="f.kind === 'xmpp'">
            <q-input v-model="f.user" outlined dense label="Address" hint="you@example.org" autocomplete="off" data-test="f-user" />
            <q-input v-model="f.secret" outlined dense type="password" label="Password" autocomplete="new-password" data-test="f-secret" />
            <q-input v-model="f.xmppServer" outlined dense label="Server (optional)" hint="host or host:port, only when it is not found from the address" data-test="f-xmpp-server" />
            <q-checkbox v-model="f.insecure" dense label="Accept a certificate that cannot be checked (a server of your own)" data-test="f-insecure" />
            <q-input v-model="f.rooms" outlined dense label="Group chats (optional)" hint="Comma separated, e.g. team@conference.example.org" data-test="f-rooms" />
            <q-input v-if="f.rooms.trim()" v-model="f.nick" outlined dense label="Your name in them (optional)" :hint="'Default: ' + (f.user.split('@')[0] || 'the first part of your address')" data-test="f-nick" />
            <p class="ph-muted ph-small">Cloud Companion signs in as one more device of your account and stays online to receive your messages. A group chat reaches your devices as <code>#team</code>.</p>
          </template>
          <template v-else-if="f.kind === 'telegram'">
            <p class="ph-muted ph-small">Telegram needs an API ID and hash of your own: <a href="https://my.telegram.org" target="_blank" rel="noopener">my.telegram.org</a> → API development tools. Cloud Companion then signs in as one more device of your account.</p>
            <q-input v-model.number="f.apiId" outlined dense type="number" label="API ID" data-test="f-tg-api-id" />
            <q-input v-model="f.secret" outlined dense type="password" label="API hash" autocomplete="off" data-test="f-secret" />
            <q-btn outline color="primary" no-caps icon="sym_o_qr_code_2" label="Sign in with a QR code" :disable="!f.apiId || !f.secret" :loading="tg.state === 'starting'" data-test="f-tg-signin" @click="tgStart" />
            <div v-if="tg.state === 'waiting' && tg.png" class="ph-qr" data-test="f-tg-qr">
              <img :src="'data:image/png;base64,' + tg.png" alt="QR code to sign Cloud Companion in to Telegram" width="220" height="220" />
              <p class="ph-small">On your phone: Telegram → Settings → Devices → Link Desktop Device, and scan this. The code changes every 30 seconds.</p>
            </div>
            <div v-if="tg.state === 'password'" class="ph-row ph-row--start" data-test="f-tg-password-row">
              <q-input v-model="tg.password" outlined dense type="password" label="Two-step verification password" autocomplete="off" data-test="f-tg-password" @keyup.enter="tgPassword" />
              <q-btn unelevated color="primary" no-caps label="Continue" :disable="!tg.password" data-test="f-tg-password-send" @click="tgPassword" />
            </div>
            <p v-if="tg.state === 'done'" class="ph-ok" role="status" data-test="f-tg-done"><q-icon name="sym_o_check_circle" size="20px" aria-hidden="true" /> Signed in as {{ tg.name }}. Test, then Add.</p>
            <p v-if="tg.error" class="ph-error ph-small" role="alert" data-test="f-tg-error">{{ tg.error }}</p>
            <p class="ph-muted ph-small">Usernames, <code>u123</code> (people), <code>g123</code> and <code>c123</code> (groups) reach it with a rule such as <code>tg.</code> → this account.</p>
          </template>
          <template v-else-if="f.kind === 'signal'">
            <p class="ph-muted ph-small">Signal needs signal-cli-rest-api next to Cloud Companion: <code>docker compose --profile signal up -d</code>. Link it once as a device of your Signal account, then give your number.</p>
            <q-input v-model="f.url" outlined dense label="signal-cli-rest-api" hint="With the compose profile: http://signal-cli:8080" data-test="f-url" />
            <q-btn outline color="primary" no-caps icon="sym_o_qr_code_2" label="Link this device" :loading="busy === 'link'" data-test="f-signal-link" @click="signalLink" />
            <div v-if="qr" class="ph-qr" data-test="f-signal-qr">
              <img :src="'data:image/png;base64,' + qr" alt="QR code to link Cloud Companion" width="220" height="220" />
              <p class="ph-small">On your phone: Signal → Settings → Linked devices → Link a new device, and scan this. Then Test.</p>
            </div>
            <q-input v-model="f.user" outlined dense label="Your number" hint="With the country code, e.g. +306912345678" autocomplete="off" data-test="f-user" />
            <p class="ph-muted ph-small">Numbers go out as SMS unless a rule sends them here: add a rule such as <code>sig.</code> → this account and write to <code>sig.+30…</code>.</p>
          </template>
          <template v-else-if="f.kind === 'nctalk'">
            <q-input v-model="f.url" outlined dense label="Nextcloud address" hint="For example https://cloud.example.org" data-test="f-url" />
            <q-input v-model="f.user" outlined dense label="User" autocomplete="off" data-test="f-user" />
            <q-input v-model="f.secret" outlined dense type="password" label="App password" hint="Nextcloud → Settings → Security → Create new app password" autocomplete="new-password" data-test="f-secret" />
            <p class="ph-muted ph-small">Cloud Companion looks at your Talk conversations every few seconds and passes new messages on.</p>
          </template>
          <template v-else-if="f.kind === 'irc'">
            <q-input v-model="f.ircServer" outlined dense label="Server" hint="For example irc.libera.chat (TLS, port 6697), or host:port" data-test="f-irc-server" />
            <q-input v-model="f.user" outlined dense label="Nick" autocomplete="off" data-test="f-user" />
            <q-input v-model="f.secret" outlined dense type="password" label="Password (optional)" hint="Your account's (SASL); empty without an account" autocomplete="new-password" data-test="f-secret" />
            <q-input v-model="f.channels" outlined dense label="Channels" hint="Comma separated, e.g. #team, #ops" data-test="f-channels" />
            <q-checkbox v-model="f.ircAll" dense label="Pass on every channel line, not only those that name you" data-test="f-irc-all" />
            <q-checkbox v-model="f.insecure" dense label="Accept a certificate that cannot be checked (a server of your own)" data-test="f-insecure" />
            <q-checkbox v-model="f.ircServerPass" dense label="The password is the server's (a bouncer such as ZNC)" data-test="f-irc-server-pass" />
            <p class="ph-muted ph-small">Cloud Companion stays connected with your nick, so people can write to you while your phone is away.</p>
          </template>
          <template v-else-if="f.kind === 'sip'">
            <q-input v-model="f.sipServer" outlined dense label="Server" hint="host or host:port; with the asterisk profile: asterisk" data-test="f-sip-server" />
            <q-input v-model="f.user" outlined dense label="User name" autocomplete="off" data-test="f-user" />
            <q-input v-model="f.secret" outlined dense type="password" label="Password" autocomplete="new-password" data-test="f-secret" />
            <q-input v-model="f.sipDomain" outlined dense label="SIP domain (optional)" hint="Only when addresses there are not @ the server's name" data-test="f-sip-domain" />
            <div class="ph-two">
              <q-select v-model="f.sipTransport" :options="sipTransports" emit-value map-options outlined dense label="Transport" data-test="f-sip-transport" />
              <q-input v-model="f.sipListen" outlined dense label="Messages arrive on" hint="Cloud Companion's address for this service" data-test="f-sip-listen" />
            </div>
          </template>
          <template v-else-if="f.kind === 'ics'">
            <q-input v-model="f.url" outlined dense label="Calendar link (https:// or webcal://)" hint="A read-only calendar feed, for example a Proton calendar share link" data-test="f-url" />
          </template>
          <template v-else>
            <q-input v-model="f.url" outlined dense :label="f.kind === 'carddav' ? 'Address book URL' : 'Calendar URL'" data-test="f-url" />
            <q-input v-model="f.user" outlined dense label="User name" autocomplete="off" data-test="f-user" />
            <q-input v-model="f.secret" outlined dense type="password" label="Password or app password" autocomplete="new-password" data-test="f-secret" />
          </template>
          <p v-if="testResult" :class="testResult.ok ? 'ph-ok' : 'ph-error'" role="status" data-test="test-result">
            <q-icon :name="testResult.ok ? 'sym_o_check_circle' : 'sym_o_error'" size="20px" aria-hidden="true" /> {{ testResult.detail }}
          </p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" @click="adding = false" />
          <q-btn outline color="primary" label="Test" :loading="busy === 'test'" data-test="f-test" @click="doTest" />
          <q-btn unelevated color="primary" label="Add" :loading="busy === 'add'" data-test="f-add" @click="doAdd" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog :model-value="!!oauthFor" @update:model-value="oauthFor = null">
      <q-card class="ph-dialog" data-test="oauth-dialog">
        <q-card-section class="ph-form">
          <h2 class="ph-h3">Sign in with {{ oauthFor && oauthFor.label }}</h2>
          <p class="ph-muted">You will be sent to {{ oauthFor && oauthFor.label }} to allow access. Cloud Companion never sees your password and keeps only a revocable token on your server.</p>
          <q-input v-model="oauthName" outlined dense label="Name" hint="For example: Personal Gmail" data-test="oauth-name" />
          <q-option-group v-model="oauthKinds" type="checkbox" :options="oauthKindOptions" data-test="oauth-kinds" />
          <p v-if="oauthFor && oauthFor.id === 'microsoft'" class="ph-muted ph-small">Outlook contacts and calendar are read through Microsoft Graph (read-only: Contacts.Read, Calendars.Read).</p>
          <p v-if="oauthError" class="ph-error" role="alert" data-test="oauth-error">{{ oauthError }}</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" @click="oauthFor = null" />
          <q-btn unelevated color="primary" label="Continue" :disable="!oauthKinds.length" data-test="oauth-go" @click="startOAuth" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog :model-value="!!devPreview" @update:model-value="devPreview = null">
      <q-card class="ph-dialog" data-test="devmerge-dialog">
        <q-card-section>
          <h2 class="ph-h3">Merge on {{ devPreview && devSource }}?</h2>
          <p class="ph-muted">This changes the contact list on that account itself. Exactly this will happen:</p>
          <ul class="ph-changes" data-test="devmerge-changes"><li v-for="(c, i) in (devPreview ? devPreview.changes : [])" :key="i">{{ c }}</li></ul>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" @click="devPreview = null" />
          <q-btn unelevated color="primary" label="Merge on the account" data-test="devmerge-confirm" @click="applyDevice" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog :model-value="!!askRemove" @update:model-value="askRemove = null">
      <q-card class="ph-dialog" data-test="remove-dialog">
        <q-card-section>
          <h2 class="ph-h3">Remove this account?</h2>
          <p v-if="askRemove && ['sip', 'matrix', 'xmpp', 'irc', 'nctalk', 'signal', 'telegram'].includes(askRemove.kind)">{{ askRemove.name }} will no longer pass messages; rules that send to it stop working until you change them.</p>
          <p v-else>{{ askRemove && askRemove.name }} will no longer be copied. What was already copied stays on your server.</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" @click="askRemove = null" />
          <q-btn unelevated color="negative" label="Remove" data-test="remove-confirm" @click="doRemove" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { apiCall } from '../cloud'
import RoutingSection from '../components/RoutingSection.vue'
import type { CheckResult, ConnAccount, DevMergePreview, DupSuggestion, OAuthProvider } from '../proto'

const accounts = ref<ConnAccount[]>([])
const hubOn = ref(true)
const error = ref('')
const syncing = ref('')
const askRemove = ref<ConnAccount | null>(null)
const adding = ref(false)
const busy = ref('')
const testResult = ref<CheckResult | null>(null)
const dups = ref<DupSuggestion[] | null>(null)
const dupError = ref('')
const finding = ref(false)
const devPreview = ref<DevMergePreview | null>(null)
const devSource = ref('')
const undoable = ref<{ id: string; source: string } | null>(null)
const providers = ref<OAuthProvider[]>([])
const oauthFor = ref<OAuthProvider | null>(null)
const oauthName = ref('')
const oauthKinds = ref(['mail'])
const oauthError = ref('')
const oauthMsg = ref<{ ok: boolean; text: string } | null>(null)
const route = useRoute()
const oauthKindOptions = computed(() => (oauthFor.value ? oauthFor.value.kinds : []).map((k) => ({ value: k, label: { mail: 'Mail', contacts: 'Contacts', calendar: 'Calendar' }[k] })))
let timer: ReturnType<typeof setInterval> | undefined

const kinds = [{ label: 'Mail (IMAP + SMTP)', value: 'imap' }, { label: 'Contacts (CardDAV)', value: 'carddav' }, { label: 'Calendar (CalDAV)', value: 'caldav' }, { label: 'Calendar link (ICS subscription)', value: 'ics' }, { label: 'SIP service (Asterisk, a provider)', value: 'sip' }, { label: 'Matrix', value: 'matrix' }, { label: 'XMPP (Jabber)', value: 'xmpp' }, { label: 'IRC', value: 'irc' }, { label: 'Nextcloud Talk', value: 'nctalk' }, { label: 'Signal', value: 'signal' }, { label: 'Telegram', value: 'telegram' }]
const sipTransports = [{ label: 'UDP', value: 'udp' }, { label: 'TCP', value: 'tcp' }]
const securities = [{ label: 'TLS (usually port 993 / 465)', value: 'tls' }, { label: 'STARTTLS (usually 143 / 587)', value: 'starttls' }, { label: 'None (this network only)', value: 'plain' }]
const blank = () => ({ kind: 'imap', name: '', imapHost: '', imapPort: 993, imapTLS: 'tls', user: '', secret: '', smtpHost: '', smtpPort: 465, smtpTLS: 'tls', from: '', folders: 'INBOX', months: 3, url: '', pin: '', sipServer: '', sipDomain: '', sipTransport: 'udp', sipListen: ':5070', xmppServer: '', rooms: '', nick: '', insecure: false, apiId: 0, ircServer: '', channels: '', ircAll: false, ircServerPass: false })
const f = reactive(blank())

const KINDS: Record<string, string> = { imap: 'Mail', carddav: 'Contacts', caldav: 'Calendar', ics: 'Calendar link', sip: 'SIP service', matrix: 'Matrix', xmpp: 'XMPP', irc: 'IRC', nctalk: 'Nextcloud Talk', signal: 'Signal', telegram: 'Telegram' }
const kindText = (k: string) => KINDS[k] || k
const STATUS: Record<ConnAccount['status'], string> = { new: 'Not synced yet', ok: 'Connected', error: 'Problem', syncing: 'Syncing' }
const statusText = (a: ConnAccount) => STATUS[a.status] || a.status
const short = (n: string) => (n || '').split(' ')[0] || 'this one'
function ago(t: number) {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - t))
  if (s < 90) return 'just now'
  if (s < 5400) return Math.round(s / 60) + ' min ago'
  if (s < 129600) return Math.round(s / 3600) + ' h ago'
  return Math.round(s / 86400) + ' days ago'
}

async function load() {
  try { const j = await apiCall('GET', '/phe/connections'); accounts.value = j.accounts; hubOn.value = j.hub } catch (e) { error.value = (e as Error).message }
}
function openAdd() { Object.assign(f, blank()); qr.value = ''; tgReset(); testResult.value = null; adding.value = true }

function payload() {
  if (f.kind === 'imap') {
    const cfg = {
      imap: { host: f.imapHost.trim(), port: f.imapPort, tls: f.imapTLS, user: f.user.trim(), pin: f.pin.trim() },
      smtp: f.smtpHost.trim() ? { host: f.smtpHost.trim(), port: f.smtpPort, tls: f.smtpTLS, user: f.user.trim(), pin: f.pin.trim() } : { host: '', port: 0, tls: 'tls', user: '' },
      folders: f.folders.split(',').map((x) => x.trim()).filter(Boolean), months: f.months || 3, from: f.from.trim(),
    }
    return { kind: 'imap', provider: 'generic', name: f.name, config: JSON.stringify(cfg), secret: f.secret }
  }
  if (f.kind === 'matrix') return { kind: 'matrix', provider: 'generic', name: f.name, config: JSON.stringify({ homeserver: f.url.trim(), user: f.user.trim() }), secret: f.secret }
  if (f.kind === 'xmpp') return { kind: 'xmpp', provider: 'generic', name: f.name, config: JSON.stringify({ jid: f.user.trim(), server: f.xmppServer.trim(), insecure: f.insecure, rooms: f.rooms.split(',').map((x) => x.trim()).filter(Boolean), nick: f.nick.trim() }), secret: f.secret }
  if (f.kind === 'telegram') return { kind: 'telegram', provider: 'generic', name: f.name, config: JSON.stringify({ api_id: Number(f.apiId) || 0, user_id: tg.userId }), secret: f.secret }
  if (f.kind === 'signal') return { kind: 'signal', provider: 'generic', name: f.name, config: JSON.stringify({ api: f.url.trim(), number: f.user.trim() }), secret: '' }
  if (f.kind === 'nctalk') return { kind: 'nctalk', provider: 'generic', name: f.name, config: JSON.stringify({ url: f.url.trim(), user: f.user.trim() }), secret: f.secret }
  if (f.kind === 'irc') {
    const cfg = { server: f.ircServer.trim(), nick: f.user.trim(), channels: f.channels.split(',').map((x) => x.trim()).filter(Boolean), all: f.ircAll, insecure: f.insecure, server_pass: f.ircServerPass }
    return { kind: 'irc', provider: 'generic', name: f.name, config: JSON.stringify(cfg), secret: f.secret }
  }
  if (f.kind === 'sip') {
    const cfg = { server: f.sipServer.trim(), domain: f.sipDomain.trim(), user: f.user.trim(), transport: f.sipTransport, listen: f.sipListen.trim() }
    return { kind: 'sip', provider: 'generic', name: f.name, config: JSON.stringify(cfg), secret: f.secret }
  }
  if (f.kind === 'ics') return { kind: 'ics', provider: 'generic', name: f.name, config: JSON.stringify({ url: f.url.trim() }), secret: '' }
  return { kind: f.kind, provider: 'generic', name: f.name, config: JSON.stringify({ url: f.url.trim(), user: f.user.trim() }), secret: f.secret }
}
const qr = ref('')
// Telegram's QR sign-in, run by the server; the dialog asks how it is going every 1.5 s
const tg = reactive({ id: '', state: '', png: '', error: '', userId: 0, name: '', password: '' })
let tgTimer: ReturnType<typeof setTimeout> | undefined
function tgReset() { clearTimeout(tgTimer); Object.assign(tg, { id: '', state: '', png: '', error: '', userId: 0, name: '', password: '' }) }
async function tgPoll() {
  if (!tg.id) return
  try {
    const s = await apiCall('GET', '/phe/telegram/login?id=' + encodeURIComponent(tg.id))
    Object.assign(tg, { state: s.state, png: s.png || '', error: s.error || '', userId: s.user_id || 0, name: s.name || '' })
  } catch (e) { tg.state = 'failed'; tg.error = (e as Error).message }
  if (tg.state !== 'done' && tg.state !== 'failed') tgTimer = setTimeout(tgPoll, 1500)
}
async function tgStart() {
  tgReset(); tg.state = 'starting'; testResult.value = null // an earlier "not signed in yet" no longer applies
  try { tg.id = (await apiCall('POST', '/phe/telegram/login', { api_id: Number(f.apiId) || 0, api_hash: f.secret.trim() })).id; tgPoll() } catch (e) { tg.state = 'failed'; tg.error = (e as Error).message }
}
async function tgPassword() {
  try { await apiCall('POST', '/phe/telegram/login/password', { id: tg.id, password: tg.password }); tg.password = ''; tg.error = '' } catch (e) { tg.error = (e as Error).message }
}
async function signalLink() {
  busy.value = 'link'; testResult.value = null; qr.value = ''
  try { qr.value = (await apiCall('POST', '/phe/signal/link', { api: f.url.trim() })).png } catch (e) { testResult.value = { ok: false, detail: (e as Error).message } } finally { busy.value = '' }
}
async function doTest() {
  busy.value = 'test'; testResult.value = null
  try { testResult.value = await apiCall('POST', '/phe/connections/test', payload()) } catch (e) { testResult.value = { ok: false, detail: (e as Error).message } } finally { busy.value = '' }
}
async function doAdd() {
  busy.value = 'add'; testResult.value = null
  try { await apiCall('POST', '/phe/connections', payload()); adding.value = false; await load() } catch (e) { testResult.value = { ok: false, detail: (e as Error).message } } finally { busy.value = '' }
}
async function sync(a: ConnAccount) {
  syncing.value = a.id; error.value = ''
  try { const r = await apiCall('POST', '/phe/connections/sync', { id: a.id }); if (!r.ok) error.value = a.name + ': ' + r.detail } catch (e) { error.value = (e as Error).message } finally { syncing.value = ''; await load() }
}
async function doRemove() {
  const a = askRemove.value
  askRemove.value = null
  if (!a) return
  try { await apiCall('POST', '/phe/connections/remove', { id: a.id }) } catch (e) { error.value = (e as Error).message }
  await load()
}
async function findDups() {
  finding.value = true; dupError.value = ''
  try { dups.value = (await apiCall('GET', '/phe/duplicates')).suggestions } catch (e) { dupError.value = (e as Error).message } finally { finding.value = false }
}
async function merge(d: DupSuggestion, keep: string, drop: string) {
  dupError.value = ''
  try { await apiCall('POST', '/phe/duplicates/merge', { keep, drop }); await findDups() } catch (e) { dupError.value = (e as Error).message }
}
function openOAuth(p: OAuthProvider) { oauthFor.value = p; oauthName.value = ''; oauthKinds.value = ['mail']; oauthError.value = '' }
async function startOAuth() {
  oauthError.value = ''
  try {
    const j = await apiCall('POST', '/phe/oauth/start', { provider: oauthFor.value!.id, kinds: oauthKinds.value, name: oauthName.value })
    window.location.href = j.url
  } catch (e) { oauthError.value = (e as Error).message }
}
async function previewDevice(d: DupSuggestion) {
  dupError.value = ''
  try { devPreview.value = await apiCall('POST', '/phe/devmerge/preview', { account: d.same_source, a: d.a, b: d.b }); devSource.value = d.source_name || '' } catch (e) { dupError.value = (e as Error).message }
}
async function applyDevice() {
  const p = devPreview.value
  devPreview.value = null
  if (!p) return
  try {
    await apiCall('POST', '/phe/devmerge/apply', { id: p.id })
    undoable.value = { id: p.id, source: devSource.value }
    setTimeout(() => { if (undoable.value && undoable.value.id === p.id) undoable.value = null }, 10 * 60 * 1000)
    await findDups()
  } catch (e) { dupError.value = (e as Error).message }
}
async function undoDevice() {
  const u = undoable.value
  if (!u) return
  try { await apiCall('POST', '/phe/devmerge/undo', { id: u.id }); undoable.value = null; await findDups() } catch (e) { dupError.value = (e as Error).message }
}
async function dismiss(d: DupSuggestion) {
  try { await apiCall('POST', '/phe/duplicates/dismiss', { a: d.a, b: d.b }); dups.value = (dups.value || []).filter((x) => x !== d) } catch (e) { dupError.value = (e as Error).message }
}

onMounted(async () => {
  load()
  timer = setInterval(load, 10000)
  try { providers.value = (await apiCall('GET', '/phe/oauth/providers')).providers } catch (e) { providers.value = [] }
  if (route.query.oauth === 'ok') oauthMsg.value = { ok: true, text: 'Signed in. The new accounts are syncing.' }
  else if (route.query.oauth === 'error') oauthMsg.value = { ok: false, text: String(route.query.msg || 'The sign-in did not complete.') }
})
onBeforeUnmount(() => { clearInterval(timer); tgReset() })
watch(adding, (open) => { if (!open) tgReset() }) // a closed dialog stops asking about its sign-in
</script>

<style scoped>
.ph-section { margin-top: var(--ph-s4); display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-section p { margin: 0; }
.ph-row { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); flex-wrap: wrap; }
.ph-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--ph-s2); }
.ph-list li { display: flex; align-items: center; gap: var(--ph-s2); padding: var(--ph-s3); border: 1px solid var(--ph-border); border-radius: var(--ph-r-control); flex-wrap: wrap; }
.ph-grow { flex: 1 1 220px; min-width: 0; }
.ph-chip { display: inline-block; margin-left: var(--ph-s2); padding: 2px var(--ph-s2); font-size: var(--ph-text-small); background: var(--ph-surface-2, var(--ph-bg)); }
.ph-ok { color: var(--ph-positive, #1b7f3b); display: flex; gap: var(--ph-s2); align-items: flex-start; }
.ph-error { display: flex; gap: var(--ph-s2); align-items: flex-start; color: var(--ph-negative); }
.ph-note { display: flex; gap: var(--ph-s2); align-items: flex-start; padding: var(--ph-s3); border-radius: var(--ph-r-control); background: var(--ph-surface-2, var(--ph-bg)); }
.ph-dialog { width: min(94vw, 480px); background: var(--ph-surface); color: var(--ph-text); max-height: 90vh; overflow: auto; }
.ph-form { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-two { display: grid; grid-template-columns: 1fr 110px; gap: var(--ph-s2); }
.ph-oauth { display: flex; flex-wrap: wrap; gap: var(--ph-s2); }
.ph-changes { margin: var(--ph-s2) 0 0; padding-left: var(--ph-s4); display: flex; flex-direction: column; gap: var(--ph-s1, 4px); }
.ph-h3 { font-size: var(--ph-text-h3, 1.1rem); margin: 0; }
.ph-qr { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
.ph-qr img { background: #fff; padding: 8px; border-radius: 8px; }
</style>
