// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package connectors

// Outlook.com / Microsoft 365 contacts and calendar through Microsoft Graph (JSON, not CardDAV/CalDAV), mapped into the hub's collections like any other source.
//   contacts:  GET /me/contacts/delta        -> vCards (<account>-<id>.vcf), links (hub contact <-> "graph:<id>")
//   calendar:  GET /me/calendarView/delta    (a window: 30 days back, 180 ahead; Prefer: outlook.timezone="UTC") -> iCalendar events (<account>-<id>.ics)
// Delta queries: the first run reads everything and keeps the @odata.deltaLink; later runs ask only for what changed since (changed items again, "@removed" items
// deleted). If Graph forgets the delta state (HTTP 410) the next run starts over. The access token is a Bearer credential (Cred) of an account whose
// config says "oauth":"microsoft". One way: Outlook -> hub; Outlook is never written.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/phorepoc/phecloud/internal/davhub"
	"github.com/phorepoc/phecloud/internal/proto"
)

// GraphURL is the Graph endpoint (PHECLOUD_GRAPH_URL overrides it, for tests and national clouds).
func GraphURL() string {
	return strings.TrimRight(envOr("PHECLOUD_GRAPH_URL", "https://graph.microsoft.com/v1.0"), "/")
}

// GraphStore is what the sync remembers and links (store.Store).
type GraphStore interface {
	SyncedGet(account, key string) (string, bool)
	SyncedSet(account, key, value string)
	SyncedDelete(account, key string)
	LinkContact(hubID, source, entryID string) error
	UnlinkEntry(source, entryID string)
}

type graphPage struct {
	Value     []json.RawMessage `json:"value"`
	NextLink  string            `json:"@odata.nextLink"`
	DeltaLink string            `json:"@odata.deltaLink"`
}

var errGone = fmt.Errorf("graph: the delta state is gone")

func graphGet(ctx context.Context, token, u string) (graphPage, error) {
	var pg graphPage
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return pg, err
	}
	if tok, ok := isCred(token); ok {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("Prefer", `outlook.timezone="UTC", odata.maxpagesize=100`)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return pg, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == 410:
		return pg, errGone
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return pg, fmt.Errorf("login refused (HTTP %d): sign in again, and check the app has the Contacts.Read / Calendars.Read permissions", resp.StatusCode)
	case resp.StatusCode/100 != 2:
		return pg, fmt.Errorf("Microsoft Graph answered HTTP %d", resp.StatusCode)
	}
	return pg, json.Unmarshal(b, &pg)
}

// TestGraph checks the login by asking for one item.
func TestGraph(ctx context.Context, c DAVCfg, token string) (int, error) {
	u := GraphURL() + "/me/contacts?$top=1"
	if strings.HasSuffix(c.URL, "/calendarView") {
		u = GraphURL() + "/me/calendars?$top=1"
	}
	pg, err := graphGet(ctx, token, u)
	return len(pg.Value), err
}

type gRemoved struct {
	Reason string `json:"reason"`
}

type gContact struct {
	ID             string    `json:"id"`
	Removed        *gRemoved `json:"@removed"`
	DisplayName    string    `json:"displayName"`
	GivenName      string    `json:"givenName"`
	Surname        string    `json:"surname"`
	CompanyName    string    `json:"companyName"`
	PersonalNotes  string    `json:"personalNotes"`
	MobilePhone    string    `json:"mobilePhone"`
	HomePhones     []string  `json:"homePhones"`
	BusinessPhones []string  `json:"businessPhones"`
	Emails         []struct {
		Address string `json:"address"`
	} `json:"emailAddresses"`
}

