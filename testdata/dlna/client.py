# SPDX-License-Identifier: Apache-2.0
# A DLNA player's side, with async-upnp-client (Home Assistant's UPnP library): find media servers by SSDP multicast, pick
# Cloud Companion by name, browse it down to a song and play part of it. Prints "ok ..." lines; exits 1 on a failure.
import asyncio, sys, re, html
import aiohttp
from async_upnp_client.search import async_search
from async_upnp_client.aiohttp import AiohttpRequester
from async_upnp_client.client_factory import UpnpFactory

def fail(msg):
    print("FAIL", msg); sys.exit(1)

async def main():
    found = []
    async def seen(headers):
        found.append(headers.get("LOCATION") or headers.get("location"))
    for _ in range(5):
        await async_search(seen, timeout=3, search_target="urn:schemas-upnp-org:device:MediaServer:1")
        if found: break
    if not found: fail("no media server answered the search")
    print("ok   found by SSDP at", found[0])
    factory = UpnpFactory(AiohttpRequester())
    dev = await factory.async_create_device(found[0])
    if dev.friendly_name != "Test Living Room": fail("name: " + dev.friendly_name)
    print("ok   its description: " + dev.friendly_name + ", " + dev.device_type)
    browse = dev.service("urn:schemas-upnp-org:service:ContentDirectory:1").action("Browse")
    async def children(oid):
        r = await browse.async_call(ObjectID=oid, BrowseFlag="BrowseDirectChildren", Filter="*", StartingIndex=0, RequestedCount=0, SortCriteria="")
        return r["Result"]
    top = await children("0")
    titles = re.findall(r"<dc:title>(.*?)</dc:title>", top)
    if titles != ["Music"]: fail("the top: %r (only media and their folders, so not taxes.pdf)" % titles)
    print("ok   the top:", titles)
    music_id = re.search(r'<container id="([^"]+)"', top).group(1)
    music = await children(music_id)
    album_id = re.search(r'<container id="([^"]+)"', music).group(1)
    album = await children(album_id)
    songs = re.findall(r"<dc:title>(.*?)</dc:title>", album)
    if songs != ["01 Sea.mp3"]: fail("the album: %r" % songs)
    url = html.unescape(re.search(r"<res [^>]*>(.*?)</res>", album).group(1))
    print("ok   browsed Music → Album:", songs)
    async with aiohttp.ClientSession() as s:
        async with s.get(url, headers={"Range": "bytes=3-6"}) as r:
            body = await r.read()
            if r.status != 206 or body != b"-012": fail("playing: %d %r" % (r.status, body))
    print("ok   played part of the song from", url)

asyncio.run(main())
print("dlna: all passed")
