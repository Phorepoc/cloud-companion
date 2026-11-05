# Messaging: connections, routing and the SIP front

Cloud Companion passes messages between your devices and the services you use. It is not a messaging service of its own: every
service is a **connection**, your **rules** say which connection an address goes to, and a **front** lets your devices take part.

Connections today: **SMS through your phones** (docs/sms.md), **your SIP devices** (messages between your own logins) and **SIP
services** (your own Asterisk or a provider, one per account), **Matrix**, **XMPP** and **IRC** accounts. Planned, in this order: Nextcloud
Talk, Signal, Telegram; Microsoft Teams later.

Fronts today: the **SIP front** for phones with a SIP client (the Nokia N9, Symbian, softphones) and the **XMPP front** for phones
with a Jabber client (the N9's and Symbian's built-in chat, any XMPP app). Both use the same logins.

## Routing
Connections → **Messaging** in the web app.

- **Rules** are tried in order and the first that matches wins. A rule matches **phone numbers**, or addresses **starting with** a
  prefix you choose (`signal.`, `home.`); the prefix is taken off before the connection sees the address. A rule can carry the
  connection's own choice: for SMS, the phone to send through (else the phone that already has the conversation, else the phone that
  synced last).
- **Without a rule** a phone number goes out as SMS and a login goes to that device, so a server with no rules works as before.
- **Answering goes back the way it came.** A message from a connection reaches your devices from an address that routes back to it:
  a number stays a number when your number rule leads to that connection, otherwise the connection's prefix is put in front.
- **Who gets what arrives**: per connection, the logins that get its messages. Nobody chosen means every login.

| Your device writes to | With these rules | Goes to |
|---|---|---|
| `+491715550100` | none | SMS through the phone that has that conversation |
| `e7` | none | your device with the login `e7` |
| `signal.+491715550100` | `starting with signal.` → Signal | Signal, to +491715550100 |
| `+491715550100` | `phone numbers` → Signal | Signal instead of SMS |

API (owner only): `GET/PUT /phe/routing`, `POST /phe/sip/logins {name}`, `POST /phe/sip/logins/remove {name}` (internal/webui/chat.go).

## SIP services (Asterisk, a provider)
Connections → Add an account → **SIP service**: the server, your user name and password there. Test checks the login without
registering anything. Once added, Cloud Companion registers there like a phone (renewed every 5 minutes; a problem shows on the
account) and the service becomes a connection your rules can send to, labelled with the account's name.

- Messages the service delivers to that account land in the inbox from the sender's extension or address; with a prefix rule
  (`pbx.` → the service) your devices see them from `pbx.201` and answering goes back through the service.
- Only the service's own addresses may deliver; anything else is refused.
- Cloud Companion takes the service's messages on **Messages arrive on** (default `:5070`, one port per SIP service account).
  The service must reach that: inside Docker Compose it does; for a provider on the internet it needs a public address and a
  forwarded port, and the **contact** setting if the address differs.

### Your own Asterisk (compose profile `asterisk`)
For extensions of your own: desk phones, softphones, the N9 as a plain SIP phone, calls between them.
```sh
PHECLOUD_ASTERISK_SECRET=<a password> docker compose --profile asterisk up -d --build
```
The first start writes a configuration into the `asterisk-etc` volume, yours to edit after that: an account `cloud-companion`
(the password above) and extensions 201 and 202 with generated passwords
(`docker compose exec asterisk grep password= /etc/asterisk/pjsip.conf`). Then add the SIP service account: server `asterisk`,
user `cloud-companion`, that password, and a rule such as `starting with pbx.` → it.

