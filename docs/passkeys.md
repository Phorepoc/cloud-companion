# Signing in to the web app: passkeys and a recovery code

The web app can be used with a **device pairing code** (as always) or, once the owner has set it up, with a **passkey** (WebAuthn:
a fingerprint, a face, a phone or a security key) and a **recovery code** as the fallback. Pairing is unchanged: a sign-in is just
another way to obtain a device (an admin web device, kept in the browser like a paired one); phones keep pairing with codes.

## Set up (once)
1. On the server make an **admin** pairing code: `docker exec phe-cloud phecloud paircode --admin`.
2. Open the web app, enter that code in the pairing field and press **Set up a passkey**. The browser asks you to create a passkey.
3. A **recovery code** (`ABCD-EFGH-JKLM-NPQR`) is shown once. Save it somewhere safe. **I saved it** continues into the app.
More passkeys (another computer, a phone) are added the same way with a new admin code (**Add a passkey**).

## Sign in
Open the web app and press **Sign in with a passkey**. Lost every passkey: **Use the recovery code instead**. The code works once
(dashes, spaces and case do not matter) and a **new** recovery code is shown for next time; the old one stops working.
A plain (non-admin) pairing code can never register a passkey. After five minutes an unfinished passkey request expires, and
more than eight wrong attempts from one address within a minute are refused for the rest of that minute.

## Requirements
- WebAuthn only works on a **secure origin**: `https://`, or `http://localhost`. Use the TLS reverse proxy (`deploy/Caddyfile*`);
  behind a proxy it must send `X-Forwarded-Proto: https` and keep the `Host` header.
- The relying party id is the **host name** you browse to (not an IP address). A passkey made at `https://cloud.example.org` only
  works there. `PHECLOUD_RP_ID` and `PHECLOUD_RP_ORIGINS` (comma separated) set them explicitly (for example when a proxy rewrites
  the host).
- Passkeys and the recovery code hash are stored in the server database (`/data/phecloud.db`); back it up with the rest of the
  state (see `docs/hub.md`, Backup).

## How it works (short)
`/passkey/*` on the web port: `register/begin|finish` (needs an unused admin code, consumed on success), `login/begin|finish`,
`recover`, `status`. Verification is done by github.com/go-webauthn/webauthn (BSD-3-Clause); the recovery code is 100 random bits,
stored as a SHA-256 hash and compared in constant time. A successful sign-in creates an admin device (id and key) exactly like a
pairing would; revoke it from the Devices page like any other. Tested with a Chromium virtual authenticator
(`web/test/passkey.sh`): registration, sign-in, sign counter, recovery code (sloppy input, single use, replacement), refusal of a
non-admin code, unchanged pairing; light and dark screenshots in `web/test/screenshots/passkeys/`.

The endpoints live under `/passkey/` on purpose: the hub's reverse proxy (`deploy/hub/Caddyfile.hub`) sends `/auth` and `/oauth` to the mail
server (its OAuth endpoints), so a phecloud route under `/auth` would never reach phecloud.
