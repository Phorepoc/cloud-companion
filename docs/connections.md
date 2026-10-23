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
`GET/POST /api/connections`, `POST /api/connections/test|sync|remove`, `GET /api/duplicates`, `POST /api/duplicates/merge|dismiss`, `GET /api/links?hub=<id>`.

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
