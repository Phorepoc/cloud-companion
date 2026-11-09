# The Browser page: Firefox on your server

The Browser page shows Firefox running in its own container (`deploy/browser`), streamed to the page by Neko's server. The tabs,
address bar, menus and dialogs are Cloud Companion's own; nothing of Neko's interface is used. Cloud Companion is single-user, so
there is one Firefox and one profile, shared by all your devices: a tab opened on the phone is there on the laptop.

## Turning it on

- **A board** (installed from a Cloud Companion image): Settings, Features, **Browser**. It needs an 8 GB board and asks for the
  server's address on your network (the picture goes straight to your devices over UDP 59100-59120).
- **By hand**: `docker compose -f docker-compose.yml -f docker-compose.browser.yml up -d`, with `PHECLOUD_BROWSER_PASSWORD` and
  `PHECLOUD_BROWSER_SECRET` (long random strings) and `PHE_BROWSER_IP` (the address your devices reach the server at).

The page needs a secure address (https, or localhost): its requests are signed with the device's key.

| Setting | What it does |
|---|---|
| `PHE_BROWSER_MEM` (3g), `PHE_BROWSER_CPUS` (2) | the container's limits; the memory also sets how many tabs stay loaded |
| `PHE_BROWSER_CA` | a CA certificate (PEM, a path inside the container) Firefox trusts too, for sites on your own network |
| `PHECLOUD_DOWNLOADS_AT` | where Files' Downloads folder is inside Cloud Companion, if you moved `PHECLOUD_FILES_DIR` |

## How it fits together

- **Picture and input**: Neko's WebRTC stream through `@m1k1o/neko`'s client (`web/src/browserEngine.ts`). Cloud Companion
  proxies Neko's API (`/phe/browser/neko/`); only the media goes directly. Each device logs in to Neko as itself.
- **Everything else**: our Firefox extension (`deploy/browser/ext`) talks to Cloud Companion (`/phe/browser/ext`), which relays to
  the Browser pages (`/phe/browser/bridge`): tabs, navigation, zoom, downloads, Desktop site, bookmarks and history, file inputs,
  passkeys, and the security of each page (Firefox's view of the connection, not just "https").
- **Screen size**: Xorg offers 110 sizes (`deploy/browser/xorg.conf`), phones and tablets in portrait included; the device in use
  (the one pressed or focused last) gets the largest that fits it.

## Your data

Everything stays with Cloud Companion; the container itself keeps nothing:

- the Firefox profile (sign-ins, cookies, settings, bookmarks, history) is the `phecloud-browser` volume;
- downloads go to Files, **Downloads** (the `phecloud-downloads` volume, the only part of Cloud Companion's data Firefox sees);
- on a board, the nightly backup copies both, with every other volume.

## What the page does for you

- **Tabs**: any number. Above a number that fits the container's memory (Settings, Browser shows it, and can set your own), the
  background tabs used longest ago are unloaded; they stay in the list and load again when picked.
- **Desktop site**: per tab; off gives the phone version of sites (Firefox for Android's user agent). A phone-sized window opens
  new tabs that way.
- **Send to device**: another of your devices asks whether to open the page in the Browser or in its own browser.
- **Files**: a site's "choose a file" picks files on the device you are using; downloads land in Files.
- **Clipboard**: paste from your device into a page, copy from a page back to your device.

## Passkeys

Firefox here has no authenticator of its own. When a site asks for a passkey, our extension sends the request to the device you
are using, whose own passkeys answer (the OS, a hardware key, a password manager). **Cloud Companion keeps no keys.**

A web page can't make or use a passkey for another site, so the device's app offers a provider to the Browser page
(`web/src/passkeys.ts`):

```js
window.phePasskeys = {
  // kind: 'create' | 'get'; origin: the site's origin; options: its publicKey options as JSON (binary as base64url)
  // resolves with the credential as PublicKeyCredential.toJSON() makes it, with `origin` in its client data
  request({ kind, origin, options }) { ... }
}
```

Planned providers: the Etesian app (Android Credential Manager, as a privileged browser that may name the origin) and a helper for
computers. Without one, the page says so and the site is told "not allowed", so it can offer another way to sign in.

## Tests

`web/test/browser.sh` runs the page against real Firefox: the picture, typing, links, tabs, zoom, Desktop site, Send to device, a
phone, the clipboard, file inputs, passkeys (with and without a provider), certificates, bookmarks and history, downloads, the
tab budget and the profile surviving a new container. CI runs it in the "Browser (Firefox)" job.
