# Files and photos from your other clouds

Connections → Add has three kinds for files that live somewhere else:

- **Files: Nextcloud.** Type your Nextcloud's address and press *Sign in with Nextcloud*. Nextcloud opens in a new tab and asks
  you to approve Cloud Companion; it then hands over an app password of its own, which you can revoke in Nextcloud → Settings →
  Security at any time. You can also paste an app password yourself. Signing in with an email address works: Cloud Companion
  asks Nextcloud for your user ID.
- **Files: WebDAV.** The address of the folder to show, a user and a password. Any WebDAV server: a NAS, ownCloud, Apache or
  nginx with WebDAV, a hosting provider.
- **Files: Seafile.** Your Seafile's address, your email address and password. With two-step login turned on, use a Web API
  token instead (Seafile → Settings → Web API Auth Token). Each library is a folder; encrypted libraries are left out, since
  their password would have to be kept here.

Tick *Accept a certificate that cannot be checked* only for a server of your own with a self-made certificate.

Each account becomes a folder at the top of Files, named after the account. Inside, files open, play and download on every
device, streamed from the service as they are read (seeking in a video or a song works). They are read-only for now: upload,
new folders, renaming, deleting and share links are only offered for Cloud Companion's own files.

Paired devices see the same through the API: `GET /phe/files` lists the accounts at the top with `source` and `readonly`, their
paths begin with `~<account id>/`, and `POST /phe/files/ticket` gives a streaming link for a file in them.

## Tests

- `go test ./internal/remote ./internal/webui`: a WebDAV server and a stand-in for Seafile's API, through the Files API.
- `sh testdata/remote/test.sh`: a real Nextcloud and a real Seafile 14 (with MariaDB and Redis), signing in, listing, and
  streaming a byte range of a photo.
- `sh web/test/remotefiles.sh`: the real UI against a real Nextcloud: *Sign in with Nextcloud*, approval in Nextcloud's own
  pages, Test, Add, then the read-only folder in Files and a photo opened from it.

# Photos

Photos shows your pictures and videos in one timeline, newest first, grouped by month, and as albums:

- **Immich.** Connections → Add → *Photos: Immich*: your Immich's address and an API key (Immich → Account settings → API keys).
  The timeline and albums come from Immich, with its thumbnails and previews; videos play in the version Immich made for browsers.
  The albums also show in Files as folders, so devices that only know files can reach them.
- **Photo folders.** In Files, a folder's menu has *Show in Photos* (for the owner). Photos and videos in it, and in the folders
  in it (four deep, up to 5000), join the timeline by the time the file was last changed, and the folder is an album. A folder
  can be on Cloud Companion or in another service's files. *Photo folders* in Photos lists them and removes them.

Everything is read-only for now. Paired devices use the same API: `GET /phe/photos` (the timeline, in pages with `before`),
`GET /phe/photos/albums`, `GET /phe/photos/album?src=&id=`, and `GET /phe/photos/img?t=&src=&id=&size=thumb|preview` for the
pictures, with the `ticket` from the listing as `t`.

Tests: `go test ./internal/webui` (a stand-in Immich and a photo folder through the API), `sh testdata/immich/test.sh` (a real
Immich 3) and `sh web/test/photos.sh` (the real UI against a real Immich: adding it, marking a folder, the timeline, the viewer,
albums, removing a photo folder).

# Files in other apps (WebDAV)

Cloud Companion is also a WebDAV server at `/dav/` on its web address (Files → *Use from other apps* shows it). File managers
and phones open it like a network drive: Dolphin and Nautilus (`davs://`), Windows (map a network drive), macOS (Connect to
Server), iOS Files, rclone, and the WebDAV apps of Symbian and the N9.

Sign in with a phone login (Connections → Messaging → Phone logins): one login per device, the same one its SIP and XMPP apps
use, and removing it shuts the device out of all three. Wrong passwords slow down, a second more for each in a row.

Your own files can be read and changed there. Next to them, each account of another service (Nextcloud, WebDAV, Seafile,
Immich albums) is a read-only folder: its files are streamed from where they live (seeking works), and can be copied into your
own files. Uploads follow the same size limit as the Files section.

Tests: `go test ./internal/webui` (a WebDAV session against the server, with an account's folder, writes refused there, and
links out of the files folder refused) and `sh web/test/dav.sh` (a login made in the UI, then rclone, a real client: listing,
reading, a byte range, writing, moving, deleting, a wrong password).

# On TVs and players (DLNA)

Cloud Companion can be a DLNA (UPnP) media server: smart TVs, game consoles, receivers, Kodi, VLC, BubbleUPnP and the media
apps of the N9 and Symbian find it on their own and play your music, videos and photos. It shares Files: your own folders and
those of your other services (Nextcloud, WebDAV, Seafile, Immich albums), showing only folders, music, videos and pictures.

DLNA has no passwords: everyone on your network can browse and play what it shares. So it is off until you turn it on:

```yaml
services:
  phecloud:
    network_mode: host          # players find servers by multicast, which needs the home network itself
    environment:
      PHECLOUD_DLNA_LISTEN: ":8200"
      PHECLOUD_DLNA_NAME: "Living room cloud"   # the name TVs show; "Cloud Companion" when unset
```

With `network_mode: host` the other ports are the machine's own too; drop any `ports:` entries. On a server that is reachable
from the internet, make sure port 8200 is not: keep it to your home network.

Tests: `go test ./internal/dlna` (the description, Browse in pages, a song played with a byte range, other files never shared,
discovery answers) and `sh testdata/dlna/test.sh` (async-upnp-client, the UPnP library Home Assistant uses, finds the server by
multicast on a Docker network, browses to a song and plays part of it).
