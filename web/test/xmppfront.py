# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# The XMPP front, second half: two Jabber clients (slixmpp, not the Go library the front uses) sign in with the logins xmppfront.ts
# made, chat device to device both ways, and get an error for an address no connection takes. TLS=1.0 forces what the Nokia N9 can
# do (TLS 1.0, ECDHE-RSA AES-CBC); TLS=modern leaves it to the client.
import asyncio, json, os, ssl, sys
import slixmpp

LOGINS = json.load(open(os.environ["OUT"] + "/xmppfront-logins.json"))
OLD = os.environ.get("TLS") == "1.0"


def context():
    c = ssl.create_default_context()
    c.check_hostname, c.verify_mode = False, ssl.CERT_NONE  # Cloud Companion's own certificate (the N9: "ignore certificate errors")
    if OLD:
        c.minimum_version = c.maximum_version = ssl.TLSVersion.TLSv1
        c.set_ciphers("ECDHE-RSA-AES128-SHA:@SECLEVEL=0")
    return c


class Phone(slixmpp.ClientXMPP):
    def __init__(self, name):
        super().__init__(name + "@cloud-companion/phone", LOGINS[name])
        self.ssl_context = context()
        self.got, self.errors, self.ready = asyncio.Queue(), asyncio.Queue(), asyncio.Event()
        self.add_event_handler("session_start", self.start)
        self.add_event_handler("message", lambda m: m["type"] != "error" and self.got.put_nowait(m))
        self.add_event_handler("message_error", self.errors.put_nowait)
        self.add_event_handler("failed_auth", lambda _: sys.exit("refused: " + name))

    async def start(self, _):
        self.send_presence()
        await self.get_roster()
        self.ready.set()

    def tls_version(self):
        return self.transport.get_extra_info("ssl_object").version()


async def wait(q, what):
    try:
        return await asyncio.wait_for(q.get(), 15)
    except asyncio.TimeoutError:
        sys.exit("timed out: " + what)


async def main():
    n9, e7 = Phone("n9"), Phone("e7")
    for p in (n9, e7):
        p.connect(host="localhost", port=5222)
        await asyncio.wait_for(p.ready.wait(), 20)
    v = n9.tls_version()
    assert v == ("TLSv1" if OLD else "TLSv1.3"), v
    print(f"[{v}] both signed in")

    n9.send_message(mto="e7@cloud-companion", mbody="hello from the N9", mtype="chat")
    m = await wait(e7.got, "the message at e7")
    assert m["body"] == "hello from the N9" and m["from"].bare == "n9@cloud-companion", m
    e7.send_message(mto=m["from"].bare, mbody="hello back", mtype="chat")
    m = await wait(n9.got, "the answer at n9")
    assert m["body"] == "hello back" and m["from"].bare == "e7@cloud-companion", m
    print(f"[{v}] device to device, both ways")

    n9.send_message(mto="nobody.here@cloud-companion", mbody="anyone?", mtype="chat")
    m = await wait(n9.errors, "an error for an unknown address")
    assert m["error"]["condition"] == "item-not-found", m
    print(f"[{v}] an address nobody takes comes back as an error")
    for p in (n9, e7):
        p.disconnect()


asyncio.run(main())
