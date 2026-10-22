# SPDX-License-Identifier: Apache-2.0
"""A small Exchange ActiveSync 14.1 test client (stdlib only): WBXML encode/decode for the code pages the hub test needs,
Provision / FolderSync / Sync. Written from the public MS-ASWBXML and MS-ASCMD documents; not a general client."""
import base64, http.client, ssl, urllib.parse

# page -> token -> name. Only the pages used by the tests; unknown tokens decode as "p<page>:0x<tok>".
PAGES = {
    0: {0x05: "Sync", 0x06: "Responses", 0x07: "Add", 0x08: "Change", 0x09: "Delete", 0x0A: "Fetch", 0x0B: "SyncKey",
        0x0C: "ClientId", 0x0D: "ServerId", 0x0E: "Status", 0x0F: "Collection", 0x10: "Class", 0x12: "CollectionId",
        0x13: "GetChanges", 0x14: "MoreAvailable", 0x15: "WindowSize", 0x16: "Commands", 0x17: "Options",
        0x18: "FilterType", 0x1C: "Collections", 0x1D: "ApplicationData", 0x1E: "DeletesAsMoves"},
    1: {0x05: "Anniversary", 0x08: "Birthday", 0x0C: "Business2PhoneNumber", 0x0D: "BusinessAddressCity",
        0x13: "BusinessPhoneNumber", 0x19: "CompanyName", 0x1B: "Email1Address", 0x1E: "FileAs", 0x1F: "FirstName",
        0x27: "HomePhoneNumber", 0x28: "JobTitle", 0x29: "LastName", 0x2B: "MobilePhoneNumber", 0x37: "WebPage"},
    2: {0x0F: "DateReceived", 0x11: "DisplayTo", 0x13: "MessageClass", 0x14: "Subject", 0x15: "Read", 0x16: "To",
        0x18: "From", 0x39: "InternetCPID"},
    4: {0x05: "Timezone", 0x06: "AllDayEvent", 0x07: "Attendees", 0x0D: "BusyStatus", 0x11: "DtStamp", 0x12: "EndTime",
        0x17: "Location", 0x18: "MeetingStatus", 0x19: "OrganizerEmail", 0x1A: "OrganizerName", 0x1B: "Recurrence",
        0x24: "Reminder", 0x25: "Sensitivity", 0x26: "Subject", 0x27: "StartTime", 0x28: "UID"},
    7: {0x05: "Folders", 0x06: "Folder", 0x07: "DisplayName", 0x08: "ServerId", 0x09: "ParentId", 0x0A: "Type",
        0x0C: "Status", 0x0E: "Changes", 0x0F: "Add", 0x10: "Remove", 0x11: "Update", 0x12: "SyncKey", 0x16: "FolderSync",
        0x17: "Count"},
    14: {0x05: "Provision", 0x06: "Policies", 0x07: "Policy", 0x08: "PolicyType", 0x09: "PolicyKey", 0x0A: "Data",
         0x0B: "Status", 0x0C: "RemoteWipe", 0x0D: "EASProvisionDoc"},
    17: {0x05: "BodyPreference", 0x06: "Type", 0x07: "TruncationSize", 0x08: "AllOrNone", 0x0A: "Body", 0x0B: "Data",
         0x0C: "EstimatedDataSize", 0x0D: "Truncated"},
}
NAME2TOK = {(p, n): t for p, tab in PAGES.items() for t, n in tab.items()}


class N:
    """A node: N(page, name, "text") or N(page, name, child, child, ...)."""

    def __init__(self, page, name, *kids):
        self.page, self.name = page, name
        self.text = kids[0] if len(kids) == 1 and isinstance(kids[0], str) else None
        self.kids = [] if self.text is not None else list(kids)
        self.opaque = None

    def find(self, name):
        for k in self.kids:
            if k.name == name:
                return k
        return None

    def findall(self, name):
        return [k for k in self.kids if k.name == name]

    def get(self, name, default=None):
        k = self.find(name)
        return k.text if k is not None and k.text is not None else default

    def dump(self, ind=0):
        s = " " * ind + self.name + (": " + self.text if self.text is not None else "") + "\n"
        return s + "".join(k.dump(ind + 1) for k in self.kids)

    def __repr__(self):
        return self.dump()


def encode(root):
    out = bytearray(b"\x03\x01\x6a\x00")
    cur = [0]

    def emit(n):
        if n.page != cur[0]:
            out.extend([0x00, n.page])
            cur[0] = n.page
        tok = NAME2TOK[(n.page, n.name)]
        has = n.text is not None or n.kids
        out.append(tok | (0x40 if has else 0))
        if n.text is not None:
            out.append(0x03)
            out.extend(n.text.encode())
            out.append(0)
            out.append(0x01)
        elif n.kids:
            for k in n.kids:
                emit(k)
            out.append(0x01)

    emit(root)
    return bytes(out)


