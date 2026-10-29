<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <h1>Catalogue</h1>
    <p class="ph-muted">
      Host your own software list for Catalogue on your phone: upload installation files for any platform, and merge in other lists (the official one, a friend's
      server). Your phone reads a list from the address shown under it.
    </p>

    <section class="ph-card ph-section" aria-labelledby="cats-h">
      <div class="ph-row">
        <h2 id="cats-h">Your catalogues</h2>
        <q-btn unelevated color="primary" icon="sym_o_add" label="New catalogue" data-test="cat-new" @click="creating = true" />
      </div>
      <p v-if="!cats.length" class="ph-muted" data-test="no-cats">No catalogue yet.</p>
      <ul v-else class="ph-list" aria-label="Catalogues">
        <li v-for="c in cats" :key="c.id" data-test="cat-row">
          <div class="ph-grow">
            <strong>{{ c.name }}</strong>
            <span class="ph-chip">{{ c.public ? 'Public' : 'Private (token)' }}</span>
            <div class="ph-small ph-muted">{{ c.hosted_apps }} uploaded app(s), {{ c.sources }} source(s)</div>
            <div class="ph-small" data-test="cat-url">Address for Catalogue: <code>{{ c.index_url }}</code></div>
          </div>
          <q-btn flat no-caps color="primary" label="Open" data-test="cat-open" @click="open(c)" />
          <q-btn flat round color="negative" icon="sym_o_delete" :aria-label="'Remove ' + c.name" data-test="cat-remove" @click="askRemove = c" />
        </li>
      </ul>
      <p v-if="error" class="ph-error" role="alert" data-test="cat-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ error }}</p>
    </section>

    <template v-if="cur">
      <section class="ph-card ph-section" aria-labelledby="set-h">
        <h2 id="set-h">{{ cur.name }}: settings</h2>
        <q-toggle :model-value="cur.public" label="Public (anyone with the address can read it)" data-test="cat-public" @update:model-value="setPublic" />
        <div v-if="!cur.public" class="ph-small">
          Private: the address contains a secret token. Anyone with it can read the list.
          <q-btn flat no-caps color="primary" label="Make a new token" data-test="cat-rotate" @click="rotate" />
        </div>
        <div class="ph-two">
          <q-input v-model.number="cur.priority" outlined dense type="number" label="Priority of your own uploads" hint="Higher wins when the same app is in several places" data-test="cat-priority" />
          <q-input v-model.number="syncHours" outlined dense type="number" label="Sync every (hours, 0 = only by hand)" data-test="cat-interval" />
        </div>
        <q-btn outline color="primary" no-caps label="Save" data-test="cat-save" @click="saveSettings" />
      </section>

      <section class="ph-card ph-section" aria-labelledby="up-h">
        <h2 id="up-h">Upload an app</h2>
        <div class="ph-form">
          <q-file v-model="u.file" outlined dense label="Installation file (.sisx, .apk, .exe, .deb, .zip ...)" data-test="up-file" />
          <div class="ph-two">
            <q-input v-model="u.app_id" outlined dense label="App id" hint="lower case, for example my-app" data-test="up-id" />
            <q-input v-model="u.version" outlined dense label="Version" data-test="up-version" />
          </div>
          <q-input v-model="u.name" outlined dense label="Name" data-test="up-name" />
          <q-input v-model="u.summary" outlined dense label="One-line summary" data-test="up-summary" />
          <q-input v-model="u.description" outlined dense type="textarea" label="Description (optional)" data-test="up-desc" />
          <div class="ph-two">
            <q-select v-model="u.platform" :options="platforms" emit-value map-options outlined dense label="Platform" data-test="up-platform" />
            <q-input v-model="u.arch" outlined dense label="Processors" hint="arm, x32 | aarch64, x86_64 ... (empty = default)" data-test="up-arch" />
          </div>
          <q-input v-model="u.boards" outlined dense label="Only these devices (optional)" hint="board names, comma separated" data-test="up-boards" />
          <q-file v-model="u.icon" outlined dense label="Icon (optional, PNG)" data-test="up-icon" />
          <q-btn unelevated color="primary" icon="sym_o_upload" label="Upload" :loading="busy === 'up'" data-test="up-go" @click="upload" />
          <p v-if="upMsg" :class="upMsg.ok ? 'ph-ok' : 'ph-error'" role="status" data-test="up-result">{{ upMsg.text }}</p>
        </div>
      </section>

      <section class="ph-card ph-section" aria-labelledby="src-h">
        <div class="ph-row">
          <h2 id="src-h">Sources</h2>
          <q-btn outline color="primary" no-caps icon="sym_o_sync" label="Sync all now" :loading="busy === 'sync'" data-test="src-sync-all" @click="syncAll" />
          <q-btn unelevated color="primary" icon="sym_o_add" label="Add a source" data-test="src-new" @click="addingSrc = true" />
        </div>
        <p class="ph-muted">Other catalogues merged into this one. When an app is in several places, the source with the higher priority wins.</p>
        <p v-if="!sources.length" class="ph-muted" data-test="no-sources">No source yet.</p>
        <ul v-else class="ph-list" aria-label="Sources">
          <li v-for="s in sources" :key="s.id" data-test="src-row">
            <div class="ph-grow">
              <strong>{{ s.name }}</strong>
              <span class="ph-chip">{{ s.enabled ? 'On' : 'Off' }}</span>
              <div class="ph-small ph-muted">{{ s.url }}</div>
              <div class="ph-small">{{ s.items }} app(s), {{ s.mirrored_files }} file(s) kept here · {{ s.last_sync ? 'synced ' + new Date(s.last_sync * 1000).toLocaleString() : 'not synced yet' }}</div>
              <div v-if="s.last_error" class="ph-error ph-small" role="alert">{{ s.last_error }}</div>
              <div v-for="m in s.mirror_problems" :key="m.sha256" class="ph-error ph-small" role="alert">File {{ m.sha256.slice(0, 12) }}: {{ m.state }} {{ m.error }}</div>
              <div class="ph-two">
                <q-input v-model.number="s.priority" outlined dense type="number" label="Priority" data-test="src-prio" @change="patchSrc(s, { priority: s.priority })" />
                <q-toggle :model-value="s.mirror" label="Keep copies of its files here" data-test="src-mirror" @update:model-value="(v) => patchSrc(s, { mirror: v })" />
              </div>
            </div>
            <q-btn flat round icon="sym_o_sync" :aria-label="'Sync ' + s.name" data-test="src-sync" @click="sync(s)" />
            <q-btn flat round :icon="s.enabled ? 'sym_o_pause' : 'sym_o_play_arrow'" :aria-label="(s.enabled ? 'Turn off ' : 'Turn on ') + s.name" data-test="src-toggle" @click="patchSrc(s, { enabled: !s.enabled })" />
            <q-btn flat round color="negative" icon="sym_o_delete" :aria-label="'Remove ' + s.name" data-test="src-remove" @click="removeSrc(s)" />
          </li>
        </ul>
      </section>

      <section class="ph-card ph-section" aria-labelledby="apps-h">
        <h2 id="apps-h">What the catalogue offers</h2>
        <p v-if="!apps.length" class="ph-muted" data-test="no-apps">Nothing yet.</p>
        <ul v-else class="ph-list" aria-label="Apps">
          <li v-for="a in apps" :key="a.id" data-test="app-row">
            <div class="ph-grow">
              <strong>{{ a.name }}</strong> <span class="ph-small ph-muted">{{ a.id }}</span>
              <div class="ph-small">From <strong>{{ a.winner.source }}</strong>, version {{ a.winner.version }}<span v-if="a.winner.mirrored"> (copy kept here)</span></div>
              <div v-for="o in a.others || []" :key="o.source_id" class="ph-small ph-muted" data-test="app-other">Also in {{ o.source }}: {{ o.version }} ({{ o.reason }})</div>
            </div>
          </li>
        </ul>
        <h3 class="ph-h3">Your uploads</h3>
        <ul class="ph-list" aria-label="Uploaded releases">
          <li v-for="r in releases" :key="r.app_id + r.version" data-test="rel-row">
            <div class="ph-grow">{{ r.name }} {{ r.version }} <span class="ph-chip">{{ r.platform }} / {{ r.package }}</span>
              <div class="ph-small ph-muted">{{ r.file.name }}, {{ r.file.size }} bytes, sha256 {{ r.file.sha256.slice(0, 16) }}...</div></div>
            <q-btn flat round color="negative" icon="sym_o_delete" :aria-label="'Remove ' + r.name + ' ' + r.version" data-test="rel-remove" @click="removeRel(r)" />
          </li>
        </ul>
      </section>
    </template>

    <q-dialog v-model="creating">
      <q-card class="ph-dialog" data-test="cat-dialog">
        <q-card-section class="ph-form">
          <h2 class="ph-h3">New catalogue</h2>
          <q-input v-model="nc.id" outlined dense label="Id" hint="lower case letters, digits, - and ." data-test="nc-id" />
          <q-input v-model="nc.name" outlined dense label="Name" data-test="nc-name" />
          <q-toggle v-model="nc.public" label="Public (no token needed)" data-test="nc-public" />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" v-close-popup />
          <q-btn unelevated color="primary" no-caps label="Create" data-test="nc-go" @click="create" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog v-model="addingSrc">
      <q-card class="ph-dialog" data-test="src-dialog">
        <q-card-section class="ph-form">
          <h2 class="ph-h3">Add a source</h2>
          <q-input v-model="ns.url" outlined dense label="Address of its index.json" hint="https://phorepoc.github.io/index/index.json is the official list" data-test="ns-url" />
          <q-input v-model="ns.name" outlined dense label="Name" data-test="ns-name" />
          <q-input v-model="ns.token" outlined dense type="password" autocomplete="off" label="Token (only if the source is private and the address has none)" data-test="ns-token" />
          <q-input v-model.number="ns.priority" outlined dense type="number" label="Priority" data-test="ns-prio" />
          <q-toggle v-model="ns.mirror" label="Keep copies of its files on this server" data-test="ns-mirror" />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" v-close-popup />
          <q-btn unelevated color="primary" no-caps label="Add" data-test="ns-go" @click="addSource" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog :model-value="!!askRemove" @update:model-value="askRemove = null">
      <q-card class="ph-dialog">
        <q-card-section>Remove the catalogue "{{ askRemove && askRemove.name }}", its uploads and sources? Phones that read it will stop seeing it.</q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" v-close-popup />
          <q-btn unelevated color="negative" no-caps label="Remove" data-test="cat-remove-yes" @click="removeCat" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { apiCall, apiUpload } from '../cloud'
