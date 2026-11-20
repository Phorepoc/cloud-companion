<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->
<template>
  <q-page class="ph-contacts" :style-fn="fillPage" @keydown.esc="viewOpen = false">
    <SplitView :detail="viewOpen && !!current" list-label="Contacts" detail-label="Contact">
      <template #list>
        <HdPageHeader :level="2" title="Contacts" :subtitle="contacts.length ? contacts.length + (contacts.length === 1 ? ' contact' : ' contacts') : undefined" colored accent="#007DC9" />
        <div class="ph-list-search ph-contacts__search">
          <q-input v-model="filter" rounded outlined dense clearable placeholder="Search" aria-label="Search contacts" data-test="contacts-search" class="col">
            <template #append><q-icon name="sym_o_search" aria-hidden="true" /></template>
          </q-input>
          <!-- with a header (the classic bar, a desktop window) New contact is there -->
          <q-btn v-if="!chrome.hosted" unelevated color="primary" icon="sym_o_add" aria-label="New contact" data-test="contact-new" @click="openNew" />
        </div>
        <div class="ph-contacts__wrap">
        <div ref="listEl" class="ph-scroll ph-contacts__list" :class="{ 'has-fast': fast }" @scroll.passive="onScroll">
          <q-banner v-if="error" inline-actions class="ph-note is-error ph-contacts__note" role="alert">
            <template #avatar><q-icon name="sym_o_error" aria-hidden="true" /></template>
            {{ error }}
            <template #action><q-btn flat no-caps color="primary" label="Try again" @click="load" /></template>
          </q-banner>
          <HdStatusState v-if="loading && !contacts.length" state="loading" title="Loading contacts" text="This takes a moment." />
          <HdStatusState v-else-if="!loading && !contacts.length && !error" icon="person" title="No contacts yet"
                      text="Add your first contact. It will be available on every device that syncs with your hub.">
            <q-btn outline no-caps color="primary" label="New contact" @click="openNew" />
          </HdStatusState>
          <p v-else-if="contacts.length && !shown.length" class="ph-contacts__none" data-test="contacts-none">No contact matches "{{ filter }}".</p>

          <!-- the N9 Contacts list: each letter opens with a thin rule, dark squircle avatars with white initials, names in bold -->
          <ul v-if="shown.length" class="ph-rows" data-test="contact-list">
            <template v-for="(c, i) in shown" :key="c.href">
              <li v-if="letter(c) !== (i ? letter(shown[i - 1]) : '')" class="ph-contacts__rule" :data-letter="letter(c)" aria-hidden="true"><span>{{ letter(c) }}</span></li>
              <li>
                <button type="button" class="ph-contacts__row" :class="{ 'is-selected': viewOpen && current && current.href === c.href }" :data-test="'contact-row-' + i" @click="openView(c)">
                  <span class="ph-contacts__av hd-squircle" aria-hidden="true">{{ initials(c) }}</span>
                  <span class="ph-row2__text">
                    <span class="ph-row2__title">{{ c.name }}</span>
                    <span v-if="secondary(c)" class="ph-row2__sub">{{ secondary(c) }}</span>
                  </span>
                </button>
              </li>
            </template>
          </ul>
        </div>
        <!-- the N9 fast-scroll band (HdFastScroll): the letters down the right edge, drag to jump section by section -->
        <HdFastScroll v-if="fast" :position="pos" :sections="secs" label="Fast scroll through the contacts" @jump="jump" />
        </div>
      </template>

      <!-- the contact (Quasar/Examples, Contacts detail): the big squircle, the name in light type, Call and Message, the details -->
      <template #detail>
        <article v-if="viewOpen && current" class="ph-contacts__card ph-scroll" data-test="contact-view">
          <q-btn flat no-caps icon="sym_o_arrow_back" label="Contacts" class="ph-split__back ph-contacts__back" @click="viewOpen = false" />
          <div class="ph-contacts__hero">
            <span class="ph-contacts__big hd-squircle" aria-hidden="true">{{ initials(current) }}</span>
            <h2 class="ph-contacts__name" data-test="contact-view-name">{{ current.name }}</h2>
            <p v-if="current.org" class="ph-contacts__org">{{ current.org }}</p>
            <div class="ph-contacts__acts">
              <q-btn v-if="current.phones && current.phones.length" unelevated no-caps color="positive" icon="sym_o_call" label="Call" :href="'tel:' + current.phones[0].value" />
              <q-btn v-if="current.emails && current.emails.length" unelevated no-caps color="primary" icon="sym_o_mail" label="Email" :href="'mailto:' + current.emails[0].value" />
              <q-btn outline no-caps icon="sym_o_edit" label="Edit" data-test="contact-edit" @click="openEdit(current)" />
              <q-btn flat round icon="sym_o_delete" aria-label="Delete" data-test="contact-delete" @click="remove(current)" />
            </div>
          </div>
          <template v-if="current.phones && current.phones.length">
            <HdSectionHeader title="Phone" />
            <a v-for="p in current.phones" :key="'p' + p.value" class="ph-contacts__fact" :href="'tel:' + p.value"><b>{{ p.value }}</b><span>{{ typeName(p.type) }}</span></a>
          </template>
          <template v-if="current.emails && current.emails.length">
            <HdSectionHeader title="Email" />
            <a v-for="e in current.emails" :key="'e' + e.value" class="ph-contacts__fact" :href="'mailto:' + e.value"><b>{{ e.value }}</b><span>{{ typeName(e.type) }}</span></a>
          </template>
          <template v-if="current.note">
            <HdSectionHeader title="Note" />
            <p class="ph-note-text">{{ current.note }}</p>
          </template>
        </article>
        <div v-else class="ph-split__empty"><div><q-icon name="sym_o_contacts" size="48px" aria-hidden="true" /><p>Pick a contact to see the details.</p></div></div>
      </template>
    </SplitView>

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
import { ref, computed, nextTick, onBeforeUnmount, onMounted, watch } from 'vue'
import { hubCall, NO_HUB } from '../cloud'
import type { Contact, Labeled } from '../proto'