| Setting | Default | |
|---|---|---|
| `PHECLOUD_ASTERISK_PORT` | `5062` | host port phones register at (5060 is Cloud Companion's own SIP front) |
| `PHECLOUD_ASTERISK_BIND` | `127.0.0.1` | `0.0.0.0` for phones on your network |
| `PHECLOUD_ASTERISK_PUBLIC` | | this machine's address on your network: calls through Docker's NAT need it |
| `PHECLOUD_ASTERISK_PHONE_201`, `_202` | generated | the extensions' passwords on the first start |

A message to an extension goes to that extension, one to `cloud-companion` comes to Cloud Companion and through your rules;
calls ring the extension (media on UDP 10000-10099). Asterisk is GPL-2.0 and runs in its own container from Alpine's package
(deploy/asterisk); add extensions by copying a 201 block in `pjsip.conf`, then
`docker compose exec asterisk asterisk -rx "module reload res_pjsip.so"`.

## Matrix
Connections → Add an account → **Matrix**: the homeserver (`https://matrix.example.org`), your user (`@you:example.org`, or the name
alone on that homeserver) and password. Test signs a check device in and out again. Once added, Cloud Companion signs in as a device
of its own, **end-to-end encryption included**, and keeps it (and its keys, in `matrix-<account>.db` next to the other data) from then
on; your other Matrix apps may show it as a new device to verify. It joins the chats you are invited to and becomes a connection your
rules can send to.

- **Direct chats** reach your devices from the person: `bob` for someone on your own homeserver, `bob=other.org` for anyone else
  (a SIP phone cannot type `:`). With a rule `starting with mx.` → the account, write to `mx.bob`; a new direct chat is created
  (encrypted) when you have none with them yet, and a name nobody has is refused.
- **Rooms** reach your devices as `!roomid=example.org`, named "sender (room name)"; answering goes to the room.
- Messages from before the account was connected are not replayed. Pictures and files arrive as a line naming them.

## XMPP (Jabber)
Connections → Add an account → **XMPP (Jabber)**: your address (`you@example.org`) and password; the server only when it is not
found from the address (DNS SRV records), and "accept a certificate that cannot be checked" only for a server of your own with a
self-made certificate. TLS is always required. Once added, Cloud Companion signs in as one more device (resource) of your account and
stays online, so messages to your address reach it as well as your other XMPP apps.

- People reach your devices as `bob` (on your own server) or `bob=other.org` (anyone else; a SIP phone cannot type `@`), named as in
  your contact list. With a rule `starting with xm.` → the account, write to `xm.bob`.
- Messages kept by the server while Cloud Companion was offline arrive when it is back. A message the server cannot deliver (no such
  user) is logged. Group chats (MUC) are not passed through yet.

## IRC
Connections → Add an account → **IRC**: the server (`irc.libera.chat`: TLS on port 6697; or `host:port`), your nick, and your
account's password if you have one (signed in with SASL; tick "the password is the server's" for a bouncer such as ZNC instead).
Channels to join are comma separated. Cloud Companion stays connected with that nick and connects again after a drop.

- Private messages reach your devices from the nick (`bob`); with a rule `starting with irc.` → the account, write to `irc.bob`.
- From a channel, only the lines that name you reach your devices (as IRC apps highlight them), named "bob (#team)"; tick "pass on
  every channel line" for all of them. Answering goes to the channel: `irc.#team` (a SIP phone may need to write `#` as `%23`).
- Long messages are split into IRC lines; `/me` lines arrive as `* bob waves`. A message to a nick that is not there is logged.

## The SIP front
Set `PHECLOUD_SIP_LISTEN=:5060` (UDP and TCP). In Docker Compose the port is published on 127.0.0.1 only; set
`PHECLOUD_SIP_BIND=0.0.0.0` for phones on your network. Plain SIP is not encrypted: keep it on a network you trust or a VPN.

Each phone has its own login, made in Connections → Messaging or with:
```sh
phecloud sip add n9      # prints the password once (only a hash is kept)
phecloud sip list
phecloud sip rm n9       # also drops what was waiting only for it
```
A login is 1 to 32 of `a-z 0-9 . _ -`; all-digit names are refused, because those addresses are phone numbers. A new login gets the
messages that arrive after it was made.

Phone settings:

| | |
|---|---|
| User / SIP address | `n9@<server address>` |
| Password | from the login you made |
| Registrar / proxy | `<server address>:5060`, UDP or TCP |
| Realm (if asked) | `cloud-companion` |

The front answers REGISTER, MESSAGE (text/plain only) and OPTIONS; every REGISTER and MESSAGE needs the login's digest, and a device
can only register its own login. A message to an address no rule or default knows is refused (404); one to a connection that cannot
send now, such as SMS with no phone syncing, gets 480. Messages wait for a device that is not registered, in order, until it takes
them. Calls are not handled yet.

## The XMPP front
Set `PHECLOUD_XMPP_LISTEN=:5222`. In Docker Compose the port is published on 127.0.0.1 only; set `PHECLOUD_XMPP_BIND=0.0.0.0` for
phones on your network. It uses the logins of the SIP front (above): a phone can use either front, or both.

It is a small XMPP server for those logins only, with no federation and no accounts of its own. Every address is a contact at
`cloud-companion`: write to `+306912345678@cloud-companion` for an SMS, `xm.bob@cloud-companion` for whatever the `xm.` rule
names, `e7@cloud-companion` for your other device. Messages arrive from those same contacts, with the sender's name (XEP-0172),
so answering goes back the way it came. Characters an XMPP name cannot hold are escaped as XEP-0106 says (`bob@other.org` becomes
`bob\40other.org`). There is no contact list to keep: the front lists nobody, and everyone is reachable.

TLS is required, with a certificate of its own (`legacy.crt` in the data folder, RSA 2048, made at first start; the log prints its
fingerprint). Old phones know neither TLS 1.3 nor the phone port's ECDSA certificate, so this one also accepts TLS 1.0 with AES-CBC.
That is weaker than the phone port: keep the front on a network you trust or a VPN, as with SIP. The password goes over SASL PLAIN
inside TLS.

Phone settings (on the N9: Settings → Accounts → Jabber, then Advanced):

| | |
|---|---|
| Address / user name | `n9@cloud-companion` |
| Password | from the login you made |
| Server | `<server address>` |
| Port | `5222` |
| Encryption | required; ignore certificate errors (the certificate is self-made; compare the fingerprint if you can) |

A message to an address nobody takes comes back as an XMPP error (item-not-found), one to a connection that cannot send now as
recipient-unavailable. Messages wait for a device that is not signed in, in order; a login signed in on two devices gets them on
both. Group chats and file transfers are not handled yet.

## Tests
Every build and test needs the tag `goolm` (`go test -tags goolm ./...`, or `GOFLAGS=-tags=goolm`): Matrix encryption uses
mautrix-go's pure-Go olm instead of cgo and libolm.

- `go test ./internal/chat`: rules, defaults, reply addresses, who gets which connection, SMS taken into the inbox once.
- `go test ./internal/sipgw`: the front on localhost against a SIP client playing the phone (registering, wrong passwords, a number
  queued on the right phone, incoming SMS delivered and answered, device to device with a device that refuses at first).
- `go test ./internal/sipsvc`: a SIP service against a stand-in (digest logins, registering, both directions, the account following).
- `sh deploy/asterisk/test.sh`: the same against the real Asterisk of the profile, extension 201 playing a phone.
- `sh testdata/matrix/test.sh`: Matrix against a real homeserver (Conduit): encrypted direct chats both ways, a group, unknown users.
- `sh testdata/xmpp/test.sh`: XMPP against a real server (Prosody): wrong passwords, both directions, contact names, messages kept
  while offline, signing in again after a dropped connection.
- `go test ./internal/xmppgw`: the XMPP front on localhost (wrong passwords, both directions through a connection with an escaped
  address and a name, refusals as errors, device to device waiting until signed in).
- `go test ./internal/tlsutil`: the front's certificate takes the N9's and Symbian's TLS 1.0 handshakes; the phone port's does not.
- `sh web/test/xmppfront.sh`: logins made through the UI, then two independent Jabber clients (slixmpp) chat through the front,
  over modern TLS and over TLS 1.0 as the N9.
- `sh testdata/irc/test.sh`: IRC against a real server (Ergo): SASL refused and accepted, private messages both ways, only the
  channel lines that name you, a long answer split into lines.
- `go test ./internal/webui -run Routing`: the API, owner only.
- `sh web/test/routing.sh`: the Messaging section through the real UI (logins, rules, order, delivery, after a reload).
- `sh web/test/matrix.sh`: adding a Matrix account through the UI, using it in a rule, removing it.
- `sh web/test/xmpp.sh`: adding an XMPP account through the UI against Prosody, using it in a rule, removing it.
- `sh web/test/irc.sh`: adding an IRC account through the UI against Ergo, using it in a rule, removing it.
- `sh web/test/sipservice.sh`: adding a SIP service through the UI against the profile's Asterisk, using it in a rule, removing it.