import type { CatAppReport, CatRelease, CatSourceInfo, CatSyncResult, CatalogueInfo } from '../proto'

/** The upload form: the installation file and icon, and the text fields (empty = taken from the file). */
interface UploadForm { file: File | null; icon: File | null; app_id: string; version: string; name: string; summary: string; description: string; platform: string; arch: string; boards: string }
type UploadText = Exclude<keyof UploadForm, 'file' | 'icon'>

const cats = ref<CatalogueInfo[]>([])
const cur = ref<CatalogueInfo | null>(null)
const syncHours = ref(6)
const sources = ref<CatSourceInfo[]>([])
const apps = ref<CatAppReport[]>([])
const releases = ref<CatRelease[]>([])
const error = ref('')
const busy = ref('')
const creating = ref(false)
const addingSrc = ref(false)
const askRemove = ref<CatalogueInfo | null>(null)
const upMsg = ref<{ ok: boolean; text: string } | null>(null)
const nc = reactive({ id: '', name: '', public: false })
const ns = reactive({ url: '', name: '', token: '', priority: 50, mirror: false })
const blank = (): UploadForm => ({ file: null, app_id: '', version: '', name: '', summary: '', description: '', platform: '', arch: '', boards: '', icon: null })
const u = reactive(blank())
const platforms = [{ label: 'Detect from the file', value: '' }, { label: 'Symbian', value: 'symbian' }, { label: 'Android', value: 'android' }, { label: 'Windows', value: 'windows' },
  { label: 'Linux', value: 'linux' }, { label: 'macOS', value: 'macos' }, { label: 'Other', value: 'other' }]

