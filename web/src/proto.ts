// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// The frames and objects of the phecloud protocol as the web app sees them: the TypeScript side of internal/proto/proto.go.
// Field names are the CBOR keys. Keep this in step with proto.go: a field the server adds is invisible here until it is added.

// ---- the page: tiles, layers, tap regions ----

export type LayerKind = 'fixed' | 'sticky' | 'scroll'
export interface Layer {
  id: number
  kind: LayerKind
  x: number; y: number; w: number; h: number
  z: number
  top?: number | null    // sticky: offset from the top of the view
  bottom?: number | null // sticky: offset from the bottom
  cw?: number; ch?: number // scroll: content size
  sx?: number; sy?: number // scroll: the box's own scroll position at render time
}

export interface SelectOption { l: string; s?: boolean }
export interface Region {
  id: number
  layer: number
  x: number; y: number; w: number; h: number
  kind: 'link' | 'button' | 'input' | 'select' | 'textarea'
  href?: string
  type?: string
  name?: string
  ph?: string // placeholder
  value?: string
  label?: string
  opts?: SelectOption[]
}

export interface PageInfo { pid: number; zoom: number; url: string; title: string; w: number; h: number; back: boolean; fwd: boolean; trunc: boolean }
export interface Focus { kind: string; id: number; type: string; value: string; start?: number; end?: number; im?: string; enter?: string }
export interface Dialog { id: number; kind: string; message: string; def: string }
export interface TabInfo { v: number; title?: string; url?: string }
export interface SessionInfo { sid: number; dev: string; name: string; self?: boolean; tabs?: TabInfo[] }
export interface DeviceInfo { id: string; name: string; created: number; seen: number; revoked?: boolean; admin?: boolean }

// ---- frames from the server ----

interface ViewFrame { v?: number } // the view (tab) a frame is about; 0 / absent = view 1
export interface WelcomeFrame { t: 'welcome'; dev: string; key?: number[] | Uint8Array; server: string; admin?: boolean; features?: string[] }
export interface PageFrame extends ViewFrame { t: 'page'; pid: number; seq: number; url: string; title: string; w: number; h: number; tw: number; th: number; trunc?: boolean; zoom?: number; reset?: boolean; back?: boolean; fwd?: boolean }
export interface TileFrame extends ViewFrame { t: 'tile'; pid: number; layer: number; x: number; y: number; w: number; h: number; fmt: 'png' | 'jpeg'; hash: number; data: Uint8Array }
export interface LayersFrame extends ViewFrame { t: 'layers'; pid: number; seq: number; list?: Layer[] }
export interface RegionsFrame extends ViewFrame { t: 'regions'; pid: number; seq: number; list?: Region[] }
export interface FocusFrame extends ViewFrame { t: 'focus'; pid: number; seq: number; id?: number; kind: string; type?: string; value?: string; start: number; end: number; im?: string; enter?: string }
export interface DoneFrame extends ViewFrame { t: 'done'; pid: number; seq: number; ms: number; tiles: number; bytes: number; more?: boolean }
export interface DialogFrame extends ViewFrame { t: 'dialog'; id: number; kind: string; msg: string; def?: string; url?: string }
export interface DownloadFrame extends ViewFrame { t: 'download'; url: string; name: string; refused: boolean }
export interface DeviceListFrame { t: 'devicelist'; list?: DeviceInfo[] }
export interface TabsFrame { t: 'tabs'; active: number; list?: TabInfo[] }
export interface SessionListFrame { t: 'sessionlist'; list?: SessionInfo[] }
export interface ErrorFrame { t: 'error'; msg: string }
export interface PongFrame { t: 'pong' }
export interface ChallengeFrame { t: 'challenge'; nonce: Uint8Array }
export interface HubResFrame extends HubResult { t: 'hubres'; id: number; ok: boolean; err?: string }

export type ServerFrame = WelcomeFrame | PageFrame | TileFrame | LayersFrame | RegionsFrame | FocusFrame | DoneFrame | DialogFrame | DownloadFrame
  | DeviceListFrame | TabsFrame | SessionListFrame | ErrorFrame | PongFrame | ChallengeFrame | HubResFrame

// ---- the personal hub (contacts, calendar, mail) ----

export interface Labeled { type?: string; value: string }
export interface Contact { href?: string; etag?: string; name: string; given?: string; family?: string; org?: string; note?: string; phones?: Labeled[]; emails?: Labeled[] }
export interface CalendarEvent { href?: string; etag?: string; title: string; start: string; end?: string; allday?: boolean; location?: string; notes?: string; recurs?: boolean; tz?: string }
export interface MailAccount { id: string; name: string; from: string }
export interface AppPassword { id: string; name: string; created?: string; expires?: string }
export interface MailFolder { id: string; name: string; role?: 'inbox' | 'sent' | 'drafts' | 'trash' | 'junk' | 'archive'; total: number; unread: number; parent?: string }
export interface MailItem { id: string; subject: string; from: string; fromaddr?: string; date: string; preview?: string; unread?: boolean; attach?: boolean }
export interface MailBody extends MailItem { to?: string[]; cc?: string[]; text: string; html?: boolean; safe?: string; msgid?: string; folders?: string[] }

