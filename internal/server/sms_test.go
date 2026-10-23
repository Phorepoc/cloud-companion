// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/phorepoc/phecloud/internal/proto"
	"github.com/phorepoc/phecloud/internal/store"
)

func TestSMSFrame(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sync := func(dev string, in proto.SMSSync) proto.SMSSyncRes {
		fio := &fakeIO{}
		s := &session{srv: &Server{Store: st}, c: &conn{io: fio}, log: slog.Default(), dev: dev}
		in.T, in.ID = proto.TSMS, 3
		raw, _ := proto.Marshal(in)
		if err := s.smsSync(raw); err != nil {
			t.Fatal(err)
		}
		return fio.sent[len(fio.sent)-1].(proto.SMSSyncRes)
	}
	msgs := []proto.SMSMsg{{ID: "1048583", Number: "+358407654321", Dir: "in", Body: "Hello from phorepoc", TS: 1760000000}, {ID: "1048590", Number: "0407654321", Dir: "out", Body: "Hi", TS: 1760000100, Status: "sent"}}
	if r := sync("phoneA", proto.SMSSync{Messages: msgs}); r.Accepted != 2 || r.Err != "" {
		t.Fatalf("first: %+v", r)
	}
	if r := sync("phoneA", proto.SMSSync{Messages: msgs}); r.Accepted != 0 {
		t.Fatalf("the message server ids make a retry harmless: %+v", r)
	}
	id, err := st.SMSQueue("phoneA", "+358407654321", "reply from the web")
	if err != nil {
		t.Fatal(err)
	}
	r := sync("phoneA", proto.SMSSync{})
	if len(r.Pending) != 1 || r.Pending[0].OutID != id || r.Pending[0].Body != "reply from the web" {
		t.Fatalf("pending: %+v", r)
	}
	if r := sync("phoneB", proto.SMSSync{}); len(r.Pending) != 0 {
		t.Fatalf("another phone sees the queue: %+v", r)
	}
	r = sync("phoneA", proto.SMSSync{Acks: []proto.SMSAck{{OutID: id, ID: "1048600", Status: "sent"}}})
	if len(r.Pending) != 0 {
		t.Fatalf("an acked message leaves the queue: %+v", r)
	}
}

// The phone uploads its own copy of a message (its Sent folder) BEFORE the ack for the queued message arrives: one message must remain.
func TestSMSAckAfterOwnCopy(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id, _ := st.SMSQueue("p", "+358410121112", "ReplyFromTheWeb")
	st.SMSUpload("p", []store.SMS{{DevMsgID: "269484039", Number: "+358410121112", Dir: "out", Body: "ReplyFromTheWeb", TS: 1, Status: "sent"}})
	if err := st.SMSAck("p", id, "269484039", "sent", ""); err != nil {
		t.Fatal(err)
	}
	if len(st.SMSPending("p")) != 0 {
		t.Fatal("the message must leave the queue")
	}
	th := st.SMSThread(store.NumberKey("+358410121112"), 10)
	if len(th) != 1 || th[0].Status != "sent" {
		t.Fatalf("one message, sent: %+v", th)
	}
}
