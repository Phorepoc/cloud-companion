# Files from your other clouds

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
