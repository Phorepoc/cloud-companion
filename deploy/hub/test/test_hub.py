# SPDX-License-Identifier: Apache-2.0
"""Hub test: seed Stalwart with contacts, events and mail (CardDAV PUT, CalDAV PUT, IMAP APPEND, SMTP), then read them back
through Z-Push with the EAS 14.1 client in eas.py (Provision, FolderSync, Sync for contacts, calendar and mail), add a contact over
EAS and check it in CardDAV. Environment: HUB_STALWART (http://stalwart:8080), HUB_IMAP (stalwart), HUB_USER, HUB_PASSWORD (the
account password, for seeding), EAS_HOST/EAS_PORT/EAS_TLS, EAS_PASSWORD (an app password; default HUB_PASSWORD)."""
import base64, http.client, imaplib, os, smtplib, ssl, sys, time, urllib.parse
from email.message import EmailMessage
from eas import Eas, N

BASE = os.environ.get("HUB_STALWART", "http://stalwart:8080")
USER = os.environ["HUB_USER"]
PW = os.environ["HUB_PASSWORD"]
EAS_PW = os.environ.get("EAS_PASSWORD", PW)
u = urllib.parse.urlparse(BASE)
QUSER = urllib.parse.quote(USER)
fails = []


def check(name, ok, info=""):
    print(("PASS " if ok else "FAIL ") + name + (" -- " + info if info else ""), flush=True)
    if not ok:
        fails.append(name)


def dav(method, path, body=None, headers=None):
    c = http.client.HTTPConnection(u.hostname, u.port or 80, timeout=30)
    h = {"Authorization": "Basic " + base64.b64encode(f"{USER}:{PW}".encode()).decode()}
    h.update(headers or {})
    c.request(method, path, body=body, headers=h)
    r = c.getresponse()
    return r.status, r.read()


CONTACTS = [("Ada", "Lovelace", "ada@example.org", "+441234567890"), ("Grace", "Hopper", "grace@example.org", "+15551230000"),
            ("Linus", "Torvalds", "linus@example.org", "+358401234567")]
EVENTS = [("Dentist", "20261105T090000Z", "20261105T100000Z", "Clinic"), ("Team lunch", "20261106T113000Z", "20261106T130000Z", "Cafe"),
          ("Holiday", None, None, "Everywhere")]
MAILS = [("Hello from the hub", "first test message"), ("Second message", "body two"), ("Third message", "body three")]


