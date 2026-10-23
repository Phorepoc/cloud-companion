// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package davhub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/phorepoc/phecloud/internal/proto"
)

const (
	capCore = "urn:ietf:params:jmap:core"
	capMail = "urn:ietf:params:jmap:mail"
	capSW   = "urn:stalwart:jmap"
)

// call sends one JMAP request and returns the method responses.
func (c *Client) call(using []string, calls ...[]any) ([]json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"using": using, "methodCalls": calls})
	req, _ := http.NewRequest("POST", c.URL+"/jmap", bytes.NewReader(body))
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else {
		req.SetBasicAuth(c.Owner, c.Pass)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("hub: HTTP %d", resp.StatusCode)
	}
	var r struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return r.MethodResponses, nil
}

// result unpacks ["name", {args}, "id"] and fails on an "error" response.
func result(raw json.RawMessage, into any) error {
	var t []json.RawMessage
	if err := json.Unmarshal(raw, &t); err != nil || len(t) < 2 {
		return fmt.Errorf("bad response %.120s", raw)
	}
	var name string
	json.Unmarshal(t[0], &name)
	if name == "error" {
		return fmt.Errorf("hub: %.200s", t[1])
	}
	return json.Unmarshal(t[1], into)
}

// ---- app passwords (the owner account's own; each device gets a revocable one) ----

func (c *Client) AppPasswords() ([]proto.AppPw, error) {
	rs, err := c.call([]string{capCore, capSW}, []any{"x:AppPassword/get", map[string]any{"ids": nil}, "c1"})
	if err != nil {
		return nil, err
	}
	var g struct {
		List []struct {
			ID, Description, CreatedAt string
			ExpiresAt                  *string
		} `json:"list"`
	}
	if err := result(rs[0], &g); err != nil {
		return nil, err
	}
	var out []proto.AppPw
	for _, p := range g.List {
		a := proto.AppPw{ID: p.ID, Name: p.Description, Created: p.CreatedAt}
		if p.ExpiresAt != nil {
			a.Expires = *p.ExpiresAt
		}
		out = append(out, a)
	}
	return out, nil
}

// AddAppPassword creates one; the secret is returned once and never again.
func (c *Client) AddAppPassword(name string, days int) (id, secret string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", errors.New("give the device a name")
	}
	create := map[string]any{"description": name}
	if days > 0 {
		create["expiresAt"] = time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02T15:04:05Z")
	}
	rs, err := c.call([]string{capCore, capSW}, []any{"x:AppPassword/set", map[string]any{"create": map[string]any{"a": create}}, "c1"})
	if err != nil {
		return "", "", err
	}
	var set struct {
		Created    map[string]struct{ ID, Secret string } `json:"created"`
		NotCreated map[string]json.RawMessage             `json:"notCreated"`
	}
	if err := result(rs[0], &set); err != nil {
		return "", "", err
	}
	if nc, bad := set.NotCreated["a"]; bad {
		return "", "", fmt.Errorf("not created: %.160s", nc)
	}
	cr := set.Created["a"]
	return cr.ID, cr.Secret, nil
}

func (c *Client) RevokeAppPassword(id string) error {
	rs, err := c.call([]string{capCore, capSW}, []any{"x:AppPassword/set", map[string]any{"destroy": []string{id}}, "c1"})
	if err != nil {
		return err
	}
	var set struct {
		Destroyed    []string                   `json:"destroyed"`
		NotDestroyed map[string]json.RawMessage `json:"notDestroyed"`
	}
	if err := result(rs[0], &set); err != nil {
		return err
	}
	if len(set.Destroyed) == 0 {
		return fmt.Errorf("not revoked: %.160s", set.NotDestroyed[id])
	}
	return nil
}

// ---- mail (read-only) ----

