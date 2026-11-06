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
