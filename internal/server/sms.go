// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/store"
)

type storeSMS = store.SMS

// smsSync is the phone SMS service's single request (docs/sms.md): upload messages with the phone's own ids, report on outbound ones,
// get the phone's pending queue. The device is the authenticated connection, so no per-request signature is needed here. A phone may
// only touch its own messages and queue (the store functions are keyed by the device id).
func (s *session) smsSync(raw []byte) error {
	var in proto.SMSSync
	if err := proto.Decode(raw, &in); err != nil {
		return err
	}
	res := proto.SMSSyncRes{T: proto.TSMSRes, ID: in.ID}
	if len(in.Messages) > 500 || len(in.Acks) > 500 {
		res.Err = "at most 500 messages per sync"
		return s.c.send(res)
	}
	st := s.srv.Store
	batch := make([]storeSMS, 0, len(in.Messages))
	for _, m := range in.Messages {
		batch = append(batch, storeSMS{DevMsgID: m.ID, Number: m.Number, Dir: m.Dir, Body: m.Body, TS: m.TS, Status: m.Status})
	}
	n, err := st.SMSUpload(s.dev, batch)
	if err != nil {
		res.Err = "could not store the messages"
		return s.c.send(res)
	}
	res.Accepted = n
	for _, a := range in.Acks {
		st.SMSAck(s.dev, a.OutID, a.ID, a.Status, a.Error) // an unknown id is ignored: the phone must not stall on it
	}
	for _, m := range st.SMSPending(s.dev) {
		res.Pending = append(res.Pending, proto.SMSPending{OutID: m.ID, Number: m.Number, Body: m.Body})
	}
	return s.c.send(res)
}
