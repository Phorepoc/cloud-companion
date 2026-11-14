// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/proto"
)

type closeIO struct {
	fakeIO
	closed bool
}

func (c *closeIO) Close() error { c.closed = true; return nil }

// A revoked device is disconnected at once, told why; the other devices stay connected.
func TestKickRevokedDevice(t *testing.T) {
	s := &Server{}
	lost, kept := &closeIO{}, &closeIO{}
	s.register(&session{dev: "lost", c: &conn{io: lost}})
	s.register(&session{dev: "kept", c: &conn{io: kept}})
	s.kick("lost")
	if !lost.closed || kept.closed {
		t.Fatalf("closed: lost %v, kept %v", lost.closed, kept.closed)
	}
	if e, ok := lost.sent[0].(proto.Error); !ok || e.Msg != proto.ErrRevoked {
		t.Errorf("not told why: %+v", lost.sent)
	}
}

// A device that is not an admin sees and drives only its own windows; an admin device reaches every device.
func TestOtherDevicesNeedAdmin(t *testing.T) {
	s := &Server{}
	owner := &session{srv: s, dev: "owner", admin: true, c: &conn{io: &closeIO{}}, ops: make(chan func(ctx context.Context) error, 4)}
	guest := &session{srv: s, dev: "guest", c: &conn{io: &closeIO{}}, ops: make(chan func(ctx context.Context) error, 4)}
	guest2 := &session{srv: s, dev: "guest", c: &conn{io: &closeIO{}}, ops: make(chan func(ctx context.Context) error, 4)}
	for _, x := range []*session{owner, guest, guest2} {
		s.register(x)
	}
	devs := func(l []proto.SessionInfo) (out []string) {
		for _, i := range l {
			out = append(out, i.Dev)
		}
		return out
	}
	if got := devs(s.sessionList(guest)); len(got) != 2 || got[0] != "guest" || got[1] != "guest" {
		t.Errorf("the guest sees %v", got)
	}
	if got := devs(s.sessionList(owner)); len(got) != 3 {
		t.Errorf("the owner sees %v", got)
	}
	open := func(from, to *session) error {
		raw, _ := proto.Marshal(proto.Remote{T: proto.TRemote, SID: to.sid, Action: "open", URL: "https://example.com"})
		return from.remote(raw)
	}
	if err := open(guest, owner); err == nil {
		t.Error("the guest opened a page in the owner's browser")
	}
	if err := open(guest, guest2); err != nil {
		t.Errorf("the guest could not reach its own other window: %v", err)
	}
	if err := open(owner, guest); err != nil {
		t.Errorf("the owner could not reach the guest: %v", err)
	}
}
