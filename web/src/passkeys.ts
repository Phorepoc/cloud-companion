// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// Passkeys for sites in the Browser: Firefox asks the device you are using (deploy/browser/ext/passkey.js). A web page can't make
// or use a passkey for another site (the browser binds it to the page's own origin), so the device's app offers a provider:
//
//   window.phePasskeys = { request({ kind: 'create' | 'get', origin, options }) -> Promise<credential as JSON> }
//
// `options` is the site's publicKey options as JSON (binary as base64url, as in PublicKeyCredential.parseCreationOptionsFromJSON);
// the answer is PublicKeyCredential.toJSON()'s shape, made by the device's own authenticator (Android Credential Manager as a
// privileged browser on Etesian, a desktop helper elsewhere) with `origin` in its client data. Cloud Companion keeps no keys.
export interface PasskeyRequest { kind: 'create' | 'get'; origin: string; options: Record<string, unknown> }
export interface PasskeyProvider { request(r: PasskeyRequest): Promise<Record<string, unknown>> }
export const passkeyProvider = (): PasskeyProvider | null => (window as any).phePasskeys ?? null
