// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The sections of the app, in navigation order. Add a section here and in router.ts; the desktop drawer and the phone's bottom bar
// both read this list. `primary` sections get a place in the phone's bottom bar (four at most); the rest sit under "More".
// `color` is the section's N9 application colour (Etesian accent tokens): its lit squircle tile in the navigation and the desktop dock.
export interface Section {
  id: string
  label: string
  icon: string
  color: string
  path: string
  primary?: boolean // a place in the phone's bottom bar
  admin?: boolean   // admin devices only
}

export const sections: Section[] = [
  { id: 'browse', label: 'Browser', icon: 'sym_o_language', color: '#4591FF', path: '/browse', primary: true },
  { id: 'chats', label: 'Chats', icon: 'sym_o_chat', color: '#09A7CC', path: '/chats', primary: true },
  { id: 'mail', label: 'Mail', icon: 'sym_o_mail', color: '#0067BC', path: '/mail', primary: true },
  { id: 'calendar', label: 'Calendar', icon: 'sym_o_calendar_month', color: '#EF5500', path: '/calendar', primary: true },
  { id: 'contacts', label: 'Contacts', icon: 'sym_o_contacts', color: '#007DC9', path: '/contacts' },
  { id: 'files', label: 'Files', icon: 'sym_o_folder', color: '#EA650A', path: '/files' },
  { id: 'music', label: 'Music', icon: 'sym_o_music_note', color: '#E6007E', path: '/music' },
  { id: 'android', label: 'Android', icon: 'sym_o_smartphone', color: '#62B700', path: '/android', admin: true },
  { id: 'connections', label: 'Connections', icon: 'sym_o_sync_alt', color: '#347708', path: '/connections', admin: true },
  { id: 'catalogue', label: 'Catalogue', icon: 'sym_o_storefront', color: '#1C51AF', path: '/catalogue', admin: true },
  { id: 'devices', label: 'Devices', icon: 'sym_o_devices', color: '#F77219', path: '/devices', admin: true },
  { id: 'settings', label: 'Settings', icon: 'sym_o_settings', color: '#6A6A6A', path: '/settings' },
]

// what each placeholder section will hold (shown by the empty state until the section exists)
export const placeholders: Record<string, { title: string; icon: string; text: string }> = {
  chats: { title: 'Chats', icon: 'sym_o_chat', text: 'Your conversations from every messaging service will appear here, in one list.' },
  mail: { title: 'Mail', icon: 'sym_o_mail', text: 'Your mailboxes will appear here, with one inbox across all your accounts.' },
  calendar: { title: 'Calendar', icon: 'sym_o_calendar_month', text: 'Your events from every calendar will appear here.' },
  contacts: { title: 'Contacts', icon: 'sym_o_contacts', text: 'Your contacts will appear here, merged from all your accounts and devices.' },
}