/** The edit form: a contact's fields as plain strings and editable lists. */
interface ContactForm { href?: string; etag?: string; given: string; family: string; org: string; note: string; phones: Required<Labeled>[]; emails: Required<Labeled>[] }
import { toast } from '../toast'
import { HdFastScroll, HdPageHeader, HdSectionHeader, HdStatusState } from '@etesian-design/vue'
import SplitView from '../components/SplitView.vue'
import { fillPage } from '../fillPage'
import { useChrome } from '@etesian-design/shell'

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
const initials = (c: Contact) => (c.name || '?').split(/\s+/).filter(Boolean).slice(0, 2).map((w) => (Array.from(w)[0] ?? '').toUpperCase()).join('')
const letter = (c: Contact) => (Array.from(c.name || '#')[0] ?? '#').toLocaleUpperCase()
const typeName = (t?: string) => ({ cell: 'Mobile', home: 'Home', work: 'Work', other: 'Other' } as Record<string, string>)[t || ''] || t || ''
const secondary = (c: Contact) => c.org || (c.emails && c.emails[0] && c.emails[0].value) || (c.phones && c.phones[0] && c.phones[0].value) || ''
// sorted once per change of the list (not on every key typed in the search), Ä next to A; the search text per contact made once too
const byName = new Intl.Collator(undefined, { sensitivity: 'base' })
const sortedContacts = computed(() => [...contacts.value].sort((a, b) => byName.compare(a.name, b.name))
  .map((c) => ({ c, text: [c.name, c.org, ...(c.emails || []).map((e) => e.value), ...(c.phones || []).map((p) => p.value)].join(' ').toLowerCase() })))
const shown = computed(() => {
  const q = (filter.value || '').toLowerCase().trim()
  return sortedContacts.value.filter((x) => !q || x.text.includes(q)).map((x) => x.c)
})

// fast scroll: only for a long list that scrolls and is not being searched; the letters sit where their section is in the list
const listEl = ref<HTMLElement | null>(null)
const pos = ref(0)
const secs = ref<{ letter: string; top: number }[]>([])
const fast = computed(() => !filter.value && shown.value.length > 12)
function measure() {
  const el = listEl.value
  if (!el) return
  const h = el.scrollHeight
  secs.value = [...el.querySelectorAll<HTMLElement>('.ph-contacts__rule')].map((r) => ({ letter: r.dataset.letter || '', top: Math.round(r.offsetTop / h * el.clientHeight) + 14 }))
}
function onScroll() { const el = listEl.value; if (el) pos.value = el.scrollHeight > el.clientHeight ? el.scrollTop / (el.scrollHeight - el.clientHeight) : 0 }
function jump(i: number) { const r = listEl.value?.querySelectorAll<HTMLElement>('.ph-contacts__rule')[i]; if (r && listEl.value) listEl.value.scrollTop = r.offsetTop }
watch([shown, fast], () => nextTick(measure))
onMounted(() => window.addEventListener('resize', measure))
onBeforeUnmount(() => window.removeEventListener('resize', measure))

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
  if (/not set up/i.test(m)) return NO_HUB
  return m
}

const openView = (c: Contact) => { current.value = c; viewOpen.value = true }
function openNew() { form.value = blank(); formError.value = ''; editOpen.value = true }

