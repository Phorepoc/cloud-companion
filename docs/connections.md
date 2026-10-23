# Connections: outside accounts instead of mail hosting

Phecloud does not host your mail. You connect the accounts you already have; the hub (Stalwart) is only the **local cache**:
no MX record, no DKIM, no SPF. **Settings, Connections** in the web app (owner only).

| Kind | What happens |
|---|---|
| Mail (IMAP + SMTP) | The selected folders (default INBOX, last N months, default 3) are pulled into the hub, one hub folder per account and folder (`<account> / INBOX`), keeping read state and date. Each message is pulled once (folder, UIDVALIDITY, UID). At most 300 messages per account and run, so a big mailbox fills in over several runs. Outgoing mail is **relayed through that account's own SMTP server** and a copy is appended to the account's **Sent** folder (found by its special-use flag, else `Sent`). |
| Contacts (CardDAV) | One address book URL. New and changed cards are copied into the hub address book as `<account>-<uid>.vcf`, cards deleted at the source are deleted in the hub copy. One way: outside account to hub. Each copied card is recorded in the **link table** (hub contact id, source account, entry href). |
| Calendar (CalDAV) | The same, for one calendar URL. |

Accounts sync when added, on **Sync** and every 15 minutes. Status, last sync and the last error show on the page. Passwords and tokens are
sealed with AES-256-GCM before they are stored (key: `PHECLOUD_SECRET_KEY`, 64 hex digits, or a random `connectors.key` next to the database;
back both up together). They are never sent back to the browser. Removing an account stops the sync and drops its links; what was already
copied stays in the hub. Use an app password where the provider offers one; OAuth sign-in for Gmail and Outlook is not built yet.

## Duplicate contacts
**Look for duplicates** compares the hub contacts: same phone number (last 9 digits, so +49 171 and 0171 match), same email address, or the
same name (words in any order). For each suggestion: **Keep A** or **Keep B** merges the other into it (phones and emails united, empty fields
filled, notes joined), re-points its links to the kept contact and deletes the other; **Not the same** hides the pair. A source entry that was
merged away is not re-created by later syncs.

## API (admin devices, signed requests as for passkeys)
`GET/POST /phe/connections`, `POST /phe/connections/test|sync|remove`, `GET /phe/duplicates`, `POST /phe/duplicates/merge|dismiss`, `GET /phe/links?hub=<id>`.

## Merging and the sources
Every device and every connected service keeps its **own** contact list. A merge only changes the hub: the data goes into the kept hub contact,
the other hub contact is deleted, and the link table now points every source entry of both to the kept contact. Entries in the sources
(Radicale, Google, a phone) are **never written or deleted** by a merge, and later syncs do not bring the merged-away hub contact back. (Merging
duplicates inside one device's own list is a separate, explicit action that is not built yet.)

## Tested
- Go unit tests: IMAP pull (window, no repeats, per-run cap) and SMTP relay with the Sent copy against in-process IMAP and SMTP servers
  (go-imap, go-smtp); DAV pull against a fake server; sealing; duplicate finder and merge.
- **End to end against a real Stalwart and a real Radicale** (throwaway hub from `web/test/hubtest-up.sh`, `web/test/hubtest-run.sh`):
  IMAP messages imported into a hub folder and read back (3 messages, no repeats on the second run); CardDAV cards and a CalDAV event pulled
  from Radicale into the hub; duplicate finder sees the two cards; the merge on the real hub contacts leaves one contact, both source
  entries linked to it, the Radicale entries byte-identical and still present, and a later sync with a changed source entry does not resurrect
  the merged contact.
- Playwright: `web/test/connections.sh` (add, test with wrong and right password, sync, remove through the UI against Radicale and a go-imap server)
  and `web/test/dups.sh` (the duplicate review against the real hub: both pairs proposed with reasons, Keep merges one pair, Not the same hides
  the other without deleting anything). Light and dark screenshots in `web/test/screenshots/`.
Still not exercised: a real Dovecot/Postfix and the big providers (OAuth is not built).

## Send as
The mail composer has a **From** list when a mail account with an SMTP server and a sender address is connected: the hub address (stays on
this server, as before) or an account. A message sent from an account is relayed through **that account's own SMTP server** and a copy is filed in
its Sent folder. **Replies default to the account the message came from** (the hub folder is named `<account> / <folder>`). A failure from the
account's server (login refused, recipient refused) is shown in the composer.

## Merging duplicates on one device
A suggestion whose two entries both come from the same contacts account also offers **Merge on <account>**. That is the one action that changes
the device's own list, so it is explicit: the dialog lists exactly what will happen (the entry that is rewritten and what it gains, the entry that
is deleted). **Merge on the account** then writes it through the connector: the merged card replaces the first entry and the second is deleted,
both guarded by their ETags, so a change made on the device in the meantime stops the merge and nothing is touched. For 10 minutes an **Undo**
puts both original cards back. The hub copies follow with the account's next sync (run right away). Properties the app does not model are kept.

The Connections API is under `/phe/` (not `/api/`): the hub's Caddy sends `/api` to the mail server.

