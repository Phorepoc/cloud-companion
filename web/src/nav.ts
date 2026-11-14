// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The sections of the app, in navigation order. Add a section here and in router.ts; the desktop drawer and the phone's bottom bar
// both read this list. `primary` sections get a place in the phone's bottom bar (four at most); the rest sit under "More".
// `color` is the section's N9 application colour (Etesian accent tokens): its lit squircle tile in the navigation and the desktop dock.
export interface Section {
  id: string
  label: string
  icon: string
  color: string
  glyph: string // the Etesian design icon (@etesian-design icons), for the shared desktop and the classic apps menu
  path: string
  primary?: boolean // a place in the phone's bottom bar
  admin?: boolean   // admin devices only
}

// Connections, Devices and Catalogue are pages of Settings (settingsNav.ts), not sections of their own
export const sections: Section[] = [
  { id: 'browse', glyph: 'globe', label: 'Browser', icon: 'sym_o_language', color: '#4591FF', path: '/browse', primary: true },
  { id: 'chats', glyph: 'message', label: 'Chats', icon: 'sym_o_chat', color: '#09A7CC', path: '/chats', primary: true },
  { id: 'mail', glyph: 'mail', label: 'Mail', icon: 'sym_o_mail', color: '#0067BC', path: '/mail', primary: true },
  { id: 'calendar', glyph: 'calendar', label: 'Calendar', icon: 'sym_o_calendar_month', color: '#EF5500', path: '/calendar', primary: true },
  { id: 'contacts', glyph: 'person', label: 'Contacts', icon: 'sym_o_contacts', color: '#007DC9', path: '/contacts' },
  { id: 'files', glyph: 'folder', label: 'Files', icon: 'sym_o_folder', color: '#EA650A', path: '/files' },
  { id: 'photos', glyph: 'photo', label: 'Photos', icon: 'sym_o_photo_library', color: '#7A3FD0', path: '/photos' },
  { id: 'feeds', glyph: 'rss', label: 'Feeds', icon: 'sym_o_rss_feed', color: '#FF8500', path: '/feeds' },
  { id: 'music', glyph: 'music', label: 'Music', icon: 'sym_o_music_note', color: '#E6007E', path: '/music' },
  { id: 'android', glyph: 'phone', label: 'Android', icon: 'sym_o_smartphone', color: '#62B700', path: '/android', admin: true },
  { id: 'settings', glyph: 'settings', label: 'Settings', icon: 'sym_o_settings', color: '#6A6A6A', path: '/settings' },
]

// what each placeholder section will hold (shown by the empty state until the section exists)
export const placeholders: Record<string, { title: string; icon: string; text: string }> = {
  chats: { title: 'Chats', icon: 'message', text: 'Your conversations from every messaging service will appear here, in one list.' },
  mail: { title: 'Mail', icon: 'mail', text: 'Your mailboxes will appear here, with one inbox across all your accounts.' },
  calendar: { title: 'Calendar', icon: 'calendar', text: 'Your events from every calendar will appear here.' },
  contacts: { title: 'Contacts', icon: 'person', text: 'Your contacts will appear here, merged from all your accounts and devices.' },
}
