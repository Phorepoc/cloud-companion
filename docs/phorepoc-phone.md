# Use Phecloud from a Phorepoc phone

Phecloud renders web pages on a server you run (a real Chromium), and the phone's browser shows the result: tiles of the page
come over one TLS 1.3 connection, scrolling is local, taps and typing go back to the server. The phone needs no JavaScript engine;
modern sites, logins and cookies work because they run on the server.

You need a machine that stays on and is reachable from the phone (home server, VPS), with Docker, and the Phecloud engine package
installed on the phone next to the Phorepoc browser.

## 1. Start the server
```
git clone <this repository> phecloud && cd phecloud
docker compose up -d
```
The phone protocol listens on TCP **7400** (TLS 1.3, a self-signed certificate created on first start); the web app is on 8080.
Open port 7400 on the machine's firewall for the phone. Put 7400 behind a VPN or a private network if you can: the server renders
pages for whoever is paired, so it should not be a public service.

## 2. Note the certificate fingerprint and make a pairing code
```
docker exec phe-cloud phecloud paircode
```
It prints two things: the **server certificate fingerprint** (SHA-256, 64 hex digits; it is on the first line, on the error
stream) and a one-time **pairing code** such as `ABCD-EF12` (valid for 10 minutes, usable once). The fingerprint is also in the
server log (`docker logs phe-cloud`, line `TLS 1.3 certificate fingerprint=...`). Keep this window open.

## 3. Pair the phone
1. Open the browser and make sure the **Phecloud** engine is the one in use (it is the default when it is the only engine installed;
   otherwise choose it in the browser's engine list).
2. Load any address (or `phecloud:setup`). With no server configured yet the engine shows its setup screen:
   - **Server**: the machine's address, `host` or `host:port` (the port defaults to 7400).
   - **Pairing code**: the code from step 2. Tap a field to type into it, then confirm.
   - Tap **Pair** (or press Enter).
3. The phone connects and shows **Trust this server?** with the certificate fingerprint in four lines of 16 digits.
   **Compare it with the fingerprint from step 2.** If every digit matches, tap **Trust**. If it differs, tap **Cancel**: someone
   else is answering on that address (or you typed the wrong server).
4. The phone now stores the server, the pinned fingerprint and its own device key in `C:\data\phecloud.ini`, and opens the page you
   asked for. From then on it connects without a code and refuses any server whose certificate does not match the pin.

To pair again (new server, new certificate, revoked device) load `phecloud:setup`, or delete `C:\data\phecloud.ini`.

### Without the screen
Create `C:\data\phecloud.ini` yourself:
```
host=your.server.example
port=7400
pin=<the 64 hex digits from step 2>
code=<a fresh pairing code>
name=My phone
```
The first connection pairs, adds `dev=` and `key=` and removes the code.

## Managing devices
On the server: `docker exec phe-cloud phecloud devices` lists paired devices, `... phecloud revoke <id>` removes one (its key stops
working at once). The web app (port 8080) has the same list under Devices.

## What works, what does not
- Works: any site the server's Chromium renders, scripts, cookies (kept per device on the server), forms and text input,
  links, back and forward, JavaScript dialogs, HTTP authentication. Scrolling is instant from cached tiles; long pages load more
  as you scroll.
- Two pages (views) can be open at the same time in the phone browser.
- Not in this version: downloads (the server refuses them and the phone says so), zoom, find in page, private tabs (a cloud session
  is not private from the server's owner), the notice at the end of a truncated page.
- Data use: a page costs tens of KB per screen (about 30 KB for a short page); the byte budget per device can be lowered in the web app's settings.

## If it does not connect
- "Cannot reach the phecloud server": wrong address or port, or the firewall blocks 7400. Test from another machine with
  `openssl s_client -connect host:7400`.
- "The server certificate does not match the pinned fingerprint": the server's certificate changed (new install, wiped data
  volume). Pair again and compare the new fingerprint.
- "Pairing or authentication failed": the code was used, expired, or typed wrongly. Make a new one with `phecloud paircode`.
