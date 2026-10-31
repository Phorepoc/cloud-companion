<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- Files: the owner's own storage on the server, the same on every device (internal/webui/files.go). Folders and files with their
     type on a squircle tile; upload with the button or by dropping files on the page; a file opens in the browser (songs play in
     the music player), downloads, gets a public link to share, or is renamed or deleted (Undo while the toast shows). -->
<template>
  <q-page class="ph-page" :class="{ 'is-drop': dropping }" data-test="files-page" @dragover.prevent="dropping = true" @dragleave.self="dropping = false" @drop.prevent="onDrop">
    <div class="ph-head">
      <h1>Files</h1>
      <span class="ph-head__acts">
        <q-btn flat no-caps icon="sym_o_create_new_folder" label="New folder" data-test="files-mkdir" @click="askFolder" />
        <q-btn unelevated no-caps color="primary" icon="sym_o_upload" label="Upload" :loading="uploading" data-test="files-upload" @click="picker?.click()" />
        <input ref="picker" type="file" multiple hidden data-test="files-input" @change="onPick">
      </span>
    </div>
    <nav class="ph-crumbs" aria-label="Folder">
      <button type="button" data-test="files-crumb-root" @click="go('')">Files</button>
      <template v-for="(c, i) in crumbs" :key="c.path"><q-icon name="sym_o_chevron_right" size="18px" aria-hidden="true" />
        <button type="button" :aria-current="i === crumbs.length - 1 ? 'page' : undefined" @click="go(c.path)">{{ c.name }}</button></template>
    </nav>

    <q-banner v-if="error" class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="load" /></template>
    </q-banner>
    <EmptyState v-if="loading && !entries.length" state="loading" title="Loading files" text="This takes a moment." />
    <EmptyState v-else-if="!loading && !entries.length && !error" icon="sym_o_folder" :title="path ? 'This folder is empty' : 'No files yet'"
                text="Upload files, or drop them here. They are kept on your server and reach every device you pair." data-test="files-empty" />

    <ul v-if="entries.length" class="ph-list ph-card" data-test="files-list">
      <li v-for="e in entries" :key="e.path" class="ph-frow">
        <button type="button" class="ph-row" :data-test="'file-' + e.name" @click="openEntry(e)">
          <span class="ph-ftile hd-squircle" :class="{ 'is-dir': e.dir }" aria-hidden="true"><q-icon :name="iconOf(e)" size="22px" /></span>
          <span class="ph-row-main">
            <span class="ph-row-title">{{ e.name }}</span>
            <span class="ph-small ph-muted">{{ e.dir ? 'Folder' : size(e.size) }} · {{ when(e.mod) }}<template v-if="sharedPaths.has(e.path)"> · Shared</template></span>
          </span>
        </button>
        <q-btn flat round dense icon="sym_o_more_vert" :aria-label="'More for ' + e.name" :data-test="'file-more-' + e.name">
          <q-menu auto-close>
            <q-list dense style="min-width: 180px">
              <q-item v-if="!e.dir" clickable @click="download(e)"><q-item-section avatar><q-icon name="sym_o_download" /></q-item-section><q-item-section>Download</q-item-section></q-item>
              <q-item v-if="!e.dir" clickable :data-test="'file-share-' + e.name" @click="askShare(e)"><q-item-section avatar><q-icon name="sym_o_share" /></q-item-section><q-item-section>Share a link</q-item-section></q-item>
              <q-item clickable :data-test="'file-rename-' + e.name" @click="askRename(e)"><q-item-section avatar><q-icon name="sym_o_edit" /></q-item-section><q-item-section>Rename</q-item-section></q-item>
              <q-item clickable class="text-negative" :data-test="'file-delete-' + e.name" @click="remove(e)"><q-item-section avatar><q-icon name="sym_o_delete" /></q-item-section><q-item-section>Delete</q-item-section></q-item>
            </q-list>
          </q-menu>
        </q-btn>
      </li>
    </ul>

    <template v-if="shares.length && !path">
      <h2 class="ph-sh">Shared links</h2>
      <ul class="ph-list ph-card" data-test="files-shares">
        <li v-for="s in shares" :key="s.token" class="ph-frow">
          <span class="ph-row">
            <span class="ph-ftile hd-squircle" aria-hidden="true"><q-icon name="sym_o_link" size="22px" /></span>
            <span class="ph-row-main"><span class="ph-row-title">{{ s.path }}</span>
              <span class="ph-small ph-muted">{{ s.expires ? 'Until ' + new Date(s.expires).toLocaleDateString() : 'No end date' }}</span></span>
          </span>
          <q-btn flat dense no-caps label="Copy link" @click="copy(shareLink(s))" />
          <q-btn flat dense no-caps color="negative" label="Stop sharing" :data-test="'unshare-' + s.path" @click="stopSharing(s)" />
        </li>
      </ul>
    </template>

    <!-- name a new folder / rename -->
    <q-dialog v-model="naming.open">
      <q-card class="ph-dialog" data-test="files-name">
        <form @submit.prevent="saveName">
          <q-card-section><h2>{{ naming.from ? 'Rename' : 'New folder' }}</h2>
            <q-input v-model="naming.name" outlined autofocus label="Name" data-test="files-name-input" :error="!!naming.error" :error-message="naming.error" /></q-card-section>
          <q-card-actions align="right"><q-btn flat label="Cancel" v-close-popup /><q-btn unelevated color="primary" type="submit" :label="naming.from ? 'Rename' : 'Create'" data-test="files-name-save" /></q-card-actions>
        </form>
      </q-card>
    </q-dialog>
    <!-- share -->
    <q-dialog v-model="sharing.open">
      <q-card class="ph-dialog" data-test="files-share">
        <q-card-section><h2>Share "{{ sharing.entry?.name }}"</h2>
          <p class="ph-muted">Anyone with the link can download this file.</p>
          <q-btn-toggle v-model="sharing.days" no-caps unelevated spread toggle-color="primary" :options="[{ label: '1 day', value: 1 }, { label: '1 week', value: 7 }, { label: '30 days', value: 30 }, { label: 'No end', value: 0 }]" />
          <q-input v-if="sharing.url" :model-value="sharing.url" readonly outlined class="q-mt-md" data-test="files-share-url" @focus="($event.target as HTMLInputElement).select()" />
          <p v-if="sharing.error" class="text-negative q-mt-sm" role="alert">{{ sharing.error }}</p></q-card-section>
        <q-card-actions align="right"><q-btn flat label="Close" v-close-popup />
          <q-btn v-if="!sharing.url" unelevated color="primary" label="Make the link" data-test="files-share-make" @click="makeShare" />
          <q-btn v-else unelevated color="primary" label="Copy link" @click="copy(sharing.url)" /></q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import EmptyState from '../components/EmptyState.vue'