def decode(b):
    if not b:
        return None
    i = 4 + b[3]  # version, publicid, charset, string table length (0 in ActiveSync)
    page = 0
    stack, root = [], None
    while i < len(b):
        c = b[i]
        i += 1
        if c == 0x00:
            page = b[i]
            i += 1
        elif c == 0x01:
            stack.pop()
        elif c == 0x03:
            j = b.index(0, i)
            stack[-1].text = b[i:j].decode("utf-8", "replace")
            i = j + 1
        elif c == 0xC3:
            n = 0
            while True:
                x = b[i]
                i += 1
                n = (n << 7) | (x & 0x7F)
                if not x & 0x80:
                    break
            stack[-1].opaque = b[i:i + n]
            i += n
        else:
            tok = c & 0x3F
            node = N(page, PAGES.get(page, {}).get(tok, "p%d:0x%02x" % (page, tok)))
            node.text = None
            if stack:
                stack[-1].kids.append(node)
            else:
                root = node
            if c & 0x40:
                stack.append(node)
    return root


class Eas:
    def __init__(self, host, port, user, password, tls=False, device="PHETEST0001", dtype="SymbianTest", ver="14.1",
                 hostheader=None, min_tls=None):
        self.host, self.port, self.user, self.pw, self.tls = host, port, user, password, tls
        self.device, self.dtype, self.ver = device, dtype, ver
        self.policykey = "0"
        self.ctx = ssl.create_default_context()
        self.ctx.check_hostname = False
        self.ctx.verify_mode = ssl.CERT_NONE
        self.hostheader = hostheader

    def _conn(self):
        if self.tls:
            return http.client.HTTPSConnection(self.host, self.port, context=self.ctx, timeout=60)
        return http.client.HTTPConnection(self.host, self.port, timeout=60)

    def _hdr(self):
        h = {"Authorization": "Basic " + base64.b64encode(f"{self.user}:{self.pw}".encode()).decode(),
             "MS-ASProtocolVersion": self.ver, "User-Agent": "PheEasTest/1", "X-MS-PolicyKey": self.policykey,
             "Content-Type": "application/vnd.ms-sync.wbxml"}
        if self.hostheader:
            h["Host"] = self.hostheader
        return h

    def options(self):
        c = self._conn()
        c.request("OPTIONS", "/Microsoft-Server-ActiveSync", headers=self._hdr())
        r = c.getresponse()
        r.read()
        return r.status, dict((k.lower(), v) for k, v in r.getheaders())

    def cmd(self, name, root):
        q = urllib.parse.urlencode({"Cmd": name, "User": self.user, "DeviceId": self.device, "DeviceType": self.dtype})
        c = self._conn()
        c.request("POST", "/Microsoft-Server-ActiveSync?" + q, body=encode(root), headers=self._hdr())
        r = c.getresponse()
        body = r.read()
        if r.status != 200:
            raise RuntimeError(f"{name}: HTTP {r.status} {body[:200]!r}")
        return decode(body)

    def provision(self):
        pt = N(14, "PolicyType", "MS-EAS-Provisioning-WBXML")
        r = self.cmd("Provision", N(14, "Provision", N(14, "Policies", N(14, "Policy", pt))))
        key = r.find("Policies").find("Policy").get("PolicyKey")
        r2 = self.cmd("Provision", N(14, "Provision", N(14, "Policies", N(14, "Policy", N(14, "PolicyType", "MS-EAS-Provisioning-WBXML"),
                                                                           N(14, "PolicyKey", key), N(14, "Status", "1")))))
        self.policykey = r2.find("Policies").find("Policy").get("PolicyKey")
        return r, r2

    def foldersync(self, key="0"):
        r = self.cmd("FolderSync", N(7, "FolderSync", N(7, "SyncKey", key)))
        ch = r.find("Changes")
        folders = [{k.name: k.text for k in f.kids} for f in (ch.findall("Add") if ch else [])]
        return r.get("SyncKey"), folders, r.get("Status")

    def sync(self, folder, key, commands=None, window=100, body=False):
        coll = [N(0, "SyncKey", key), N(0, "CollectionId", folder)]
        if key != "0":
            coll.append(N(0, "GetChanges", "1"))
            coll.append(N(0, "WindowSize", str(window)))
            opts = [N(0, "FilterType", "0")]
            if body:
                opts.append(N(17, "BodyPreference", N(17, "Type", "1"), N(17, "TruncationSize", "2048")))
            coll.append(N(0, "Options", *opts))
            if commands:
                coll.append(N(0, "Commands", *commands))
        return self.cmd("Sync", N(0, "Sync", N(0, "Collections", N(0, "Collection", *coll))))

    def pull_all(self, folder, body=False):
        r = self.sync(folder, "0")
        key = r.find("Collections").find("Collection").get("SyncKey")
        items = []
        for _ in range(50):
            r = self.sync(folder, key, body=body)
            col = r.find("Collections").find("Collection")
            if col.get("Status") != "1":
                raise RuntimeError("Sync status %s" % col.get("Status"))
            key = col.get("SyncKey")
            cmds = col.find("Commands")
            if cmds:
                for a in cmds.findall("Add"):
                    items.append((a.get("ServerId"), a.find("ApplicationData")))
            if col.find("MoreAvailable") is None:
                break
        return key, items
