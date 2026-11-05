# Messaging: connections, routing and the SIP front

Cloud Companion passes messages between your devices and the services you use. It is not a messaging service of its own: every
service is a **connection**, your **rules** say which connection an address goes to, and a **front** lets your devices take part.

Connections today: **SMS through your phones** (docs/sms.md), **your SIP devices** (messages between your own logins) and **SIP
services** (your own Asterisk or a provider, one per account). Planned, in this order: Matrix, XMPP, IRC, Nextcloud Talk, Signal,
Telegram; Microsoft Teams later.

Fronts today: the **SIP front** for phones with a SIP client (the Nokia N9, Symbian, softphones). Planned: XMPP, which the N9 and
Symbian also speak.

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

## Tests
- `go test ./internal/chat`: rules, defaults, reply addresses, who gets which connection, SMS taken into the inbox once.
- `go test ./internal/sipgw`: the front on localhost against a SIP client playing the phone (registering, wrong passwords, a number
  queued on the right phone, incoming SMS delivered and answered, device to device with a device that refuses at first).
- `go test ./internal/sipsvc`: a SIP service against a stand-in (digest logins, registering, both directions, the account following).
- `sh deploy/asterisk/test.sh`: the same against the real Asterisk of the profile, extension 201 playing a phone.
- `go test ./internal/webui -run Routing`: the API, owner only.
- `sh web/test/routing.sh`: the Messaging section through the real UI (logins, rules, order, delivery, after a reload).
- `sh web/test/sipservice.sh`: adding a SIP service through the UI against the profile's Asterisk, using it in a rule, removing it.
