# phesmssync: the Phorepoc SMS service

A background program for a Phorepoc phone. It mirrors the phone's text messages into your own phecloud server (shown in **Chats**) and sends
the messages you write there. Apache-2.0. Files: `phesmssync.cpp` (the Symbian side), `ss_net.c` (the TLS link, plain C), `../phc*.c` (the
protocol core), `group/phesmssync.mmp`.

## What it does
- Opens the **message server** (`CMsvSession`) and, every 10 seconds or when the store reports a change, collects the SMS entries in the
  **Inbox** (incoming) and **Sent** folder (outgoing) that are newer than its cursor. Each message is uploaded with its **own TMsvId** as the
  message id, so the server can ignore repeats.
- One `sms` frame over the Phecloud link (TLS 1.3, certificate pinned, the device key from `C:\data\phecloud.ini` that the browser engine's
  pairing screen wrote): upload up to 4 messages, report results, receive this phone's **pending outbound queue**.
- Each pending message is built as an SMS in Drafts with `CSmsClientMtm` (recipient, text), moved to the Outbox and scheduled with
  `ESmsMtmCommandScheduleCopy`: the real send path (smss, the SMS stack, ETel). When the message server marks the entry **Sent** or **Failed**
  the next sync reports `sent` / `failed` to the server.
- **Offline**: a failed upload leaves the cursor where it was, so the same messages go out again next cycle; the cursor and the list of
  messages whose result is not reported yet are kept in `C:\data\phesmssync\state.txt`. Nothing is dropped or sent twice (message ids).
- Start it with the phone's startup list (it is an ordinary exe: `!phesmssync` in `bootui.txt`, or from the SSM startup list).

## Tested on Phorepoc x86 (QEMU, the emulated modem)
Boot with the Messaging chain and the networked ROM, a phecloud server on the LAN, a device paired with `phecloud-client`, `C:\data\phecloud.ini`
on a copy of a fresh disk, `!phesmssync` then the telephony probe's `stim|sms|HelloPhorepoc`:
1. the incoming SMS reaches the Inbox (watcher), the service uploads it: **"HelloPhorepoc" appears as a conversation in Chats**
   (`docs/shots/sms-chats-from-x86-phone.png`);
2. a message sent from Chats "through" that phone is picked up (`queued out_id 2 as message 269484039`), the SMS stack submits it to the modem
   (`SMS submitted ... looped back`), the entry appears in Sent, and the thread shows it as **sent** with the phone's own id;
3. the emulated modem loops every sent message back as an incoming one, so the thread also shows the echo as received (a property of the test modem).
Found on the way: `fopen` on `C:\data` did not find the ini from this exe (settings are read with the file server now); an ack that arrives after
the phone's own copy of the message was uploaded used to leave the message queued forever (the server now keeps the phone's row and drops the queued one;
`TestSMSAckAfterOwnCopy`); the sync request is built by `phc_sms_sync` (selftest in `client/test/selftest.c`).

## Not done
A phone with a different message store layout (the 701 / PinePhone ROMs: only the x86 image was run); delivery reports (`delivered`);
a notification when the pairing in `phecloud.ini` is missing (the service logs it and keeps retrying); long messages (over 512 bytes) and MMS.