## Sign in with Google / Microsoft (OAuth)
Gmail and Outlook no longer accept an ordinary password for IMAP/SMTP; they want an OAuth token. Phecloud does the sign-in with **your own app registration**
(a client id and secret you create at the provider; nothing is shared with anyone else). Set the variables, restart phecloud, and the Connections page
offers **Sign in with Google** / **Sign in with Microsoft** (only for the providers that are configured):

| variable | meaning |
|---|---|
| `PHECLOUD_OAUTH_GOOGLE_ID`, `PHECLOUD_OAUTH_GOOGLE_SECRET` | Google OAuth client |
| `PHECLOUD_OAUTH_MS_ID`, `PHECLOUD_OAUTH_MS_SECRET`, `PHECLOUD_OAUTH_MS_TENANT` | Microsoft app (tenant defaults to `common`: work, school and personal accounts) |
| `PHECLOUD_PUBLIC_URL` | the public address (for example `https://cloud.example.org`) when it cannot be derived; behind a reverse proxy set `PHECLOUD_TRUSTED_PROXIES` instead (docs/reverse-proxy.md) |

**The redirect address** you register at the provider is `<public address>/phe/oauth/callback`, for example `https://cloud.example.org/phe/oauth/callback`. Both
providers want a real domain name with https (Google also allows `http://localhost`); an IP address does not work.

**Register the app at Google.** Google Cloud console, new project, *APIs & Services*: enable the *Gmail API*, the *CardDAV API* and the *Google Calendar API*
(for the parts you want); *OAuth consent screen* (External); add the scopes `https://mail.google.com/`, `https://www.googleapis.com/auth/carddav` and
`https://www.googleapis.com/auth/calendar`; *Credentials*, *Create credentials*, *OAuth client ID*, type *Web application*, add the redirect address above; copy the id and secret.
Notes: `https://mail.google.com/` is a *restricted* scope, so a published app needs Google's verification; for your own account keep the app in **Testing**, add yourself as
a test user, and expect to sign in again every 7 days (Google expires refresh tokens of apps in Testing). A Workspace (company) admin can mark an *Internal* app, which has no such limit.

**Register the app at Microsoft.** Microsoft Entra admin center, *App registrations*, *New registration*; supported account types as you need; redirect URI of type *Web* with the
address above; *Certificates & secrets*, new client secret (copy the *value*); *API permissions*, *Add a permission*, *APIs my organization uses*, *Office 365 Exchange Online*,
*Delegated permissions*: `IMAP.AccessAsUser.All` and `SMTP.Send`; then *Microsoft Graph*, *Delegated permissions*: `Contacts.Read` and `Calendars.Read` (for contacts and calendar); also `offline_access`. Notes: a Microsoft 365 mailbox must allow *Authenticated SMTP* for sending; a company
tenant may require an admin to consent.

**What you get.** Pick Mail, Contacts and/or Calendar: each becomes a normal account on the page (the Gmail one is IMAP imap.gmail.com + SMTP smtp.gmail.com with
XOAUTH2; Google contacts and calendar are read over CardDAV and CalDAV with a Bearer token). Outlook **contacts and calendar** come through Microsoft Graph (JSON, not CardDAV/CalDAV; see below).
**Tokens.** The access token (about an hour) is renewed automatically with the refresh token before it expires; both are sealed with the same key as passwords and never
sent to the browser. If you revoke the app at Google/Microsoft, or the refresh token expires, the account shows the problem ("could not renew the sign-in ... sign in again") and you
sign in again. Removing the account deletes the tokens (revoke the app at the provider too if you want it gone there).

**Tested** with a fake OAuth provider and fake IMAP, SMTP and DAV servers (no real Google or Microsoft credentials): code exchange with PKCE, id_token address, refresh and re-sealing,
a revoked grant, XOAUTH2 login on IMAP and SMTP, Bearer on DAV, the redirect and the one-use state, and the Connections page (`web/test/oauth.sh`, `internal/connectors/oauth_test.go`,
`internal/webui/oauth_test.go`). **Not tested against the real providers**: the endpoints, scopes and host names are from their documentation.

### Outlook contacts and calendar (Microsoft Graph)
Pulled one way (Outlook is never written) into the hub's address book and calendar like any other source. Contacts: `GET /me/contacts/delta`; calendar:
`GET /me/calendarView/delta` for a window of 30 days back and 180 days ahead (times asked in UTC). **Delta queries**: the first run reads everything and keeps the `deltaLink`; later runs
fetch only what changed (changed items are written again, items marked `@removed` and cancelled events are deleted from the hub and unlinked); the link is stored only after all pages were
applied, so a failed run repeats the same changes. If Graph forgets the delta state (HTTP 410) the next run starts over. Mapping: display name, given and family name, company, notes, mobile /
home / business phones and email addresses become the vCard (`<account>-<id>.vcf`, recorded in the contact link table as `graph:<id>`); events become iCalendar events (summary, start and end,
all-day as a DATE, location, description). Occurrences of recurring events arrive expanded inside the window as separate events (no recurrence rule is written). **Tokens**: Microsoft gives
one access token per resource (Graph and Outlook mail are different resources), so every account renews its own token with its own scopes; the first renewal happens right after sign-in.
Tested against a fake Graph server (`internal/connectors/graph_test.go`: paging, delta, removal, 410, bad token, mapping); not against the real service.
