// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package davhub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// What the connectors need from the hub: Stalwart is only the local cache, so mail from outside accounts is IMPORTED (never delivered
// through SMTP), and contacts / events are written as they are.

func (c *Client) auth(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else {
		req.SetBasicAuth(c.Owner, c.Pass)
	}
}

// MailFolderID returns the id of the hub folder called name, creating it when missing.
func (c *Client) MailFolderID(name string) (string, error) {
	acc, err := c.mailAccount()
	if err != nil {
		return "", err
	}
	rs, err := c.call([]string{capCore, capMail}, []any{"Mailbox/get", map[string]any{"accountId": acc, "properties": []string{"id", "name", "parentId"}}, "g"})
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct {
			ID, Name string
			ParentID *string `json:"parentId"`
		} `json:"list"`
	}
	if err := result(rs[0], &g); err != nil {
		return "", err
	}
	for _, m := range g.List {
		if m.Name == name && m.ParentID == nil {
			return m.ID, nil
		}
	}
	rs, err = c.call([]string{capCore, capMail}, []any{"Mailbox/set", map[string]any{"accountId": acc, "create": map[string]any{"n": map[string]any{"name": name}}}, "s"})
	if err != nil {
		return "", err
	}
	var s struct {
		Created map[string]struct{ ID string } `json:"created"`
	}
	if err := result(rs[0], &s); err != nil || s.Created["n"].ID == "" {
		return "", fmt.Errorf("could not create the folder %q", name)
	}
	return s.Created["n"].ID, nil
}

// MailImport stores a complete RFC 5322 message in the hub folder (a JMAP blob upload and Email/import); it returns the hub email id.
func (c *Client) MailImport(folderID string, raw []byte, seen bool, received time.Time) (string, error) {
	acc, err := c.mailAccount()
	if err != nil {
		return "", err
	}
	// the upload endpoint (relative to this hub URL: the hub may know itself by another name)
	req, _ := http.NewRequest("POST", c.URL+"/jmap/upload/"+url.PathEscape(acc)+"/", bytes.NewReader(raw))
	c.auth(req)
	req.Header.Set("Content-Type", "message/rfc822")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("hub: upload HTTP %d", resp.StatusCode)
	}
	var up struct {
		BlobID string `json:"blobId"`
	}
	if json.Unmarshal(b, &up) != nil || up.BlobID == "" {
		return "", errors.New("hub: no blob id")
	}
	kw := map[string]bool{}
	if seen {
		kw["$seen"] = true
	}
	em := map[string]any{"blobId": up.BlobID, "mailboxIds": map[string]bool{folderID: true}, "keywords": kw}
	if !received.IsZero() {
		em["receivedAt"] = received.UTC().Format(time.RFC3339)
	}
	rs, err := c.call([]string{capCore, capMail}, []any{"Email/import", map[string]any{"accountId": acc, "emails": map[string]any{"i": em}}, "i"})
	if err != nil {
		return "", err
	}
	var r struct {
		Created    map[string]struct{ ID string } `json:"created"`
		NotCreated map[string]struct {
			Type, Description string
		} `json:"notCreated"`
	}
	if err := result(rs[0], &r); err != nil {
		return "", err
	}
	if nc, ok := r.NotCreated["i"]; ok {
		return "", fmt.Errorf("hub: import refused: %s %s", nc.Type, nc.Description)
	}
	return r.Created["i"].ID, nil
}

// PutRaw writes a vCard or iCalendar resource into the hub collection (kind card|cal) under name (for example "<uid>.vcf").
func (c *Client) PutRaw(kind, name, body string) error {
	ct := "text/vcard; charset=utf-8"
	if kind == "cal" {
		ct = "text/calendar; charset=utf-8"
	}
	code, _, _, err := c.do("PUT", c.base(kind)+url.PathEscape(name), body, map[string]string{"Content-Type": ct})
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fmt.Errorf("hub: PUT %s -> HTTP %d", name, code)
	}
	return nil
}

// DeleteRaw removes a resource of the hub collection by name.
func (c *Client) DeleteRaw(kind, name string) error {
	code, _, _, err := c.do("DELETE", c.base(kind)+url.PathEscape(name), "", nil)
	if err != nil {
		return err
	}
	if code/100 != 2 && code != 404 {
		return fmt.Errorf("hub: DELETE %s -> HTTP %d", name, code)
	}
	return nil
}

var _ = strings.TrimSpace