def seed():
    for i, (fn, ln, em, tel) in enumerate(CONTACTS):
        vc = (f"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:phe-test-c{i}\r\nFN:{fn} {ln}\r\nN:{ln};{fn};;;\r\nEMAIL;TYPE=INTERNET:{em}\r\n"
              f"TEL;TYPE=CELL:{tel}\r\nORG:Test Org\r\nEND:VCARD\r\n")
        st, _ = dav("PUT", f"/dav/card/{QUSER}/default/phe-test-c{i}.vcf", vc.encode(), {"Content-Type": "text/vcard"})
        check(f"CardDAV PUT {fn} {ln}", st in (201, 204), str(st))
    for i, (sub, s, e, loc) in enumerate(EVENTS):
        if s:
            dt = f"DTSTART:{s}\r\nDTEND:{e}\r\n"
        else:
            dt = "DTSTART;VALUE=DATE:20261224\r\nDTEND;VALUE=DATE:20261227\r\n"
        ics = (f"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//phecloud hub test//EN\r\nBEGIN:VEVENT\r\nUID:phe-test-e{i}\r\n"
               f"DTSTAMP:20261022T120000Z\r\n{dt}SUMMARY:{sub}\r\nLOCATION:{loc}\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
        st, _ = dav("PUT", f"/dav/cal/{QUSER}/default/phe-test-e{i}.ics", ics.encode(), {"Content-Type": "text/calendar"})
        check(f"CalDAV PUT {sub}", st in (201, 204), str(st))
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    m = imaplib.IMAP4_SSL(os.environ.get("HUB_IMAP", "stalwart"), 993, ssl_context=ctx)
    m.login(USER, PW)
    for sub, body in MAILS[:2]:
        msg = EmailMessage()
        msg["From"], msg["To"], msg["Subject"] = "sender@example.org", USER, sub
        msg["Date"] = "Thu, 22 Oct 2026 12:00:00 +0000"
        msg.set_content(body)
        typ, _ = m.append("INBOX", None, None, msg.as_bytes())
        check(f"IMAP APPEND {sub}", typ == "OK")
    m.logout()
    # one through the real delivery path: SMTP on 25 to the local domain
    msg = EmailMessage()
    msg["From"], msg["To"], msg["Subject"] = "sender@example.org", USER, MAILS[2][0]
    msg.set_content(MAILS[2][1])
    with smtplib.SMTP(os.environ.get("HUB_IMAP", "stalwart"), 25, timeout=30) as s:
        s.send_message(msg)
    check("SMTP delivery to the owner", True)
    time.sleep(2)


def eas_test():
    e = Eas(os.environ.get("EAS_HOST", "zpush"), int(os.environ.get("EAS_PORT", "80")), USER, EAS_PW,
            tls=os.environ.get("EAS_TLS") == "1", hostheader=os.environ.get("EAS_HOSTHEADER"))
    st, hdr = e.options()
    vers = hdr.get("ms-asprotocolversions", "")
    check("OPTIONS: 200 and protocol 14.1 offered", st == 200 and "14.1" in vers, f"{st} {vers}")
    e.provision()
    check("Provision (2 phases) gave a policy key", e.policykey not in ("", "0"), e.policykey)
    key, folders, status = e.foldersync()
    check("FolderSync status 1", status == "1", str(status))
    byname = {f["DisplayName"]: f for f in folders}
    print("  folders:", [(f["DisplayName"], f["Type"], f["ServerId"]) for f in folders])
    inbox = next((f for f in folders if f["Type"] == "2"), None)
    cal = next((f for f in folders if f["Type"] == "8"), None)
    con = next((f for f in folders if f["Type"] == "9"), None)
    check("inbox, calendar and contacts folders present", bool(inbox and cal and con))
    if not (inbox and cal and con):
        return e, None
    _, items = e.pull_all(con["ServerId"])
    names = sorted(f"{a.get('FirstName', '')} {a.get('LastName', '')}" for _, a in items)
    print("  contacts:", names)
    check("3 seeded contacts arrive via EAS", all(f"{fn} {ln}" in names for fn, ln, _, _ in CONTACTS), str(names))
    mob = {a.get("LastName"): a.get("MobilePhoneNumber") for _, a in items}
    check("contact phone numbers survive", mob.get("Hopper") == "+15551230000", str(mob))
    _, items = e.pull_all(cal["ServerId"])
    subs = sorted(a.get("Subject", "") for _, a in items)
    print("  events:", subs)
    check("3 seeded events arrive via EAS", all(s in subs for s, _, _, _ in EVENTS), str(subs))
    ev = {a.get("Subject"): a for _, a in items}
    d = ev.get("Dentist")
    check("event start/end/location", d is not None and d.get("StartTime") == "20261105T090000Z" and d.get("Location") == "Clinic",
          str(d.get("StartTime") if d is not None else None))
    _, items = e.pull_all(inbox["ServerId"], body=True)
    subs = sorted(a.get("Subject", "") for _, a in items)
    print("  mail:", subs)
    check("3 seeded mails arrive via EAS", all(s in subs for s, _ in MAILS), str(subs))
    # EAS -> server: add a contact, then see it over CardDAV
    k, _ = e.pull_all(con["ServerId"])
    add = N(0, "Add", N(0, "ClientId", "c1"), N(0, "ApplicationData", N(1, "FirstName", "Eas"), N(1, "LastName", "Created"),
                                                    N(1, "MobilePhoneNumber", "+10005550100"), N(1, "Email1Address", "eas@example.org")))
    r = e.sync(con["ServerId"], k, commands=[add])
    col = r.find("Collections").find("Collection")
    resp = col.find("Responses")
    sid = resp.find("Add").get("ServerId") if resp is not None and resp.find("Add") is not None else None
    check("EAS Add of a contact accepted", sid is not None, r.dump() if sid is None else sid)
    time.sleep(1)
    st, body = dav("REPORT", f"/dav/card/{QUSER}/default/",
                   b'<c:addressbook-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:prop><d:getetag/><c:address-data/></d:prop></c:addressbook-query>',
                   {"Depth": "1", "Content-Type": "application/xml"})
    check("the EAS-created contact is in CardDAV", st == 207 and b"Created" in body and b"+10005550100" in body, str(st))
    return e, (inbox, cal, con)


if __name__ == "__main__":
    mode = sys.argv[1] if len(sys.argv) > 1 else "all"
    if mode in ("all", "seed"):
        seed()
    if mode in ("all", "eas"):
        eas_test()
    print("RESULT:", "FAIL " + ", ".join(fails) if fails else "all passed")
    sys.exit(1 if fails else 0)
