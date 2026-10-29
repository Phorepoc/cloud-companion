// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The test/debug hook: cloud.ts and BrowserPage.vue put their state and helpers here for the browser tests (typed for the tests in
// web/test/globals.d.ts), and the Android stream the phone's clipboard.
interface Window {
  __phe?: Record<string, unknown>
  __clip?: string
}
