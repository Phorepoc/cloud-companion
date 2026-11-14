// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/chat"
	"github.com/phorepoc/cloud-companion/internal/store"
)

func TestRoutingAPI(t *testing.T) {
	st := store.OpenTest(t)
	mux := http.NewServeMux()
	(&chatAPI{a: newAuth(st), st: st, hub: chat.NewHub(st)}).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(name string, admin bool) *fakeDevice {
		id, key, err := st.NewDevice(name, admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	owner, phone := mk("web", true), mk("Phone", false)

	if code, _ := phone.call(t, "GET", "/phe/routing", nil); code != 403 {
		t.Fatalf("a phone read the routing: %d", code)
	}
	if code, _ := phone.call(t, "POST", "/phe/sip/logins", map[string]any{"name": "n9"}); code != 403 {
		t.Fatalf("a phone made a SIP login: %d", code)
	}
	code, r := owner.call(t, "POST", "/phe/sip/logins", map[string]any{"name": "n9"})
	if code != 200 || r["password"] == "" || r["realm"] != store.SIPRealm {
		t.Fatalf("make a login: %d %v", code, r)
	}
	owner.call(t, "POST", "/phe/sip/logins", map[string]any{"name": "e7"})

	code, r = owner.call(t, "GET", "/phe/routing", nil)
	conns, _ := r["connections"].([]any)
	if code != 200 || len(conns) != 2 || conns[0].(map[string]any)["id"] != "sms" || conns[1].(map[string]any)["id"] != "sip" {
		t.Fatalf("connections: %d %v", code, r)
	}
	if ls, _ := r["logins"].([]any); len(ls) != 2 {
		t.Fatalf("logins: %v", r["logins"])
	}

	put := map[string]any{
		"routes":  []map[string]any{{"kind": "prefix", "value": "home.", "conn": "sip"}, {"kind": "number", "conn": "sms"}},
		"deliver": map[string][]string{"sms": {"n9"}},
	}
	if code, r := owner.call(t, "PUT", "/phe/routing", put); code != 200 {
		t.Fatalf("save: %d %v", code, r)
	}
	if rs := st.ChatRoutes(); len(rs) != 2 || rs[0].Value != "home." || rs[1].Kind != "number" {
		t.Fatalf("stored rules: %+v", rs)
	}
	if d := st.ChatDeliver(); len(d["sms"]) != 1 || d["sms"][0] != "n9" {
		t.Fatalf("stored delivery: %v", d)
	}
	for _, bad := range []map[string]any{
		{"routes": []map[string]any{{"kind": "prefix", "value": "tg.", "conn": "telegram"}}}, // not set up
		{"routes": []map[string]any{{"kind": "prefix", "value": "", "conn": "sms"}}},         // no prefix
		{"routes": []map[string]any{}, "deliver": map[string][]string{"sms": {"nobody"}}},    // unknown login
	} {
		if code, _ := owner.call(t, "PUT", "/phe/routing", bad); code != 400 {
			t.Fatalf("accepted %v: %d", bad, code)
		}
	}
	if code, _ := owner.call(t, "POST", "/phe/sip/logins/remove", map[string]any{"name": "e7"}); code != 200 {
		t.Fatal("remove a login")
	}
	if code, _ := owner.call(t, "POST", "/phe/sip/logins/remove", map[string]any{"name": "e7"}); code != 404 {
		t.Fatal("removed a login twice")
	}
}