async function guard<T>(fn: () => Promise<T>) { try { error.value = ''; return await fn() } catch (e) { error.value = (e as Error).message } }
async function load() {
  await guard(async () => {
    cats.value = (await apiCall('GET', '/phe/catalogues')).catalogues
    if (cur.value) cur.value = cats.value.find((c) => c.id === cur.value!.id) || null
    if (cur.value) await loadCur()
  })
}
async function loadCur() {
  if (!cur.value) return
  const id = encodeURIComponent(cur.value.id)
  syncHours.value = Math.round(cur.value.sync_interval / 3600)
  sources.value = (await apiCall('GET', '/phe/catalogue/sources?catalogue=' + id)).sources
  const a = await apiCall('GET', '/phe/catalogue/apps?catalogue=' + id)
  apps.value = a.apps
  releases.value = a.releases
}
function open(c: CatalogueInfo) { cur.value = { ...c }; guard(loadCur) }
async function create() {
  await guard(async () => {
    const j = await apiCall('POST', '/phe/catalogues', { id: nc.id.trim(), name: nc.name.trim(), public: nc.public })
    creating.value = false
    Object.assign(nc, { id: '', name: '', public: false })
    await load()
    open(j.catalogue)
  })
}
async function patchCat(body: Record<string, unknown>) {
  const id = cur.value?.id
  if (!id) return
  await guard(async () => { await apiCall('POST', '/phe/catalogues/update', { id, ...body }); await load() })
}
const setPublic = (v: boolean) => patchCat({ public: v })
const rotate = () => patchCat({ rotate_token: true })
const saveSettings = () => patchCat({ priority: cur.value?.priority || 0, sync_interval: Math.max(0, Math.round((syncHours.value || 0) * 3600)) })
async function removeCat() {
  const c = askRemove.value
  askRemove.value = null
  if (!c) return
  await guard(async () => { await apiCall('POST', '/phe/catalogues/remove', { id: c.id }); if (cur.value && cur.value.id === c.id) cur.value = null; await load() })
}
async function upload() {
  upMsg.value = null
  if (!u.file) { upMsg.value = { ok: false, text: 'Choose the installation file first.' }; return }
  busy.value = 'up'
  try {
    const f = new FormData()
    f.append('catalogue', cur.value!.id) // the upload form shows only with a catalogue open
    for (const k of ['app_id', 'version', 'name', 'summary', 'description', 'platform', 'arch', 'boards'] as UploadText[]) f.append(k, u[k] || '')
    f.append('file', u.file)
    if (u.icon) f.append('icon', u.icon)
    const j = await apiUpload('/phe/catalogue/upload', f)
    upMsg.value = { ok: true, text: 'Uploaded ' + j.release.name + ' ' + j.release.version + '.' }
    Object.assign(u, blank())
    await loadCur()
  } catch (e) { upMsg.value = { ok: false, text: (e as Error).message } } finally { busy.value = '' }
}
async function removeRel(r: CatRelease) {
  await guard(async () => { await apiCall('POST', '/phe/catalogue/release/remove', { catalogue: cur.value!.id, app_id: r.app_id, version: r.version }); await loadCur() })
}
async function addSource() {
  await guard(async () => {
    await apiCall('POST', '/phe/catalogue/sources', { catalogue: cur.value!.id, url: ns.url.trim(), name: ns.name.trim(), token: ns.token, priority: ns.priority || 0, mirror: ns.mirror })
    addingSrc.value = false
    Object.assign(ns, { url: '', name: '', token: '', priority: 50, mirror: false })
    await loadCur()
  })
}
async function patchSrc(s: CatSourceInfo, body: Record<string, unknown>) {
  await guard(async () => { await apiCall('POST', '/phe/catalogue/sources/update', { id: s.id, ...body }); await loadCur() })
}
async function removeSrc(s: CatSourceInfo) {
  await guard(async () => { await apiCall('POST', '/phe/catalogue/sources/remove', { id: s.id }); await loadCur() })
}
async function runSync(body: { id?: string; catalogue?: string }) {
  busy.value = 'sync'
  await guard(async () => {
    const j = await apiCall('POST', '/phe/catalogue/sources/sync', body)
    const bad = (j.results as CatSyncResult[]).filter((r) => !r.ok)
    if (bad.length) error.value = bad.map((r) => r.name + ': ' + r.detail).join('; ')
    await loadCur()
  })
  busy.value = ''
}
const sync = (s: CatSourceInfo) => runSync({ id: s.id })
const syncAll = () => runSync({ catalogue: cur.value!.id })

onMounted(load)
</script>
