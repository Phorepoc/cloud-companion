// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The sections of the app, in navigation order. Add a section here and in router.js; the desktop drawer and the phone's bottom bar
// both read this list. `primary` sections get a place in the phone's bottom bar (four at most); the rest sit under "More".
export const sections = [
  { id: 'browse', label: 'Browser', icon: 'sym_o_language', path: '/browse', primary: true },
  { id: 'chats', label: 'Chats', icon: 'sym_o_chat', path: '/chats', primary: true },
  { id: 'mail', label: 'Mail', icon: 'sym_o_mail', path: '/mail', primary: true },
  { id: 'calendar', label: 'Calendar', icon: 'sym_o_calendar_month', path: '/calendar', primary: true },
  { id: 'contacts', label: 'Contacts', icon: 'sym_o_contacts', path: '/contacts' },
  { id: 'files', label: 'Files', icon: 'sym_o_folder', path: '/files' },
  { id: 'devices', label: 'Devices', icon: 'sym_o_devices', path: '/devices', admin: true },
  { id: 'settings', label: 'Settings', icon: 'sym_o_settings', path: '/settings' },
]

// what each placeholder section will hold (shown by the empty state until the section exists)
export const placeholders = {
  chats: { title: 'Chats', icon: 'sym_o_chat', text: 'Your conversations from every messaging service will appear here, in one list.' },
  mail: { title: 'Mail', icon: 'sym_o_mail', text: 'Your mailboxes will appear here, with one inbox across all your accounts.' },
  calendar: { title: 'Calendar', icon: 'sym_o_calendar_month', text: 'Your events from every calendar will appear here.' },
  contacts: { title: 'Contacts', icon: 'sym_o_contacts', text: 'Your contacts will appear here, merged from all your accounts and devices.' },
  files: { title: 'Files', icon: 'sym_o_folder', text: 'Your files will appear here, available on every device.' },
}