/** A hub request: the operation and its arguments (proto.Hub without t and id). */
export interface HubRequest {
  op: string
  contact?: Contact; event?: CalendarEvent
  href?: string; etag?: string
  from?: string; to?: string
  name?: string; days?: number
  id2?: string; folder?: string; pos?: number; flag?: 'read' | 'unread'; role?: string
  rcpt?: string; subject?: string; body?: string; htmlbody?: string; acct?: string; replyto?: string
}

/** What a hub answer can carry; which fields are set depends on the operation. */
export interface HubResult {
  accounts?: MailAccount[]
  contacts?: Contact[]; events?: CalendarEvent[]
  contact?: Contact; event?: CalendarEvent
  apppws?: AppPassword[]; secret?: string; owner?: string; domain?: string
  folders?: MailFolder[]; mails?: MailItem[]; total?: number; mail?: MailBody
}

// ---- the web API (JSON over signed HTTP, internal/webui) ----

/** GET /sms/conversations: one conversation per number (internal/webui/sms.go). */
export interface SmsConversation {
  key: string       // the number reduced for matching
  number: string
  name?: string     // the hub contact's name when the number matches one
  last: string
  last_ts: number   // Unix seconds
  last_dir: 'in' | 'out'
  count: number
  devices: string[] // the phones that carry this conversation
  last_failed: boolean
}
/** A phone that syncs its messages. */
export interface SmsDevice { device: string; name: string; last_sync: number }
/** GET /sms/thread: one message (internal/store/sms.go SMSMessage). */
export interface SmsMessage {
  id: number; device: string; dev_msg_id: string; number: string; key: string
  dir: 'in' | 'out'; body: string; ts: number
  status: 'received' | 'queued' | 'sent' | 'delivered' | 'failed'
  error?: string
}
/** POST /phe/pair/code: a single-use pairing code (internal/webui/pair.go). `expires` is unix seconds. */
export interface PairCodeInfo { code: string; expires: number; ttl: number; admin: boolean; fingerprint: string }

/** GET /passkey/list: one of the owner's passkeys (internal/store/passkey.go). */
export interface PasskeyInfo { id: string; name: string; created: number; last_used: number }

/** GET /phe/connections: a connected account (internal/store/connectors.go Account). */
export interface ConnAccount {
  id: string; kind: 'imap' | 'carddav' | 'caldav' | 'ics'; provider: string; name: string
  config: string // JSON without secrets
  status: 'new' | 'ok' | 'error' | 'syncing'; last_sync: number; last_error: string; created: number
}
/** POST /phe/connections/test and /sync: whether it worked and what happened. */
export interface CheckResult { ok: boolean; detail?: string }
/** GET /phe/oauth/providers: a sign-in provider the server is set up for. */
export interface OAuthProvider { id: string; label: string; kinds: string[] }
/** GET /phe/duplicates: two hub contacts that look like the same person (connectors/dedupe.go Suggestion). */
export interface DupSuggestion { a: string; b: string; name_a: string; name_b: string; reason: string; same_source?: string; source_name?: string }
/** POST /phe/devmerge/preview: what merging two cards on their source will do (connectors/devmerge.go). */
export interface DevMergePreview { id: string; account: string; keep: Contact; drop: Contact; merged: Contact; changes: string[] }

/** GET /phe/catalogues: an app catalogue (internal/webui/catalogue.go describe). */
export interface CatalogueInfo {
  id: string; name: string; public: boolean; priority: number; sync_interval: number; created: number
  index_url: string; hosted_apps: number; sources: number; token?: string
}
export interface CatMirror { sha256: string; state: 'ok' | 'failed' | 'mismatch'; error?: string }
/** A catalogue's upstream source (catalogue.go sourceView). */
export interface CatSourceInfo {
  id: string; catalogue: string; name: string; url: string; has_token: boolean; priority: number; enabled: boolean; mirror: boolean
  last_sync: number; last_error: string; items: number; mirrored_files: number; mirror_problems: CatMirror[]
}
/** A candidate for an app: which source offers which version (internal/catalogue Cand). */
export interface CatCand { source_id: string; source: string; priority: number; version: string; published: string; mirrored: boolean; reason?: string }
/** One app as the catalogue resolves it: the winning candidate and the others (internal/catalogue AppReport). */
export interface CatAppReport { id: string; name: string; winner: CatCand; others?: CatCand[] }
export interface CatBlob { name: string; sha256: string; size: number }
/** A hosted release (internal/catalogue Release). */
export interface CatRelease {
  app_id: string; version: string; name: string; summary?: string; description?: string; category?: string; license?: string; homepage?: string
  authors?: string[]; platform: string; package: string; arch?: string[]; boards?: string[]; min_core?: string; legacy?: boolean; needs_platsec_hack?: boolean
  file: CatBlob; icon?: CatBlob; screenshots?: CatBlob[]; published: number
}
/** POST /phe/catalogue/sync: one source's result. */
export interface CatSyncResult { id: string; name: string; ok: boolean; summary?: unknown; detail?: string }
