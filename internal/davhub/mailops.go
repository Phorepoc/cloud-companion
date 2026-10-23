// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package davhub

import (
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

const capSubmit = "urn:ietf:params:jmap:submission"

// safePolicy: formatting and structure only. No images, no styles, no scripts, no forms, no frames, no remote loads of any kind;
// links stay as links (nothing is fetched until the person follows one) and open in a new context without a referrer.
var safePolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "div", "span", "b", "i", "u", "strong", "em", "ul", "ol", "li", "blockquote", "pre", "code", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6", "table", "thead", "tbody", "tr", "td", "th", "sub", "sup", "small", "s")
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

// SanitizeHTML returns a safe fragment of an HTML message body.
func SanitizeHTML(s string) string { return strings.TrimSpace(safePolicy.Sanitize(s)) }

// mailboxByRole finds the id of the folder with the given role.
func (c *Client) mailboxByRole(acc, role string) (string, error) {
	rs, err := c.call([]string{capCore, capMail}, []any{"Mailbox/get", map[string]any{"accountId": acc, "properties": []string{"id", "role"}}, "m1"})
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct{ ID, Role string } `json:"list"`
	}
	if err := result(rs[0], &g); err != nil {
		return "", err
	}
	for _, m := range g.List {
		if m.Role == role {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("this hub has no %s folder", role)
}

func (c *Client) emailSet(acc string, set map[string]any) error {
	set["accountId"] = acc
	rs, err := c.call([]string{capCore, capMail}, []any{"Email/set", set, "s1"})
	if err != nil {
		return err
	}
	var r struct {
		NotUpdated   map[string]struct{ Type, Description string } `json:"notUpdated"`
		NotDestroyed map[string]struct{ Type, Description string } `json:"notDestroyed"`
	}
	if err := result(rs[0], &r); err != nil {
		return err
	}
	for _, e := range r.NotUpdated {
		return fmt.Errorf("the hub did not accept that: %s", e.Type)
	}
	for _, e := range r.NotDestroyed {
		return fmt.Errorf("the hub did not accept that: %s", e.Type)
	}
	return nil
}

// MailMark marks a message read or unread.
func (c *Client) MailMark(id string, read bool) error {
	acc, err := c.mailAccount()
	if err != nil {
		return err
	}
	var v any = true
	if !read {
		v = nil // patch: remove the keyword
	}
	return c.emailSet(acc, map[string]any{"update": map[string]any{id: map[string]any{"keywords/$seen": v}}})
}

// MailMove moves a message to the folder with the given role (inbox, archive, junk, trash).
func (c *Client) MailMove(id, role string) error {
	acc, err := c.mailAccount()
	if err != nil {
		return err
	}
	to, err := c.mailboxByRole(acc, role)
	if err != nil {
		return err
	}
	return c.emailSet(acc, map[string]any{"update": map[string]any{id: map[string]any{"mailboxIds": map[string]bool{to: true}}}})
}

// MailDelete moves a message to Trash; a message that is already in Trash is destroyed for good.
func (c *Client) MailDelete(id string) error {
	acc, err := c.mailAccount()
	if err != nil {
		return err
	}
	trash, err := c.mailboxByRole(acc, "trash")
	if err != nil {
		return err
	}
	rs, err := c.call([]string{capCore, capMail}, []any{"Email/get", map[string]any{"accountId": acc, "ids": []string{id}, "properties": []string{"mailboxIds"}}, "g1"})
	if err != nil {
		return err
	}
	var g struct {
		List []struct {
			MailboxIDs map[string]bool `json:"mailboxIds"`
		} `json:"list"`
	}
	if err := result(rs[0], &g); err != nil {
		return err
	}
	if len(g.List) == 0 {
		return errors.New("that message is not there any more")
	}
	if g.List[0].MailboxIDs[trash] {
		return c.emailSet(acc, map[string]any{"destroy": []string{id}})
	}
	return c.emailSet(acc, map[string]any{"update": map[string]any{id: map[string]any{"mailboxIds": map[string]bool{trash: true}}}})
}

func (c *Client) domain() string {
	if i := strings.LastIndex(c.Owner, "@"); i >= 0 {
		return strings.ToLower(c.Owner[i+1:])
	}
	return ""
}

// checkRecipients: sending outside the hub's own domain is off unless PHECLOUD_HUB_ALLOW_EXTERNAL=1 (no outside sending in tests).
func (c *Client) checkRecipients(rcpt string) ([]string, error) {
	list, err := mail.ParseAddressList(strings.ReplaceAll(rcpt, ";", ","))
	if err != nil || len(list) == 0 {
		return nil, errors.New("enter at least one valid email address")
	}
	var out []string
	for _, a := range list {
		if os.Getenv("PHECLOUD_HUB_ALLOW_EXTERNAL") != "1" && !strings.HasSuffix(strings.ToLower(a.Address), "@"+c.domain()) {
			return nil, fmt.Errorf("sending to %s is switched off on this server: it only delivers to addresses at %s", a.Address, c.domain())
		}
		out = append(out, a.Address)
	}
	return out, nil
}

// MailSend writes a plain-text message and submits it (EmailSubmission). The copy is filed in Sent.
func (c *Client) MailSend(rcpt, subject, body, inReplyTo string) error {
	to, err := c.checkRecipients(rcpt)
	if err != nil {
		return err
	}
	acc, err := c.mailAccount()
	if err != nil {
		return err
	}
	drafts, err := c.mailboxByRole(acc, "drafts")
	if err != nil {
		return err
	}
	sent, err := c.mailboxByRole(acc, "sent")
	if err != nil {
		return err
	}
	rs, err := c.call([]string{capCore, capSubmit}, []any{"Identity/get", map[string]any{"accountId": acc}, "i1"})
	if err != nil {
		return err
	}
	var idn struct {
		List []struct{ ID, Name, Email string } `json:"list"`
	}
	if err := result(rs[0], &idn); err != nil {
		return err
	}
	if len(idn.List) == 0 {
		return errors.New("this hub account has no sender identity")
	}
	me := idn.List[0]
	var tos []map[string]string
	for _, a := range to {
		tos = append(tos, map[string]string{"email": a})
	}
	draft := map[string]any{
		"mailboxIds": map[string]bool{drafts: true}, "keywords": map[string]bool{"$draft": true},
		"from": []map[string]string{{"name": me.Name, "email": me.Email}}, "to": tos, "subject": subject,
		"bodyStructure": map[string]any{"partId": "1", "type": "text/plain"}, "bodyValues": map[string]any{"1": map[string]any{"value": body}},
	}
	if inReplyTo != "" {
		draft["inReplyTo"] = []string{strings.Trim(inReplyTo, "<>")}
		draft["references"] = []string{strings.Trim(inReplyTo, "<>")}
	}
	rs, err = c.call([]string{capCore, capMail, capSubmit},
		[]any{"Email/set", map[string]any{"accountId": acc, "create": map[string]any{"d": draft}}, "e1"},
		[]any{"EmailSubmission/set", map[string]any{"accountId": acc, "create": map[string]any{"s": map[string]any{"identityId": me.ID, "emailId": "#d"}},
			"onSuccessUpdateEmail": map[string]any{"#s": map[string]any{"keywords/$draft": nil, "mailboxIds": map[string]bool{sent: true}}}}, "e2"})
	if err != nil {
		return err
	}
	var e1 struct {
		NotCreated map[string]struct{ Type, Description string } `json:"notCreated"`
	}
	if err := result(rs[0], &e1); err != nil {
		return err
	}
	for _, e := range e1.NotCreated {
		return fmt.Errorf("the message could not be saved: %s %s", e.Type, e.Description)
	}
	var e2 struct {
		NotCreated map[string]struct{ Type, Description string } `json:"notCreated"`
	}
	if err := result(rs[1], &e2); err != nil {
		return err
	}
	for _, e := range e2.NotCreated {
		return fmt.Errorf("the message could not be sent: %s %s", e.Type, e.Description)
	}
	return nil
}
