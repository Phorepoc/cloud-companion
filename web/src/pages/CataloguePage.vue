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

      <section class="ph-card ph-section" aria-labelledby="lib-h">
        <div class="ph-row">
          <h2 id="lib-h">Library</h2>
          <q-btn outline color="primary" no-caps icon="sym_o_refresh" label="Rescan" :loading="busy === 'scan'" data-test="lib-scan" @click="rescan" />
          <q-btn unelevated color="primary" no-caps icon="sym_o_upload" label="Upload a file" data-test="lib-upload" @click="uploading = true" />
        </div>
        <p class="ph-muted ph-small" data-test="lib-info">
          Files in <code>{{ libInfo?.path }}</code> are read automatically: name, version, vendor and icon come from the file itself.
          {{ libInfo && libInfo.scan_interval ? 'Rescanned every ' + Math.round(libInfo.scan_interval / 60) + ' minutes' : 'Rescanned only when you press Rescan' }}<span v-if="libInfo && libInfo.last_scan">; last scan {{ new Date(libInfo.last_scan * 1000).toLocaleString() }}</span>.
        </p>
        <p v-if="scanMsg" class="ph-ok ph-small" role="status" data-test="scan-result">{{ scanMsg }}</p>
        <div v-for="e in missing" :key="e.id" class="ph-error ph-small" role="alert" data-test="missing-warning">
          <q-icon name="sym_o_warning" size="18px" aria-hidden="true" /> {{ e.name }}: the file <code>{{ e.path }}</code> is missing, so it was unpublished. Put the file back and rescan, or delete the entry.
        </div>
        <div class="ph-row">
          <q-input v-model="filter" outlined dense clearable label="Search" class="ph-grow" data-test="lib-filter" />
          <q-checkbox :model-value="allSelected" label="Select all shown" data-test="lib-all" @update:model-value="toggleAll" />
        </div>
        <div class="ph-row">
          <q-btn unelevated color="primary" no-caps icon="sym_o_publish" :label="'Publish (' + selected.length + ')'" :disable="!selected.length" data-test="lib-publish" @click="publish(true)" />
          <q-btn outline color="primary" no-caps icon="sym_o_unpublished" label="Unpublish" :disable="!selected.length" data-test="lib-unpublish" @click="publish(false)" />
        </div>
        <p v-if="!entries.length" class="ph-muted" data-test="no-lib">The library is empty. Put .sisx or .apk files in the folder and press Rescan, or upload one.</p>
        <ul v-else class="ph-list" aria-label="Library">
          <li v-for="e in shown" :key="e.id" data-test="lib-row">
            <q-checkbox v-model="selected" :val="e.id" :aria-label="'Select ' + e.name" :disable="e.status === 'missing' && !e.published" />
            <img v-if="e.icon_svg_data || e.icon_data" :src="e.icon_svg_data || e.icon_data" alt="" width="48" height="48" class="ph-icon" data-test="lib-icon" />
            <div v-else class="ph-icon ph-muted ph-small" :title="e.icon_note || 'No icon in the file'">{{ e.icon_note ? 'No icon shown' : '' }}</div>
            <div class="ph-grow">
              <strong>{{ e.name }}</strong> <span class="ph-small ph-muted">{{ e.version }}</span>
              <span class="ph-chip" data-test="lib-status">{{ statusText(e) }}</span>
              <div class="ph-small ph-muted">{{ e.platform }} / {{ e.package }}<span v-if="e.arch && e.arch.length"> / {{ e.arch.join(', ') }}</span> · {{ fmtSize(e.size) }} · {{ e.path }}</div>
              <div v-if="e.meta.vendor" class="ph-small ph-muted">{{ e.meta.vendor }}<span v-if="e.meta.targets && e.meta.targets.length"> · for {{ e.meta.targets.join(', ') }}</span></div>
              <div v-if="e.status !== 'ok'" class="ph-error ph-small" role="alert">{{ e.error }}</div>
              <div v-else-if="e.problem" class="ph-small ph-muted">Cannot be published yet: {{ e.problem }}</div>
              <div v-if="e.icon_note && !e.icon_data" class="ph-small ph-muted">{{ e.icon_note }}</div>
            </div>
            <q-btn flat round icon="sym_o_edit" :aria-label="'Edit ' + e.name" data-test="lib-edit" @click="edit(e)" />
            <q-btn flat round color="negative" icon="sym_o_delete" :aria-label="'Delete ' + e.name" data-test="lib-delete" @click="askDelete = e" />
          </li>
        </ul>
        <p v-if="libError" class="ph-error" role="alert" data-test="lib-error"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ libError }}</p>
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
      </section>
    </template>

    <q-dialog v-model="uploading">
      <q-card class="ph-dialog" data-test="up-dialog">
        <q-card-section class="ph-form">
          <h2 class="ph-h3">Upload a file</h2>
          <p class="ph-small ph-muted">The file goes into the library folder and is read like any other. Name, version and icon come from the file; change them afterwards if you want.</p>
          <q-file v-model="u.file" outlined dense label="Installation file (.sisx, .apk, .exe, .deb, .zip ...)" data-test="up-file" />
          <q-input v-model="u.version" outlined dense label="Version (only for files that do not carry one)" data-test="up-version" />
          <q-toggle v-model="u.publish" label="Publish it in this catalogue now" data-test="up-publish" />
          <p v-if="upMsg" :class="upMsg.ok ? 'ph-ok' : 'ph-error'" role="status" data-test="up-result">{{ upMsg.text }}</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Close" v-close-popup />
          <q-btn unelevated color="primary" no-caps icon="sym_o_upload" label="Upload" :loading="busy === 'up'" data-test="up-go" @click="upload" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog :model-value="!!ed" @update:model-value="ed = null">
      <q-card v-if="ed" class="ph-dialog" data-test="edit-dialog">
        <q-card-section class="ph-form">
          <h2 class="ph-h3">Details of {{ ed.entry.name }}</h2>
          <p class="ph-small ph-muted">Leave a field empty to use what the file says. Your changes are kept when the folder is rescanned, unless the file turns out to be another app. A new version of the same app keeps them, except the version, which is read again.</p>
          <q-input v-model="ed.e.name" outlined dense label="Name" :placeholder="ed.entry.meta.read ? '' : ed.entry.name" data-test="ed-name" />
          <div class="ph-two">
            <q-input v-model="ed.e.app_id" outlined dense label="App id" hint="lower case, for example my-app" data-test="ed-id" />
            <q-input v-model="ed.e.version" outlined dense label="Version" data-test="ed-version" />
          </div>
          <q-input v-model="ed.e.summary" outlined dense label="One-line summary" data-test="ed-summary" />
          <q-input v-model="ed.e.description" outlined dense type="textarea" label="Description" hint="Files do not carry one" data-test="ed-desc" />
          <q-input v-model="ed.e.homepage" outlined dense label="Homepage (optional)" data-test="ed-home" />
          <q-file v-model="ed.icon" outlined dense accept="image/png" label="Replace the icon (PNG)" data-test="ed-icon" />
          <p v-if="ed.error" class="ph-error" role="alert">{{ ed.error }}</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" v-close-popup />
          <q-btn unelevated color="primary" no-caps label="Save" data-test="ed-save" @click="saveEdit" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog :model-value="!!askDelete" @update:model-value="askDelete = null">
      <q-card v-if="askDelete" class="ph-dialog" data-test="del-dialog">
        <q-card-section>
          Delete the entry "{{ askDelete.name }}"?
          <q-checkbox v-if="askDelete.status !== 'missing' && askDelete.kind === 'library'" v-model="delFile" label="Also delete the file from the library folder (otherwise the next scan finds it again)" data-test="del-file" />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat no-caps label="Cancel" v-close-popup />
          <q-btn unelevated color="negative" no-caps label="Delete" data-test="del-yes" @click="deleteEntry" />
        </q-card-actions>
      </q-card>
    </q-dialog>

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
import { ref, reactive, computed, onMounted } from 'vue'
import { apiCall, apiUpload } from '../cloud'
import type { CatAppReport, CatLibEntry, CatLibraryInfo, CatScanResult, CatSourceInfo, CatSyncResult, CatalogueInfo } from '../proto'

