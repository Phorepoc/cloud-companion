// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// What the web app exposes to the browser tests on `window` (src/cloud.ts, src/pages/BrowserPage.vue, src/android/stream.js).
// The protocol shapes come from src/proto.ts (the app's own types).

import type { DeviceInfo, Dialog, Focus, HubRequest, Layer, PageInfo, Region, SessionInfo, TabInfo } from '../src/proto.ts'

// the app's `state` (AppState in src/cloud.ts; repeated here because cloud.ts needs Vue, which the test container does not have)
interface PheState {
  status: 'offline' | 'connecting' | 'online'
  error: string
  dev: string
  admin: boolean
  page: PageInfo
  layers: Layer[]
  regions: Region[]
  focus: Focus
  loading: boolean
  dialogs: Dialog[]
  downloadNote: string
  deviceList: DeviceInfo[]
  adminError: string
  tabs: TabInfo[]
  activeView: number
  sessions: SessionInfo[]
  features: string[]
  settings: { q: number; gray: boolean }
}

interface PheHook {
  state: PheState
  forget(): Promise<void>
  // hub and API calls signed with this device's key; the answer is the server's JSON
  hubCall(msg: HubRequest): Promise<any>
  apiCall(method: string, path: string, body?: unknown): Promise<any>
  // the Browser page, while it is open
  renderer: { view: { scrollY: number; panX: number; zoom: number } }
  regionClientPos(q: string | null, kind?: string): { x: number; y: number; scrollY: number; zoom: number } | null
}

declare global {
  interface Window {
    __phe: PheHook
    // the Android phone's clipboard, as last sent by the bridge
    __clip?: string
  }

  // the Android bridge's standalone page (deploy/android): its audio state, read by test/android.ts
  interface Window {
    __audioMsg?: string
    __audioFrames?: number
  }
}
