// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

// "phecloud hub ...": per-device app passwords for the personal hub, kept in Stalwart (AGPL, a separate container; this file only
// speaks its JMAP management API over HTTP, nothing of it is linked in). One instance serves one person: the owner account's
// own login is used, because Stalwart lets only the account itself create app passwords (an admin can list and revoke them).
//
//	phecloud hub status                      check the connection and show the owner account
//	phecloud hub apppw add <name> [days]     create a revocable password for one device (shown once); optional expiry in days
//	phecloud hub apppw list                  list them (secrets are never shown again)
//	phecloud hub apppw revoke <id>           revoke one: that device can no longer log in to mail, calendar, contacts or ActiveSync
//
// Environment: PHECLOUD_HUB_URL (http://stalwart:8080), PHECLOUD_HUB_TOKEN (an API key of the owner account, made by hub-init.sh; or
// PHECLOUD_HUB_OWNER + PHECLOUD_HUB_OWNER_PASSWORD for a Basic login), PHECLOUD_HUB_OWNER (owner@hub.lan, shown in the output),
// PHECLOUD_HUB_DOMAIN (host name shown in the phone settings).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

type hubClient struct{ url, user, pass, token string }

func hubFromEnv() (*hubClient, error) {
	h := &hubClient{url: env("PHECLOUD_HUB_URL", "http://stalwart:8080"), user: os.Getenv("PHECLOUD_HUB_OWNER"), pass: os.Getenv("PHECLOUD_HUB_OWNER_PASSWORD"), token: os.Getenv("PHECLOUD_HUB_TOKEN")}
	if h.token == "" && (h.user == "" || h.pass == "") {
		return nil, fmt.Errorf("PHECLOUD_HUB_TOKEN (or PHECLOUD_HUB_OWNER and PHECLOUD_HUB_OWNER_PASSWORD) is not set (run deploy/hub/hub-init.sh; docker compose reads deploy/hub/.env.phecloud)")
	}
	return h, nil
}

// call sends one JMAP request and returns the method responses.
func (h *hubClient) call(calls ...[]any) ([]json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{
		"using":       []string{"urn:ietf:params:jmap:core", "urn:stalwart:jmap"},
		"methodCalls": calls,
	})
	req, _ := http.NewRequest("POST", h.url+"/jmap", bytes.NewReader(body))
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	} else {
		req.SetBasicAuth(h.user, h.pass)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("hub: HTTP %d: %.200s", resp.StatusCode, b)
	}
	var r struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return r.MethodResponses, nil
}

// methodResult unpacks ["name", {args}, "id"] and fails on an "error" response.
func methodResult(raw json.RawMessage, into any) error {
	var t []json.RawMessage
	if err := json.Unmarshal(raw, &t); err != nil || len(t) < 2 {
		return fmt.Errorf("bad response %.200s", raw)
	}
	var name string
	json.Unmarshal(t[0], &name)
	if name == "error" {
		return fmt.Errorf("hub: %s", t[1])
	}
	return json.Unmarshal(t[1], into)
}

type appPassword struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"createdAt"`
	ExpiresAt   *string `json:"expiresAt"`
}

func hubMain(args []string) {
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	usage := func() {
		fmt.Fprintln(os.Stderr, "usage: phecloud hub status | apppw add <name> [days] | apppw list | apppw revoke <id>")
		os.Exit(2)
	}
	if len(args) == 0 {
		usage()
	}
	h, err := hubFromEnv()
	if err != nil {
		fail(err)
	}
	switch {
	case args[0] == "status":
		rs, err := h.call([]any{"x:AppPassword/query", map[string]any{}, "c1"})
		if err != nil {
			fail(err)
		}
		var q struct {
			AccountID string   `json:"accountId"`
			IDs       []string `json:"ids"`
		}
		if err := methodResult(rs[0], &q); err != nil {
			fail(err)
		}
		fmt.Printf("hub ok: %s (account %s), %d app password(s)\n", h.user, q.AccountID, len(q.IDs))
	case len(args) >= 3 && args[0] == "apppw" && args[1] == "add":
		create := map[string]any{"description": args[2]}
		if len(args) > 3 {
			days, err := strconv.Atoi(args[3])
			if err != nil || days < 1 {
				fail(fmt.Errorf("days must be a positive number"))
			}
			create["expiresAt"] = time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02T15:04:05Z")
		}
		rs, err := h.call([]any{"x:AppPassword/set", map[string]any{"create": map[string]any{"a": create}}, "c1"})
		if err != nil {
			fail(err)
		}
		var set struct {
			Created    map[string]struct{ ID, Secret string } `json:"created"`
			NotCreated map[string]json.RawMessage             `json:"notCreated"`
		}
		if err := methodResult(rs[0], &set); err != nil {
			fail(err)
		}
		if nc, bad := set.NotCreated["a"]; bad {
			fail(fmt.Errorf("not created: %s", nc))
		}
		c := set.Created["a"]
		dom := env("PHECLOUD_HUB_DOMAIN", "<your hub name>")
		fmt.Printf("app password %s for %q created. The secret is shown only now:\n\n", c.ID, args[2])
		fmt.Printf("  user      %s\n  password  %s\n  server    %s (ActiveSync, HTTPS)\n\n", h.user, c.Secret, dom)
	case len(args) >= 2 && args[0] == "apppw" && args[1] == "list":
		rs, err := h.call([]any{"x:AppPassword/get", map[string]any{"ids": nil}, "c1"})
		if err != nil {
			fail(err)
		}
		var g struct {
			List []appPassword `json:"list"`
		}
		if err := methodResult(rs[0], &g); err != nil {
			fail(err)
		}
		for _, p := range g.List {
			exp := "never"
			if p.ExpiresAt != nil {
				exp = *p.ExpiresAt
			}
			fmt.Printf("%s  %-24s created %s  expires %s\n", p.ID, p.Description, p.CreatedAt, exp)
		}
	case len(args) >= 3 && args[0] == "apppw" && args[1] == "revoke":
		rs, err := h.call([]any{"x:AppPassword/set", map[string]any{"destroy": []string{args[2]}}, "c1"})
		if err != nil {
			fail(err)
		}
		var set struct {
			Destroyed    []string                   `json:"destroyed"`
			NotDestroyed map[string]json.RawMessage `json:"notDestroyed"`
		}
		if err := methodResult(rs[0], &set); err != nil {
			fail(err)
		}
		if len(set.Destroyed) == 0 {
			fail(fmt.Errorf("not revoked: %s", set.NotDestroyed[args[2]]))
		}
		fmt.Println("revoked", args[2])
	default:
		usage()
	}
}
