# SMS through your own phones

Each phone that syncs SMS is the connector of its own SIM. Phecloud keeps the messages in your server's database and shows them in **Chats**;
a message you send there goes out through the phone you pick.

## Chats
Conversations are listed by **number** (`+49 171 ...` and `0171 ...` are the same conversation, and messages from several phones join one
thread); the name of a matching hub contact is shown when there is one. Under a conversation: **Send through** (defaults to a phone that
already carries the conversation) and the message box. A message you send shows **waiting for the phone**, then **sent**, **delivered** or
**not sent (reason)**, following the phone's reports. **New** starts a conversation with any number.

## Device API (for the phone service)
A paired device signs its requests with its device key: header `X-Phe-Auth: <device id>.<unix time>.<hex HMAC-SHA256(key, "<id>.<time>.<METHOD> <path>")>`
(one minute of clock tolerance). One endpoint does everything:

```
POST /sms/sync
{ "messages": [ { "id": "<the phone's own message id>", "number": "+49 171 5550100", "dir": "in"|"out", "body": "...", "ts": 1760000000, "status": "sent" } ],
  "acks":     [ { "out_id": 12, "id": "<the phone's id for it>", "status": "sent"|"delivered"|"failed", "error": "no signal" } ] }
->
{ "accepted": 3, "pending": [ { "out_id": 12, "number": "0171 5550100", "body": "text to send" } ] }
```
- **Nothing is lost or duplicated.** A message is identified by (device, the phone's message id): uploading it again changes nothing, so the
  phone can resend a whole batch after a dropped connection. At most 500 messages per call; sent messages whose copy the phone uploads later
  (same id as in its ack) are not stored twice.
- **Outbound queue per device.** `pending` is what the web app asked this phone to send; only that phone sees it. The phone sends, then acks with
  the hub's `out_id` and its own id; acked messages leave the queue. A queued message stays until it is acked, so a phone that was off picks
  it up later.
- Short codes and alphanumeric sender ids (`BANK`) are conversations too.
- Reading conversations and sending need the **owner** (admin device): a phone's key cannot read other messages.

Pairing for the Android app (it cannot speak the frame protocol): `POST /sms/pair {"code":"ABCD-EF12","name":"Pixel"}` -> `{"dev":"...","key":"<64 hex>"}`, the same one-time code as `phecloud paircode`; wrong codes are rate-limited. The app is in `android/`.

## Not built yet
The Phorepoc messaging service on the phone, group messages / MMS, encryption of message bodies at rest and the
per-sender "do not sync" list for one-time codes (hub-design.md section 2). Test with a fake phone: `web/test/chats.sh`; the API rules
are covered by `internal/webui/sms_test.go`.
