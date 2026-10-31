<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<template>
  <q-page class="ph-page">
    <div class="ph-head">
      <h1>Contacts</h1>
      <q-btn unelevated color="primary" icon="sym_o_add" label="New contact" data-test="contact-new" @click="openNew" />
    </div>
    <q-input v-model="filter" outlined clearable placeholder="Search contacts" aria-label="Search contacts" data-test="contacts-search" class="q-mb-md">
      <template #prepend><q-icon name="sym_o_search" aria-hidden="true" /></template>
    </q-input>

    <q-banner v-if="error" class="ph-note is-error q-mb-md" rounded role="alert">
      <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
      {{ error }}
      <template #action><q-btn flat label="Try again" @click="load" /></template>
    </q-banner>
    <EmptyState v-if="loading && !contacts.length" state="loading" title="Loading contacts" text="This takes a moment." />
    <EmptyState v-else-if="!loading && !contacts.length && !error" icon="sym_o_contacts" title="No contacts yet"
                text="Add your first contact. It will be available on every device that syncs with your hub.">
      <template #action><q-btn outline color="primary" label="New contact" @click="openNew" /></template>
    </EmptyState>
    <p v-else-if="contacts.length && !shown.length" class="ph-muted" data-test="contacts-none">No contact matches "{{ filter }}".</p>

    <ul v-if="shown.length" class="ph-list ph-card" data-test="contact-list">
      <li v-for="(c, i) in shown" :key="c.href">
        <button type="button" class="ph-row" :data-test="'contact-row-' + i" @click="openView(c)">
          <span class="ph-avatar hd-squircle" aria-hidden="true">{{ initials(c) }}</span>
          <span class="ph-row-main">
            <span class="ph-row-title">{{ c.name }}</span>
            <span class="ph-small ph-muted">{{ secondary(c) }}</span>
          </span>
        </button>
      </li>
    </ul>

    <!-- read view -->
    <q-dialog v-model="viewOpen">
      <q-card v-if="current" class="ph-dialog" data-test="contact-view">
        <q-card-section>
          <h2 data-test="contact-view-name">{{ current.name }}</h2>
          <p v-if="current.org" class="ph-muted q-mb-sm">{{ current.org }}</p>
          <ul class="ph-facts">
            <li v-for="p in current.phones || []" :key="'p' + p.value"><q-icon name="sym_o_call" aria-hidden="true" /> <a :href="'tel:' + p.value">{{ p.value }}</a> <span v-if="p.type" class="ph-muted ph-small">{{ p.type }}</span></li>
            <li v-for="e in current.emails || []" :key="'e' + e.value"><q-icon name="sym_o_mail" aria-hidden="true" /> <a :href="'mailto:' + e.value">{{ e.value }}</a> <span v-if="e.type" class="ph-muted ph-small">{{ e.type }}</span></li>
          </ul>
          <p v-if="current.note" class="ph-note-text">{{ current.note }}</p>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat color="negative" label="Delete" data-test="contact-delete" @click="remove(current)" />
          <q-btn flat label="Close" v-close-popup />
          <q-btn unelevated color="primary" label="Edit" data-test="contact-edit" @click="openEdit(current)" />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <!-- edit / create -->
    <q-dialog v-model="editOpen" persistent>
      <q-card class="ph-dialog" data-test="contact-form">
        <form @submit.prevent="save">
          <q-card-section class="ph-form">
            <h2>{{ form.href ? 'Edit contact' : 'New contact' }}</h2>
            <div class="ph-two">
              <q-input v-model="form.given" label="First name" outlined autofocus data-test="contact-given" />
              <q-input v-model="form.family" label="Last name" outlined data-test="contact-family" />
            </div>
            <q-input v-model="form.org" label="Company" outlined data-test="contact-org" />
            <div v-for="(p, i) in form.phones" :key="'fp' + i" class="ph-pair">
              <q-input v-model="p.value" label="Phone" type="tel" outlined :data-test="'contact-phone-' + i" />
              <q-select v-model="p.type" :options="phoneTypes" label="Type" outlined emit-value map-options />
            </div>
            <q-btn flat icon="sym_o_add" label="Add a phone number" data-test="contact-add-phone" @click="form.phones.push({ type: 'cell', value: '' })" />
            <div v-for="(e, i) in form.emails" :key="'fe' + i" class="ph-pair">
              <q-input v-model="e.value" label="Email" type="email" outlined :data-test="'contact-email-' + i" />
              <q-select v-model="e.type" :options="emailTypes" label="Type" outlined emit-value map-options />
            </div>
            <q-btn flat icon="sym_o_add" label="Add an email address" data-test="contact-add-email" @click="form.emails.push({ type: 'home', value: '' })" />
            <q-input v-model="form.note" label="Note" type="textarea" outlined autogrow data-test="contact-note" />
            <p v-if="formError" class="ph-error" role="alert"><q-icon name="sym_o_error" size="20px" aria-hidden="true" /> {{ formError }}</p>
          </q-card-section>
          <q-card-actions align="right">
            <q-btn flat label="Cancel" :disable="saving" data-test="contact-cancel" @click="editOpen = false" />
            <q-btn unelevated color="primary" label="Save" type="submit" :loading="saving" data-test="contact-save" />
          </q-card-actions>
        </form>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { hubCall } from '../cloud'
import type { Contact, Labeled } from '../proto'

/** The edit form: a contact's fields as plain strings and editable lists. */
interface ContactForm { href?: string; etag?: string; given: string; family: string; org: string; note: string; phones: Required<Labeled>[]; emails: Required<Labeled>[] }
import { toast } from '../toast'
import EmptyState from '../components/EmptyState.vue'