type Edits = CatLibEntry['edits']

const cats = ref<CatalogueInfo[]>([])
const cur = ref<CatalogueInfo | null>(null)
const syncHours = ref(6)
const sources = ref<CatSourceInfo[]>([])
const apps = ref<CatAppReport[]>([])
const entries = ref<CatLibEntry[]>([])
const libInfo = ref<CatLibraryInfo | null>(null)
const selected = ref<string[]>([])
const filter = ref('')
const scanMsg = ref('')
const libError = ref('')
const error = ref('')
const busy = ref('')
const creating = ref(false)
const addingSrc = ref(false)
const uploading = ref(false)
const askRemove = ref<CatalogueInfo | null>(null)
const askDelete = ref<CatLibEntry | null>(null)
const delFile = ref(false)
const ed = ref<{ entry: CatLibEntry; e: Edits; icon: File | null; error: string } | null>(null)
const upMsg = ref<{ ok: boolean; text: string } | null>(null)
const nc = reactive({ id: '', name: '', public: false })
const ns = reactive({ url: '', name: '', token: '', priority: 50, mirror: false })
const u = reactive<{ file: File | null; version: string; publish: boolean }>({ file: null, version: '', publish: false })

const shown = computed(() => {
  const f = (filter.value || '').toLowerCase()
  return entries.value.filter((e) => !f || (e.name + ' ' + e.path + ' ' + e.app_id + ' ' + e.platform).toLowerCase().includes(f))
})
const missing = computed(() => entries.value.filter((e) => e.status === 'missing'))
const allSelected = computed(() => shown.value.length > 0 && shown.value.every((e) => selected.value.includes(e.id)))
const toggleAll = (v: boolean) => { selected.value = v ? shown.value.filter((e) => e.status !== 'missing').map((e) => e.id) : [] }
const fmtSize = (n: number) => (n >= 1 << 20 ? (n / (1 << 20)).toFixed(1) + ' MB' : Math.max(1, Math.round(n / 1024)) + ' KB')
function statusText(e: CatLibEntry) {
  if (e.status === 'missing') return 'File missing'
  if (e.status === 'unreadable') return e.published ? 'Published, unreadable' : 'Unreadable'
  return e.published ? 'Published' : 'Unpublished'
}