const chrome = useChrome(() => ({ title: 'Contacts', tools: [{ icon: 'add', label: 'New contact' }, { icon: 'search', label: 'Search' }, { icon: 'refresh', label: 'Refresh' }] }))
chrome.on('tool', (l) => {
  if (l === 'New contact') openNew()
  else if (l === 'Refresh') load()
  else if (l === 'Search') { viewOpen.value = false; (document.querySelector('[data-test=contacts-search] input') as HTMLInputElement | null)?.focus() }
})
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
    if (/another device/.test(formError.value)) load() // the newer version, so the next edit starts from it
  } finally {
    saving.value = false
  }
}

// deleting is deferred for the length of the toast, so "Undo" is a real undo
function remove(c: Contact) {
  viewOpen.value = false
  contacts.value = contacts.value.filter((x) => x.href !== c.href)
  toast('Contact deleted', {
    resume: ['hub.call', { op: 'contacts.delete', href: c.href, etag: c.etag }],
    undo: () => load(), // from the server: the list may have been reloaded since (it came back twice)
    onExpire: async () => {
      try { await hubCall({ op: 'contacts.delete', href: c.href, etag: c.etag }) } catch (e) { error.value = (e as Error).message; await load() }
    },
  })
}

onMounted(load)
</script>

<style scoped>
.ph-contacts { display: flex; flex-direction: column; }
.ph-contacts__search { display: flex; gap: 8px; align-items: center; border-bottom: 1px solid var(--hd-c-divider); }
.ph-contacts__wrap { position: relative; flex: 1; min-height: 0; display: flex; }
.ph-contacts__list { flex: 1; min-height: 0; }
.ph-contacts__list.has-fast { padding-right: 47px; scrollbar-width: none; }
.ph-contacts__list.has-fast::-webkit-scrollbar { display: none; }
.ph-contacts__note { margin: 10px 11px; }
.ph-contacts__none { margin: 16px; color: var(--hd-c-fg-secondary); }
.ph-contacts__rule { display: flex; align-items: center; gap: 10px; height: 28px; padding: 0 12px; font: 700 var(--hd-font-size-xsmall)/1 var(--hd-font-family-base); color: var(--hd-c-fg-secondary); }
.ph-contacts__rule::before { content: ''; flex: 1; height: 1px; background: var(--hd-c-divider); }
.ph-contacts__row { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 64px; padding: 0 12px; border: 0; background: none; color: inherit; text-align: left; font: inherit; cursor: pointer; }
.ph-contacts__row:hover, .ph-contacts__row.is-selected { background: var(--hd-c-pressed); }
.ph-contacts__row .ph-row2__title { font-size: 1.0625rem; }
.ph-contacts__av { flex: none; width: 43px; height: 43px; display: grid; place-items: center; background: linear-gradient(#3a3a3a, #0a0a0a); color: #fff; font: 700 1.125rem/1 var(--hd-font-family-base); }
.ph-contacts__card { flex: 1; min-height: 0; padding: 16px 24px 24px; }
.ph-contacts__back { margin: -8px 0 0 -12px; }
.ph-contacts__hero { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 16px 0 8px; text-align: center; }
.ph-contacts__big { width: 148px; height: 148px; display: grid; place-items: center; background: linear-gradient(135deg, #7fb0d8, #2a5b8a); color: #fff; font: 300 56px/1 var(--hd-font-family-light); }
.ph-contacts__name { margin: 0; font: 300 2.25rem/1.1 var(--hd-font-family-light); overflow-wrap: anywhere; }
.ph-contacts__org { margin: 0; font-size: var(--hd-font-size-small); color: var(--hd-c-fg-secondary); }
.ph-contacts__acts { display: flex; flex-wrap: wrap; justify-content: center; gap: 8px; margin-top: 6px; }
.ph-contacts__fact { display: flex; flex-direction: column; padding: 8px 16px; color: inherit; text-decoration: none; }
.ph-contacts__fact:hover { background: var(--hd-c-pressed); }
.ph-contacts__fact b { font: 700 1rem/1.25 var(--hd-font-family-base); }
.ph-contacts__fact span { font-size: var(--hd-font-size-small); color: var(--hd-c-fg-secondary); }
.ph-note-text { margin: 0; padding: 4px 16px; white-space: pre-wrap; overflow-wrap: anywhere; }
.ph-dialog { width: min(94vw, 480px); }
.ph-dialog h2 { margin-bottom: var(--ph-s2); overflow-wrap: anywhere; }
.ph-form { display: flex; flex-direction: column; gap: var(--ph-s3); }
.ph-two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--ph-s3); }
.ph-pair { display: grid; grid-template-columns: 1fr 120px; gap: var(--ph-s2); }
.ph-error { display: flex; gap: var(--ph-s2); color: var(--ph-negative); margin: 0; }
</style>