const contacts = ref<Contact[]>([])
const loading = ref(false)
const error = ref('')
const filter = ref('')
const current = ref<Contact | null>(null)
const viewOpen = ref(false)
const editOpen = ref(false)
const saving = ref(false)
const formError = ref('')
const form = ref<ContactForm>(blank())
const phoneTypes = [{ label: 'Mobile', value: 'cell' }, { label: 'Home', value: 'home' }, { label: 'Work', value: 'work' }, { label: 'Other', value: 'other' }]
const emailTypes = [{ label: 'Home', value: 'home' }, { label: 'Work', value: 'work' }, { label: 'Other', value: 'other' }]

function blank() { return { href: '', etag: '', given: '', family: '', org: '', note: '', phones: [], emails: [] } }
const initials = (c: Contact) => (c.name || '?').split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join('')
const secondary = (c: Contact) => c.org || (c.emails && c.emails[0] && c.emails[0].value) || (c.phones && c.phones[0] && c.phones[0].value) || ''
const shown = computed(() => {
  const q = (filter.value || '').toLowerCase().trim()
  const list = [...contacts.value].sort((a, b) => a.name.localeCompare(b.name))
  return q ? list.filter((c) => [c.name, c.org, ...(c.emails || []).map((e) => e.value), ...(c.phones || []).map((p) => p.value)].join(' ').toLowerCase().includes(q)) : list
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    contacts.value = (await hubCall({ op: 'contacts.list' })).contacts || []
  } catch (e) {
    error.value = friendly((e as Error).message)
  } finally {
    loading.value = false
  }
}

function friendly(m: string) {
  if (/not set up/i.test(m)) return 'The hub is not set up on this server, so there are no contacts to show. See the hub guide to set it up.'
  return m
}

const openView = (c: Contact) => { current.value = c; viewOpen.value = true }
function openNew() { form.value = blank(); formError.value = ''; editOpen.value = true }
function openEdit(c: Contact) {
  viewOpen.value = false
  form.value = { href: c.href, etag: c.etag, given: c.given || '', family: c.family || '', org: c.org || '', note: c.note || '',
    phones: (c.phones || []).map((p) => ({ type: p.type || 'cell', value: p.value })), emails: (c.emails || []).map((e) => ({ type: e.type || 'home', value: e.value })) }
  if (!form.value.given && !form.value.family) { const [g, ...r] = (c.name || '').split(' '); form.value.given = g; form.value.family = r.join(' ') }
  formError.value = ''
  editOpen.value = true
}

async function save() {
  const f = form.value
  const name = `${f.given} ${f.family}`.trim()
  if (!name) { formError.value = 'Enter a name for this contact.'; return }
  saving.value = true
  formError.value = ''
  try {
    const contact = { href: f.href || undefined, etag: f.etag || undefined, name, given: f.given, family: f.family, org: f.org, note: f.note,
      phones: f.phones.filter((p) => p.value.trim()), emails: f.emails.filter((e) => e.value.trim()) }
    await hubCall({ op: 'contacts.save', contact })
    editOpen.value = false
    toast(f.href ? 'Contact saved' : 'Contact added')
    await load()
  } catch (e) {
    formError.value = (e as Error).message
  } finally {
    saving.value = false
  }
}

// deleting is deferred for the length of the toast, so "Undo" is a real undo
function remove(c: Contact) {
  viewOpen.value = false
  contacts.value = contacts.value.filter((x) => x.href !== c.href)
  toast('Contact deleted', {
    undo: () => { contacts.value = [...contacts.value, c] },
    onExpire: async () => {
      try { await hubCall({ op: 'contacts.delete', href: c.href, etag: c.etag }) } catch (e) { error.value = (e as Error).message; await load() }
    },
  })
}

onMounted(load)
</script>

<style scoped>
.ph-head { display: flex; align-items: center; justify-content: space-between; gap: var(--ph-s3); margin-bottom: var(--ph-s4); }
.ph-list { list-style: none; padding: 0; margin: 0; }
.ph-list li { border-bottom: 1px solid var(--ph-divider); }
.ph-list li:last-child { border-bottom: 0; }
.ph-row { width: 100%; min-height: 56px; display: flex; align-items: center; gap: var(--ph-s3); padding: var(--ph-s2) var(--ph-s4); background: none; border: 0; text-align: left; font: inherit; color: var(--ph-text); cursor: pointer; }
.ph-row:hover { background: var(--ph-surface-2); }
.ph-row-main { display: flex; flex-direction: column; min-width: 0; }
.ph-row-title { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ph-avatar { flex: none; width: 40px; height: 40px; display: grid; place-items: center; background: var(--ph-accent-soft); color: var(--ph-accent); font-weight: 600; }
.ph-dialog { width: min(94vw, 480px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); overflow-wrap: anywhere; }
.ph-form { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ph-s3); }
.ph-pair { display: grid; grid-template-columns: 1fr 120px; gap: var(--ph-s2); }
.ph-facts { list-style: none; padding: 0; margin: var(--ph-s2) 0; display: flex; flex-direction: column; gap: var(--ph-s2); }
.ph-facts a { color: var(--ph-accent); }
.ph-note-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.ph-note { background: var(--ph-surface-2); color: var(--ph-text); border: 1px solid var(--ph-divider); }
.ph-note.is-error { border-color: var(--ph-negative); }
.ph-error { display: flex; gap: var(--ph-s2); color: var(--ph-negative); margin: 0; }
</style>
