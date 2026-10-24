// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package davhub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/phorepoc/cloud-companion/internal/proto"
)

// Client is the owner's CardDAV/CalDAV access to the hub. One person, one account: the server acts for the signed-in web app.
type Client struct {
	URL   string // http://stalwart:8080
	Owner string // owner@hub.lan
	Token string // an API key of the owner account (Bearer), or
	Pass  string // the owner password (Basic)
	HTTP  *http.Client
}

// FromEnv builds a Client from PHECLOUD_HUB_URL, PHECLOUD_HUB_OWNER and PHECLOUD_HUB_TOKEN (or ..._OWNER_PASSWORD); nil when the hub
// is not configured.
func FromEnv() *Client {
	c := &Client{URL: os.Getenv("PHECLOUD_HUB_URL"), Owner: os.Getenv("PHECLOUD_HUB_OWNER"), Token: os.Getenv("PHECLOUD_HUB_TOKEN"),
		Pass: os.Getenv("PHECLOUD_HUB_OWNER_PASSWORD"), HTTP: &http.Client{Timeout: 20 * time.Second}}
	if c.URL == "" || c.Owner == "" || (c.Token == "" && c.Pass == "") {
		return nil
	}
	return c
}

func (c *Client) base(kind string) string { // kind = card | cal
	return "/dav/" + kind + "/" + url.PathEscape(c.Owner) + "/default/"
}

func (c *Client) do(method, p string, body string, hdr map[string]string) (int, []byte, http.Header, error) {
	req, err := http.NewRequest(method, c.URL+p, strings.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else {
		req.SetBasicAuth(c.Owner, c.Pass)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, b, resp.Header, nil
}

type multistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Prop struct {
				ETag         string `xml:"getetag"`
				AddressData  string `xml:"address-data"`
				CalendarData string `xml:"calendar-data"`
			} `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

func (c *Client) report(p, body string) (*multistatus, error) {
	st, b, _, err := c.do("REPORT", p, body, map[string]string{"Depth": "1", "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, err
	}
	if st != 207 {
		return nil, fmt.Errorf("hub: HTTP %d", st)
	}
	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, err
	}
	return &ms, nil
}

func newUID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func cleanETag(s string) string { return strings.TrimSpace(s) }

// ---- contacts ----

func (c *Client) Contacts() ([]proto.Contact, error) {
	ms, err := c.report(c.base("card"), `<?xml version="1.0"?><C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:prop><D:getetag/><C:address-data/></D:prop></C:addressbook-query>`)
	if err != nil {
		return nil, err
	}
	var out []proto.Contact
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.AddressData == "" {
				continue
			}
			ct, _, _ := parseVCard(ps.Prop.AddressData)
			ct.Href, ct.ETag = r.Href, cleanETag(ps.Prop.ETag)
			out = append(out, ct)
		}
	}
	return out, nil
}

func (c *Client) get(href string) (string, string, error) {
	st, b, h, err := c.do("GET", href, "", nil)
	if err != nil {
		return "", "", err
	}
	if st != 200 {
		return "", "", fmt.Errorf("hub: HTTP %d", st)
	}
	return string(b), h.Get("ETag"), nil
}

func (c *Client) checkHref(href, kind string) error {
	if !strings.HasPrefix(strings.ReplaceAll(href, "%40", "@"), c.base(kind)) || strings.Contains(href, "..") {
		return errors.New("bad item address")
	}
	return nil
}

// SaveContact creates (Href empty) or updates a contact; unknown vCard lines of an existing contact are kept.
func (c *Client) SaveContact(ct proto.Contact) (proto.Contact, error) {
	if strings.TrimSpace(ct.Name) == "" && strings.TrimSpace(ct.Given+ct.Family) == "" {
		return ct, errors.New("a contact needs a name")
	}
	uid, extra, hdr := newUID(), []string(nil), map[string]string{"Content-Type": "text/vcard; charset=utf-8", "If-None-Match": "*"}
	href := c.base("card") + uid + ".vcf"
	if ct.Href != "" {
		if err := c.checkHref(ct.Href, "card"); err != nil {
			return ct, err
		}
		old, _, err := c.get(ct.Href)
		if err != nil {
			return ct, err
		}
		_, ouid, oextra := parseVCard(old)
		if ouid != "" {
			uid = ouid
		}
		extra, href = oextra, ct.Href
		hdr = map[string]string{"Content-Type": "text/vcard; charset=utf-8"}
		if ct.ETag != "" {
			hdr["If-Match"] = ct.ETag
		}
	}
	st, _, h, err := c.do("PUT", href, writeVCard(ct, uid, extra), hdr)
	if err != nil {
		return ct, err
	}
	if st == 412 {
		return ct, errors.New("this contact was changed somewhere else; reload and try again")
	}
	if st != 201 && st != 204 && st != 200 {
		return ct, fmt.Errorf("hub: HTTP %d", st)
	}
	ct.Href = href
	if et := h.Get("ETag"); et != "" {
		ct.ETag = et
	}
	return ct, nil
}

func (c *Client) Delete(kind, href, etag string) error {
	if err := c.checkHref(href, kind); err != nil {
		return err
	}
	hdr := map[string]string{}
	if etag != "" {
		hdr["If-Match"] = etag
	}
	st, _, _, err := c.do("DELETE", href, "", hdr)
	if err != nil {
		return err
	}
	if st == 412 {
		return errors.New("this item was changed somewhere else; reload and try again")
	}
	if st != 204 && st != 200 {
		return fmt.Errorf("hub: HTTP %d", st)
	}
	return nil
}

// ---- calendar ----

// Events returns the events overlapping [from, to) (ISO dates, default: 30 days ago to one year ahead).
func (c *Client) Events(from, to string) ([]proto.Event, error) {
	f, t := time.Now().AddDate(0, 0, -30), time.Now().AddDate(1, 0, 0)
	if v, err := time.Parse("2006-01-02", from); err == nil {
		f = v
	}
	if v, err := time.Parse("2006-01-02", to); err == nil {
		t = v
	}
	body := fmt.Sprintf(`<?xml version="1.0"?><C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/><C:calendar-data/></D:prop><C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"><C:time-range start="%s" end="%s"/></C:comp-filter></C:comp-filter></C:filter></C:calendar-query>`,
		f.UTC().Format("20060102T150405Z"), t.UTC().Format("20060102T150405Z"))
	ms, err := c.report(c.base("cal"), body)
	if err != nil {
		return nil, err
	}
	var out []proto.Event
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.CalendarData == "" {
				continue
			}
			o := parseICal(ps.Prop.CalendarData)
			if !o.hasEvent {
				continue
			}
			ev := o.ev
			ev.Href, ev.ETag = r.Href, cleanETag(ps.Prop.ETag)
			out = append(out, ev)
		}
	}
	return out, nil
}

