// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/phorepoc/phecloud/internal/proto"
)

// registry of the live sessions (connected devices): what the web app's "other devices" list shows, and how it reaches a tab of
// another device. A session's tabs live in that session only; the list is a snapshot the session publishes whenever its tab list
// changes, and an action on another device runs inside that device's own request loop (its ops channel), never from outside.
type registry struct {
	mu   sync.Mutex
	next int
	live map[int]*session
}

func (s *Server) register(ses *session) {
	s.reg.mu.Lock()
	defer s.reg.mu.Unlock()
	if s.reg.live == nil {
		s.reg.live = map[int]*session{}
	}
	s.reg.next++
	ses.sid = s.reg.next
	s.reg.live[ses.sid] = ses
}

func (s *Server) unregister(ses *session) {
	s.reg.mu.Lock()
	delete(s.reg.live, ses.sid)
	s.reg.mu.Unlock()
}

func (s *Server) sessionList(self *session) []proto.SessionInfo {
	s.reg.mu.Lock()
	defer s.reg.mu.Unlock()
	var out []proto.SessionInfo
	for sid, ses := range s.reg.live {
		ses.pubMu.Lock()
		tabs := append([]proto.TabInfo(nil), ses.pub...)
		ses.pubMu.Unlock()
		name := ses.devName
		if name == "" {
			name = ses.dev
		}
		out = append(out, proto.SessionInfo{SID: sid, Dev: ses.dev, Name: name, Self: ses == self, Tabs: tabs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SID < out[j].SID })
	return out
}

// remote runs an action on another session's tabs: close a view, or open a new tab with a URL.
func (s *session) remote(raw []byte) error {
	var r proto.Remote
	if err := proto.Decode(raw, &r); err != nil {
		return err
	}
	s.srv.reg.mu.Lock()
	target := s.srv.reg.live[r.SID]
	s.srv.reg.mu.Unlock()
	if target == nil {
		return errors.New("that device is no longer connected")
	}
	var op func(ctx context.Context) error
	switch r.Action {
	case "close":
		tr, _ := proto.Marshal(proto.Tab{T: proto.TTab, Action: "close", V: r.V})
		op = func(ctx context.Context) error { return target.tabAction(ctx, tr) }
	case "open":
		if r.URL == "" {
			return errors.New("enter a web address")
		}
		url := r.URL
		op = func(ctx context.Context) error {
			nt, _ := proto.Marshal(proto.Tab{T: proto.TTab, Action: "new"})
			if err := target.tabAction(ctx, nt); err != nil {
				return err
			}
			or, _ := proto.Marshal(proto.Open{T: proto.TOpen, V: target.active, URL: url, Width: 360})
			return target.dispatch(ctx, proto.TOpen, or)
		}
	default:
		return errors.New("unknown action")
	}
	select {
	case target.ops <- op:
		return nil
	default:
		return errors.New("that device is busy; try again")
	}
}
