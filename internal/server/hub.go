// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"

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
	default:
		err = errors.New("unknown hub operation")
	}
	if err != nil {
		return fail(err)
	}
	res.OK = true
	return s.c.send(res)
}