type gEvent struct {
	ID      string    `json:"id"`
	Removed *gRemoved `json:"@removed"`
	Subject string    `json:"subject"`
	Body    struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	BodyPreview string `json:"bodyPreview"`
	IsAllDay    bool   `json:"isAllDay"`
	Start       struct {
		DateTime string `json:"dateTime"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
	} `json:"end"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	IsCancelled bool `json:"isCancelled"`
}

func hubName(account, id string) string { return account + "-" + unsafeName.ReplaceAllString(id, "_") }

// SyncGraph pulls one Graph collection (kind "card" = contacts, "cal" = calendar) into the hub; returns what changed and what was removed.
func SyncGraph(ctx context.Context, account, kind string, c DAVCfg, token string, hub HubDAV, st GraphStore) (changed, removed int, err error) {
	deltaKey := "graph:delta:" + kind
	for attempt := 0; attempt < 2; attempt++ {
		next, _ := st.SyncedGet(account, deltaKey)
		if next == "" {
			if kind == "card" {
				next = GraphURL() + "/me/contacts/delta"
			} else {
				now := time.Now().UTC()
				q := url.Values{"startDateTime": {now.AddDate(0, 0, -30).Format("2006-01-02T15:04:05")}, "endDateTime": {now.AddDate(0, 0, 180).Format("2006-01-02T15:04:05")}}
				next = GraphURL() + "/me/calendarView/delta?" + q.Encode()
			}
		}
		changed, removed = 0, 0
		var delta string
		var pending []func() error
		gone := false
		for pages := 0; next != "" && pages < 100; pages++ {
			pg, err := graphGet(ctx, token, next)
			if err == errGone {
				st.SyncedDelete(account, deltaKey)
				gone = true
				break
			}
			if err != nil {
				return changed, removed, err
			}
			for _, raw := range pg.Value {
				ch, rm, perr := applyGraphItem(account, kind, raw, hub, st)
				if perr != nil {
					return changed, removed, perr
				}
				changed += ch
				removed += rm
			}
			next, delta = pg.NextLink, pg.DeltaLink
		}
		_ = pending
		if gone {
			continue // start over without the stale state
		}
		if delta != "" {
			st.SyncedSet(account, deltaKey, delta) // only after every page was applied
		}
		return changed, removed, nil
	}
	return changed, removed, fmt.Errorf("Microsoft Graph kept answering that the sync state is gone")
}

func applyGraphItem(account, kind string, raw json.RawMessage, hub HubDAV, st GraphStore) (changed, removed int, err error) {
	var id string
	var rm *gRemoved
	if kind == "card" {
		var g gContact
		if err := json.Unmarshal(raw, &g); err != nil {
			return 0, 0, nil
		}
		id, rm = g.ID, g.Removed
		if rm == nil {
			name := hubName(account, id)
			if err := hub.PutRaw("card", name+".vcf", davhub.WriteVCard(contactFromGraph(g), "msgraph-"+id, nil)); err != nil {
				return 0, 0, err
			}
			st.SyncedSet(account, "graph:c:"+id, name+".vcf")
			st.LinkContact(name, account, "graph:"+id)
			return 1, 0, nil
		}
	} else {
		var g gEvent
		if err := json.Unmarshal(raw, &g); err != nil {
			return 0, 0, nil
		}
		id, rm = g.ID, g.Removed
		if rm == nil {
			name := hubName(account, id)
			if g.IsCancelled {
				rm = &gRemoved{Reason: "cancelled"}
			} else {
				if err := hub.PutRaw("cal", name+".ics", eventFromGraph(g, "msgraph-"+id)); err != nil {
					return 0, 0, err
				}
				st.SyncedSet(account, "graph:e:"+id, name+".ics")
				return 1, 0, nil
			}
		}
	}
	// removed at Outlook: delete the hub copy and the link
	key := "graph:e:" + id
	if kind == "card" {
		key = "graph:c:" + id
	}
	if v, ok := st.SyncedGet(account, key); ok {
		if err := hub.DeleteRaw(kind, v); err != nil {
			return 0, 0, err
		}
		st.SyncedDelete(account, key)
		if kind == "card" {
			st.UnlinkEntry(account, "graph:"+id)
		}
		return 0, 1, nil
	}
	return 0, 0, nil
}

func contactFromGraph(g gContact) proto.Contact {
	c := proto.Contact{Name: g.DisplayName, Given: g.GivenName, Family: g.Surname, Org: g.CompanyName, Note: g.PersonalNotes}
	if c.Name == "" {
		c.Name = strings.TrimSpace(g.GivenName + " " + g.Surname)
	}
	if g.MobilePhone != "" {
		c.Phones = append(c.Phones, proto.Labeled{Type: "cell", Value: g.MobilePhone})
	}
	for _, p := range g.HomePhones {
		c.Phones = append(c.Phones, proto.Labeled{Type: "home", Value: p})
	}
	for _, p := range g.BusinessPhones {
		c.Phones = append(c.Phones, proto.Labeled{Type: "work", Value: p})
	}
	for _, e := range g.Emails {
		if e.Address != "" {
			c.Emails = append(c.Emails, proto.Labeled{Type: "home", Value: e.Address})
		}
	}
	return c
}

func icsEscape(s string) string {
	return strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\r\n", "\\n", "\n", "\\n").Replace(s)
}

// eventFromGraph writes a VEVENT. Times arrive in UTC (the Prefer header); an all-day event is a DATE value.
func eventFromGraph(g gEvent, uid string) string {
	stamp := func(s string) string { // 2026-11-01T09:00:00.0000000 -> 20261101T090000
		s = strings.SplitN(s, ".", 2)[0]
		return strings.NewReplacer("-", "", ":", "").Replace(s)
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Phecloud//Microsoft Graph//EN\r\nBEGIN:VEVENT\r\n")
	b.WriteString("UID:" + uid + "\r\nDTSTAMP:" + time.Now().UTC().Format("20060102T150405Z") + "\r\n")
	s, e := stamp(g.Start.DateTime), stamp(g.End.DateTime)
	if g.IsAllDay {
		b.WriteString("DTSTART;VALUE=DATE:" + s[:8] + "\r\nDTEND;VALUE=DATE:" + e[:8] + "\r\n")
	} else {
		b.WriteString("DTSTART:" + s + "Z\r\nDTEND:" + e + "Z\r\n")
	}
	b.WriteString("SUMMARY:" + icsEscape(g.Subject) + "\r\n")
	if g.Location.DisplayName != "" {
		b.WriteString("LOCATION:" + icsEscape(g.Location.DisplayName) + "\r\n")
	}
	desc := g.BodyPreview
	if g.Body.ContentType == "text" && g.Body.Content != "" {
		desc = g.Body.Content
	}
	if desc != "" {
		b.WriteString("DESCRIPTION:" + icsEscape(desc) + "\r\n")
	}
	b.WriteString("END:VEVENT\r\nEND:VCALENDAR\r\n")
	return b.String()
}

var _ = os.Getenv
