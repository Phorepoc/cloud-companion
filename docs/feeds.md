<!-- SPDX-License-Identifier: AGPL-3.0-only -->
# Feeds

Feeds is the owner's list of RSS and Atom feeds. The server reads them (a browser cannot read most feeds itself) and merges their posts,
newest first, for the **Feeds** page, the Feeds widget and the Feeds part of Events. Any paired device reads the posts; only the owner
adds and removes feeds. Read and unread marks follow the person between browsers.

Code: `internal/feeds` (reading and merging), `internal/webui/feeds.go` (the list and the posts), `internal/webui/feedshare.go` (sharing
with feed readers), the **Feeds** page of the web app (`web/src/pages/FeedsPage.vue`). Tests: `internal/webui/feeds_test.go`,
`internal/webui/feedshare_test.go`, `web/test/feeds.ts` (`sh web/test/feeds.sh`).

## Sharing with feed readers
The owner can share the feeds with feed readers that are not paired devices: Feeds on an Etesian phone, FreshRSS, a desktop reader.
Sharing works the way catalogues do. It is **off** until the owner turns it on, under **Share** on the Feeds page, and is then:

- **public**: anybody with the address can read the feeds;
- **private**: the address contains a secret token. **New token** makes a new one, and the old addresses stop working at once.

Each share has three addresses (`.../feeds/<token>/...` when private):

| Address | What it is |
| --- | --- |
| `https://you.example.org/feeds/index.json` | the feeds and their posts as JSON (`{title, feeds, posts, errors}`), for Feeds on an Etesian phone |
| `https://you.example.org/feeds/opml` | the list as OPML, to import into any reader |
| `https://you.example.org/feeds/atom.xml` | the posts of all the feeds as one Atom feed |

A reader that can send headers may use the plain address with `Authorization: Bearer <token>` instead of the token in the path. Wrong
tokens look like a missing page and count against the client's address, as wrong catalogue tokens do. The addresses are built from
`PHECLOUD_PUBLIC_URL`, else from the host the browser used (behind a reverse proxy: set `PHECLOUD_TRUSTED_PROXIES`, see reverse-proxy.md).

What is shared: the feed list and the posts the server has read. Read marks, devices and everything else stay private.

Owner API (admin devices, signed like the other `/phe` calls):

    GET  /phe/feeds/share    {mode, addresses}
    POST /phe/feeds/share    {mode?: "off" | "public" | "private", rotate_token?}