func (c *Client) mailAccount() (string, error) {
	req, _ := http.NewRequest("GET", c.URL+"/jmap/session", nil)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else {
		req.SetBasicAuth(c.Owner, c.Pass)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var s struct {
		PrimaryAccounts map[string]string `json:"primaryAccounts"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&s); err != nil {
		return "", err
	}
	if id := s.PrimaryAccounts[capMail]; id != "" {
		return id, nil
	}
	return "", errors.New("this hub account has no mail")
}

func (c *Client) MailFolders() ([]proto.MailFolder, error) {
	acc, err := c.mailAccount()
	if err != nil {
		return nil, err
	}
	rs, err := c.call([]string{capCore, capMail}, []any{"Mailbox/get", map[string]any{"accountId": acc}, "m1"})
	if err != nil {
		return nil, err
	}
	var g struct {
		List []struct {
			ID, Name, Role, ParentID  string
			TotalEmails, UnreadEmails int
		} `json:"list"`
	}
	if err := result(rs[0], &g); err != nil {
		return nil, err
	}
	var out []proto.MailFolder
	for _, m := range g.List {
		out = append(out, proto.MailFolder{ID: m.ID, Name: m.Name, Role: m.Role, Total: m.TotalEmails, Unread: m.UnreadEmails, Parent: m.ParentID})
	}
	return out, nil
}

type addr struct{ Name, Email string }

func (a addr) String() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Email
}

func firstAddr(l []addr) string {
	if len(l) == 0 {
		return ""
	}
	return l[0].String()
}

type jmapEmail struct {
	ID         string          `json:"id"`
	Subject    string          `json:"subject"`
	From       []addr          `json:"from"`
	To         []addr          `json:"to"`
	Cc         []addr          `json:"cc"`
	ReceivedAt string          `json:"receivedAt"`
	Preview    string          `json:"preview"`
	Keywords   map[string]bool `json:"keywords"`
	HasAttach  bool            `json:"hasAttachment"`
	MessageID  []string        `json:"messageId"`
	TextBody   []struct {
		PartID string `json:"partId"`
	} `json:"textBody"`
	HTMLBody []struct {
		PartID string `json:"partId"`
	} `json:"htmlBody"`
	BodyValues map[string]struct {
		Value string `json:"value"`
	} `json:"bodyValues"`
}

func (e jmapEmail) item() proto.MailItem {
	return proto.MailItem{ID: e.ID, Subject: e.Subject, From: firstAddr(e.From), FromAddr: firstEmail(e.From), Date: e.ReceivedAt, Preview: e.Preview, Unread: !e.Keywords["$seen"], Attach: e.HasAttach}
}

// MailList returns one page (30) of a folder, newest first, and the folder's total.
func (c *Client) MailList(folder string, pos int) ([]proto.MailItem, int, error) {
	acc, err := c.mailAccount()
	if err != nil {
		return nil, 0, err
	}
	rs, err := c.call([]string{capCore, capMail},
		[]any{"Email/query", map[string]any{"accountId": acc, "filter": map[string]any{"inMailbox": folder},
			"sort": []map[string]any{{"property": "receivedAt", "isAscending": false}}, "position": pos, "limit": 30, "calculateTotal": true}, "q1"},
		[]any{"Email/get", map[string]any{"accountId": acc, "#ids": map[string]any{"resultOf": "q1", "name": "Email/query", "path": "/ids"},
			"properties": []string{"id", "subject", "from", "receivedAt", "preview", "keywords", "hasAttachment"}}, "g1"})
	if err != nil {
		return nil, 0, err
	}
	var q struct {
		Total int `json:"total"`
	}
	if err := result(rs[0], &q); err != nil {
		return nil, 0, err
	}
	var g struct {
		List []jmapEmail `json:"list"`
	}
	if err := result(rs[1], &g); err != nil {
		return nil, 0, err
	}
	var out []proto.MailItem
	for _, e := range g.List {
		out = append(out, e.item())
	}
	return out, q.Total, nil
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	reBreak  = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/h[1-6]|/li)[^>]*>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank  = regexp.MustCompile(`\n{3,}`)
)

// htmlToText turns an HTML body into plain text: no remote content is ever fetched, links and images are dropped.
func htmlToText(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(reBlank.ReplaceAllString(s, "\n\n"))
}

// MailRead returns one message as plain text (read-only: the message is not marked as read).
func (c *Client) MailRead(id string) (*proto.MailBody, error) {
	acc, err := c.mailAccount()
	if err != nil {
		return nil, err
	}
	rs, err := c.call([]string{capCore, capMail}, []any{"Email/get", map[string]any{"accountId": acc, "ids": []string{id},
		"properties":          []string{"id", "subject", "from", "to", "cc", "receivedAt", "preview", "keywords", "hasAttachment", "textBody", "htmlBody", "bodyValues"},
		"fetchTextBodyValues": true, "fetchHTMLBodyValues": true, "maxBodyValueBytes": 200000}, "g1"})
	if err != nil {
		return nil, err
	}
	var g struct {
		List []jmapEmail `json:"list"`
	}
	if err := result(rs[0], &g); err != nil {
		return nil, err
	}
	if len(g.List) == 0 {
		return nil, errors.New("that message is not there any more")
	}
	e := g.List[0]
	body := &proto.MailBody{MailItem: e.item()}
	for _, a := range e.To {
		body.To = append(body.To, a.String())
	}
	for _, a := range e.Cc {
		body.Cc = append(body.Cc, a.String())
	}
	var txt []string
	for _, p := range e.TextBody {
		txt = append(txt, e.BodyValues[p.PartID].Value)
	}
	body.Text = strings.TrimSpace(strings.Join(txt, "\n"))
	if looksHTML(body.Text) { // an HTML-only message: textBody then holds the HTML part
		body.Text, body.HTML = htmlToText(body.Text), true
	}
	if body.Text == "" {
		var hs []string
		for _, p := range e.HTMLBody {
			hs = append(hs, e.BodyValues[p.PartID].Value)
		}
		body.Text, body.HTML = htmlToText(strings.Join(hs, "\n")), true
	}
	if len(e.MessageID) > 0 {
		body.MsgID = e.MessageID[0]
	}
	// a formatted view exists when the message has an HTML part of its own (not just the text part standing in for it)
	var hs []string
	for _, p := range e.HTMLBody {
		hs = append(hs, e.BodyValues[p.PartID].Value)
	}
	if h := strings.Join(hs, "\n"); strings.TrimSpace(h) != "" && (body.HTML || looksHTML(h)) {
		body.Safe = SanitizeHTML(h)
	}
	return body, nil
}

func looksHTML(s string) bool {
	return len(reTag.FindAllString(s, 4)) >= 3 && strings.Contains(strings.ToLower(s), "</")
}

func firstEmail(l []addr) string {
	if len(l) == 0 {
		return ""
	}
	return l[0].Email
}