async function guard<T>(fn: () => Promise<T>) { try { error.value = ''; return await fn() } catch (e) { error.value = (e as Error).message } }
async function load() {
  await guard(async () => {
    cats.value = (await apiCall('GET', '/phe/catalogues')).catalogues
    if (cur.value) cur.value = cats.value.find((c) => c.id === cur.value!.id) || null
    if (cur.value) await loadCur()
  })
}
async function loadLibrary() {
  if (!cur.value) return
  const j = await apiCall('GET', '/phe/catalogue/library?catalogue=' + encodeURIComponent(cur.value.id))
  entries.value = j.entries
  libInfo.value = j.library
  const ids = new Set(entries.value.map((e) => e.id))
  selected.value = selected.value.filter((id) => ids.has(id))
}
async function loadCur() {
  if (!cur.value) return
  const id = encodeURIComponent(cur.value.id)
  syncHours.value = Math.round(cur.value.sync_interval / 3600)
  sources.value = (await apiCall('GET', '/phe/catalogue/sources?catalogue=' + id)).sources
  apps.value = (await apiCall('GET', '/phe/catalogue/apps?catalogue=' + id)).apps
  await loadLibrary()
}
function open(c: CatalogueInfo) { cur.value = { ...c }; selected.value = []; scanMsg.value = ''; guard(loadCur) }
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

async function libGuard(fn: () => Promise<void>) { try { libError.value = ''; await fn() } catch (e) { libError.value = (e as Error).message } }
async function rescan() {
  busy.value = 'scan'
  await libGuard(async () => {
    const r = (await apiCall('POST', '/phe/catalogue/library/scan', {})).result as CatScanResult
    const bits = [
      r.added.length && r.added.length + ' added', r.changed.length && r.changed.length + ' changed', r.removed.length && r.removed.length + ' removed and unpublished',
      r.restored.length && r.restored.length + ' back again', r.renamed.length && r.renamed.length + ' moved', r.unreadable.length && r.unreadable.length + ' unreadable',
    ].filter(Boolean)
    scanMsg.value = 'Scanned ' + r.total + ' file(s): ' + (bits.length ? bits.join(', ') : 'nothing new') + '.'
    await loadCur()
  })
  busy.value = ''
}
async function publish(on: boolean) {
  if (!cur.value || !selected.value.length) return
  await libGuard(async () => {
    const j = await apiCall('POST', '/phe/catalogue/library/publish', { catalogue: cur.value!.id, ids: selected.value, publish: on })
    const probs = Object.entries(j.problems as Record<string, string>)
    if (probs.length) {
      const name = (id: string) => entries.value.find((e) => e.id === id)?.name || id
      libError.value = probs.map(([id, why]) => name(id) + ': ' + why).join('; ')
    }
    await load()
  })
}
function edit(e: CatLibEntry) { ed.value = { entry: e, e: { ...e.edits }, icon: null, error: '' } }
async function saveEdit() {
  const s = ed.value
  if (!s) return
  const clean: Edits = {}
  for (const [k, v] of Object.entries(s.e)) if (typeof v === 'string' && v.trim()) (clean as Record<string, string>)[k] = v.trim()
  try {
    await apiCall('POST', '/phe/catalogue/library/edit', { id: s.entry.id, edits: clean })
    if (s.icon) {
      const f = new FormData()
      f.append('id', s.entry.id)
      f.append('icon', s.icon)
      await apiUpload('/phe/catalogue/library/icon', f)
    }
    ed.value = null
    await loadCur()
  } catch (e) { s.error = (e as Error).message }
}
async function deleteEntry() {
  const e = askDelete.value
  askDelete.value = null
  if (!e) return
  await libGuard(async () => { await apiCall('POST', '/phe/catalogue/library/remove', { id: e.id, delete_file: delFile.value }); delFile.value = false; await loadCur() })
}
async function upload() {
  upMsg.value = null
  if (!u.file) { upMsg.value = { ok: false, text: 'Choose the installation file first.' }; return }
  busy.value = 'up'
  try {
    const f = new FormData()
    f.append('catalogue', cur.value!.id)
    f.append('publish', u.publish ? '1' : '0')
    if (u.version) f.append('version', u.version)
    f.append('file', u.file)
    await apiUpload('/phe/catalogue/upload', f)
    upMsg.value = { ok: true, text: 'Uploaded ' + u.file.name + '.' }
    Object.assign(u, { file: null, version: '' })
    await loadCur()
  } catch (e) { upMsg.value = { ok: false, text: (e as Error).message } } finally { busy.value = '' }
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

<style scoped>
.ph-icon { width: 48px; height: 48px; flex: none; object-fit: contain; display: flex; align-items: center; justify-content: center; text-align: center; border-radius: var(--ph-radius, 8px); }
</style>
