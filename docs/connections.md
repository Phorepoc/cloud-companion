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

## Tested
Go: IMAP pull (window, no repeats, per-run cap) and SMTP relay with the Sent copy against in-process IMAP and SMTP servers (go-imap, go-smtp);
DAV pull (create, unchanged, change, delete, links) against a fake server; sealing; duplicate finder and merge. Playwright (`web/test/connections.sh`):
add, test (wrong and right password), sync, remove through the UI against Radicale and a go-imap server, with light and dark screenshots.
Not yet exercised: the hub side (`MailImport`, `PutRaw`, merge on real contacts) against a live Stalwart, and CalDAV against a real server.
