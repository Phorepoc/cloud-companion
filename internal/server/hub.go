// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"
	"os"

	"github.com/phorepoc/phecloud/internal/proto"
)

// hubRequest answers one contacts/calendar request from the signed-in device. The owner of the hub is the person using the
// device, so any paired device may use it; the hub account's credentials never leave the server.
func (s *session) hubRequest(raw []byte) error {
	var h proto.Hub
	if err := proto.Decode(raw, &h); err != nil {
		return err
	}
	res := proto.HubRes{T: proto.THubRes, ID: h.ID}
	fail := func(err error) error {
		s.log.Warn("hub request failed", "op", h.Op, "err", err)
		res.Err = err.Error()
		return s.c.send(res)
	}
	hub := s.srv.Hub
	if hub == nil {
		return fail(errors.New("the hub is not set up on this server"))
	}
	var err error
	switch h.Op {
	case "contacts.list":
		res.Contacts, err = hub.Contacts()
	case "contacts.save":
		if h.Contact == nil {
			return fail(errors.New("no contact"))
		}
		var c proto.Contact
		if c, err = hub.SaveContact(*h.Contact); err == nil {
			res.Contact = &c
		}
	case "contacts.delete":
		err = hub.Delete("card", h.Href, h.ETag)
	case "events.list":
		res.Events, err = hub.Events(h.From, h.To)
	case "events.save":
		if h.Event == nil {
			return fail(errors.New("no event"))
		}
		var e proto.Event
		if e, err = hub.SaveEvent(*h.Event); err == nil {
			res.Event = &e
		}
	case "events.delete":
		err = hub.Delete("cal", h.Href, h.ETag)
	case "apppw.list", "apppw.add", "apppw.revoke":
		if !s.admin {
			return fail(errors.New("only an admin device can manage app passwords"))
		}
		res.Owner, res.Domain = hub.Owner, os.Getenv("PHECLOUD_HUB_DOMAIN")
		switch h.Op {
		case "apppw.list":
			res.AppPws, err = hub.AppPasswords()
		case "apppw.add":
			var id, secret string
			if id, secret, err = hub.AddAppPassword(h.Name, h.Days); err == nil {
				res.Secret = secret
				res.AppPws = []proto.AppPw{{ID: id, Name: h.Name}}
			}
		case "apppw.revoke":
			err = hub.RevokeAppPassword(h.ID2)
		}
	case "mail.folders":
		res.Folders, err = hub.MailFolders()
	case "mail.list":
		res.Mails, res.Total, err = hub.MailList(h.Folder, h.Pos)
	case "mail.read":
		res.Mail, err = hub.MailRead(h.ID2)
	case "mail.mark":
		err = hub.MailMark(h.ID2, h.Flag == "read")
	case "mail.move":
		err = hub.MailMove(h.ID2, h.Role)
	case "mail.delete":
		err = hub.MailDelete(h.ID2)
	case "mail.send":
		err = hub.MailSend(h.Rcpt, h.Subject, h.Body, h.HTMLBody, h.ReplyTo)
	default:
		err = errors.New("unknown hub operation")
	}
	if err != nil {
		return fail(err)
	}
	res.OK = true
	return s.c.send(res)
}