// SaveEvent creates (Href empty) or updates an event; recurrence rules, alarms and time zone definitions of an existing event are kept.
func (c *Client) SaveEvent(e proto.Event) (proto.Event, error) {
	if strings.TrimSpace(e.Title) == "" {
		return e, errors.New("an event needs a title")
	}
	if e.Start == "" {
		return e, errors.New("an event needs a start")
	}
	o := ical{uid: newUID()}
	hdr := map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-None-Match": "*"}
	href := c.base("cal") + o.uid + ".ics"
	if e.Href != "" {
		if err := c.checkHref(e.Href, "cal"); err != nil {
			return e, err
		}
		old, _, err := c.get(e.Href)
		if err != nil {
			return e, err
		}
		o = parseICal(old)
		if o.uid == "" {
			o.uid = strings.TrimSuffix(path.Base(e.Href), ".ics")
		}
		href = e.Href
		hdr = map[string]string{"Content-Type": "text/calendar; charset=utf-8"}
		if e.ETag != "" {
			hdr["If-Match"] = e.ETag
		}
	}
	st, _, h, err := c.do("PUT", href, writeICal(o, e), hdr)
	if err != nil {
		return e, err
	}
	if st == 412 {
		return e, errors.New("this event was changed somewhere else; reload and try again")
	}
	if st != 201 && st != 204 && st != 200 {
		return e, fmt.Errorf("hub: HTTP %d", st)
	}
	e.Href = href
	if et := h.Get("ETag"); et != "" {
		e.ETag = et
	}
	return e, nil
}