import { toast } from '../toast'
import { deleteFile, fileURL, iconOf, listFiles, listShares, loadTracks, makeFolder, moveFile, shareFile, shareLink, size, unshare, uploadFiles, type FileEntry, type Share } from '../files'
import { play } from '../music'

const path = ref('')
const entries = ref<FileEntry[]>([])
const shares = ref<Share[]>([])
const loading = ref(false), uploading = ref(false), dropping = ref(false)
const error = ref('')
const picker = ref<HTMLInputElement | null>(null)
const crumbs = computed(() => { const out: { name: string; path: string }[] = []; let p = ''; for (const n of path.value.split('/').filter(Boolean)) { p = p ? p + '/' + n : n; out.push({ name: n, path: p }) } return out })
const sharedPaths = computed(() => new Set(shares.value.map((s) => s.path)))
const when = (iso: string) => { const d = new Date(iso); return d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString() }

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [l, s] = await Promise.all([listFiles(path.value), listShares()])
    entries.value = l.entries
    shares.value = s
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}
function go(p: string) { path.value = p; entries.value = []; load() }
const join = (n: string) => (path.value ? path.value + '/' + n : n)

async function upload(list: FileList | File[]) {
  if (!list.length) return
  uploading.value = true
  try { const j = await uploadFiles(path.value, list); toast(j.saved.length === 1 ? 'File uploaded' : `${j.saved.length} files uploaded`); await load() }
  catch (e) { error.value = (e as Error).message } finally { uploading.value = false }
}
function onPick(ev: Event) { const i = ev.target as HTMLInputElement; if (i.files) upload(i.files).then(() => { i.value = '' }) }
function onDrop(ev: DragEvent) { dropping.value = false; if (ev.dataTransfer?.files) upload(ev.dataTransfer.files) }

