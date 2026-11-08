// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The pages of Settings (SettingsPage.vue) and which one is open. The classic layout follows the address (/settings/connections); the
// desktop has no addresses of its own, so a link to a page of Settings opens the Settings window and sets the page here.
import { ref } from 'vue'

export interface SettingsPageInfo { id: string; title: string; icon: string; color: string; subtitle?: string; admin?: boolean }
export interface SettingsGroup { title: string; pages: SettingsPageInfo[] }

export const SETTINGS_GROUPS: SettingsGroup[] = [
  { title: 'This device', pages: [
    { id: 'appearance', title: 'Appearance', icon: 'moon', color: '#3E4A5C', subtitle: 'Light or dark, classic or desktop' },
    { id: 'data', title: 'Data use', icon: 'data', color: '#0093D0', subtitle: 'Picture quality' },
    { id: 'pair', title: 'Pair a new device', icon: 'qr', color: '#5AA21C', subtitle: 'A one-time code' },
    { id: 'browser', title: 'This browser', icon: 'globe', color: '#4591FF', subtitle: 'Device ID and disconnect' },
  ] },
  { title: 'Your server', pages: [
    { id: 'connections', title: 'Connections', icon: 'split', color: '#347708', subtitle: 'Accounts and messaging', admin: true },
    { id: 'devices', title: 'Devices', icon: 'window', color: '#F77219', subtitle: 'Paired phones and browsers', admin: true },
    { id: 'features', title: 'Features', icon: 'grid', color: '#7A3FB8', subtitle: 'Turn optional parts on or off', admin: true },
    { id: 'system', title: 'System', icon: 'settings', color: '#4A5568', subtitle: 'Hardware and SSH access', admin: true },
    { id: 'updates', title: 'Updates', icon: 'refresh', color: '#1C7C3C', subtitle: 'Automatic updates', admin: true },
    { id: 'backups', title: 'Backups', icon: 'save', color: '#0B7A75', subtitle: 'Nightly backups of your data', admin: true },
    { id: 'catalogue', title: 'Catalogue', icon: 'bag', color: '#1C51AF', subtitle: 'Apps and services', admin: true },
    { id: 'weather', title: 'Weather', icon: 'location', color: '#0A9BD8', subtitle: 'Default location', admin: true },
    { id: 'security', title: 'Security', icon: 'lock', color: '#6A6A6A', subtitle: 'Passkeys, recovery code', admin: true },
  ] },
]
export const settingsPageIds = SETTINGS_GROUPS.flatMap((g) => g.pages.map((p) => p.id))

/** the page Settings shows; the classic layout sets it from the address */
export const settingsPage = ref('appearance')
