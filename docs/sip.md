# The SIP front

Phones with a SIP client built in (the Nokia N9, Symbian phones, Etesian, any softphone) can register with Cloud Companion and chat
through it. It is not a SIP service of its own: it passes messages on to the services behind it.

- **A message to a phone number** (`sip:+491715550100@<server>`) goes out as an SMS through one of your phones (docs/sms.md): the
  phone that already carries that conversation, else the phone that synced last. It shows in **Chats** like any SMS sent from the web
  app.
- **SMS that arrive on your phones** come to every registered SIP device, from `sip:<number>@cloud-companion`, so answering them
  sends an SMS back. A new login gets the SMS that arrive after it was made, not the old ones.
- **A message to another login** (`sip:e7@<server>`) waits until that device is registered and has taken it.

Calls, and connecting a real SIP service (an Asterisk, a provider) as a connected account, are not built yet.

## Turning it on
Set `PHECLOUD_SIP_LISTEN=:5060` (UDP and TCP). In Docker Compose the port is published on 127.0.0.1 only; set
`PHECLOUD_SIP_BIND=0.0.0.0` for phones on your network. Plain SIP is not encrypted: keep it on a network you trust or a VPN.

## Logins
```sh
phecloud sip add n9      # prints the password once (only a hash is kept)
phecloud sip list
phecloud sip rm n9       # also drops what was waiting for it
```
A login is 1 to 32 of `a-z 0-9 . _ -`; all-digit names are refused, because those addresses are phone numbers.

## Phone settings
| | |
|---|---|
| User / SIP address | `n9@<server address>` |
| Password | from `phecloud sip add` |
| Registrar / proxy | `<server address>:5060`, UDP or TCP |
| Realm (if asked) | `cloud-companion` |

The front answers REGISTER, MESSAGE (text/plain only) and OPTIONS; every REGISTER and MESSAGE needs the login's digest. A device can
only register its own login.

Tests: `go test ./internal/sipgw` runs the gateway on localhost against a SIP client playing the phone (registering, wrong
passwords, a message to a number queued on the right phone, incoming SMS delivered and answered, device to device with a device that
refuses at first, unregistering).