async function openEntry(e: FileEntry) {
  if (e.dir) return go(e.path)
  if (e.type?.startsWith('audio/')) {
    // play the song with the rest of its folder after it
    const all = await loadTracks().catch(() => [])
    const here = all.filter((t) => t.path.slice(0, t.path.lastIndexOf('/')) === path.value)
    const i = here.findIndex((t) => t.path === e.path)
    return i >= 0 ? play(here, i) : undefined
  }
  window.open(await fileURL(e.path), '_blank', 'noopener')
}
async function download(e: FileEntry) {
  const a = document.createElement('a')
  a.href = await fileURL(e.path, true)
  a.download = e.name
  a.click()
}

const naming = reactive({ open: false, name: '', from: '', error: '' })
function askFolder() { Object.assign(naming, { open: true, name: '', from: '', error: '' }) }
function askRename(e: FileEntry) { Object.assign(naming, { open: true, name: e.name, from: e.path, error: '' }) }
async function saveName() {
  const n = naming.name.trim()
  if (!n || /[/\\]/.test(n)) { naming.error = 'Enter a name without slashes.'; return }
  try {
    if (naming.from) await moveFile(naming.from, join(n)); else await makeFolder(join(n))
    naming.open = false
    toast(naming.from ? 'Renamed' : 'Folder created')
    await load()
  } catch (e) { naming.error = (e as Error).message }
}

// deleting waits for the toast, so Undo is a real undo
function remove(e: FileEntry) {
  entries.value = entries.value.filter((x) => x.path !== e.path)
  toast(e.dir ? 'Folder deleted' : 'File deleted', {
    undo: () => load(),
    onExpire: async () => { try { await deleteFile(e.path) } catch (err) { error.value = (err as Error).message; load() } },
  })
}

const sharing = reactive({ open: false, entry: null as FileEntry | null, days: 7, url: '', error: '' })
function askShare(e: FileEntry) { Object.assign(sharing, { open: true, entry: e, days: 7, url: '', error: '' }) }
async function makeShare() {
  try { sharing.url = await shareFile(sharing.entry!.path, sharing.days); shares.value = await listShares() } catch (e) { sharing.error = (e as Error).message }
}
async function stopSharing(s: Share) { await unshare(s.token).catch(() => {}); shares.value = await listShares(); toast('Link removed') }
function copy(t: string) { navigator.clipboard?.writeText(t).then(() => toast('Link copied'), () => {}) }

onMounted(load)
</script>

<style scoped>
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s2); }
.ph-head__acts { display: flex; gap: var(--ph-s2); }
.ph-crumbs { display: flex; align-items: center; flex-wrap: wrap; gap: 2px; margin-bottom: var(--ph-s3); color: var(--ph-text-muted); }
.ph-crumbs button { padding: 4px 6px; border: 0; border-radius: 4px; background: none; color: var(--ph-accent); font: inherit; cursor: pointer; }
.ph-crumbs button[aria-current] { color: var(--ph-text); font-weight: 600; cursor: default; }
.ph-page.is-drop { outline: 3px dashed var(--ph-accent); outline-offset: -8px; }
.ph-list { list-style: none; padding: 0; margin: 0; }
.ph-list li { border-bottom: 1px solid var(--ph-divider); }
.ph-list li:last-child { border-bottom: 0; }
.ph-frow { display: flex; align-items: center; padding-right: var(--ph-s2); }
.ph-row { flex: 1; min-width: 0; min-height: 56px; display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s2) var(--ph-s4); background: none; border: 0; text-align: left; font: inherit; color: var(--ph-text); cursor: pointer; }
button.ph-row:hover { background: var(--ph-surface-2); }
.ph-row-main { display: flex; flex-direction: column; min-width: 0; }
.ph-row-title { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-ftile { flex: none; width: 40px; height: 40px; display: grid; place-items: center; background: var(--ph-accent-soft); color: var(--ph-accent); }
.ph-ftile.is-dir { background: #EA650A; color: #fff; }
.ph-sh { margin: var(--ph-s5, 32px) 0 var(--ph-s2); }
.ph-dialog { width: min(94vw, 480px); }
</style>
