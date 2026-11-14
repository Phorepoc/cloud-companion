// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The fake homeserver's replies, through mautrix-go as the app uses it: a wrong password is refused, the right one signs in, and the
// encryption set-up (key upload and query) and a sync work.
package matrix

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

func TestFake(t *testing.T) {
	s := New()
	s.SyncWait = 10 * time.Millisecond
	hs := httptest.NewServer(s)
	defer hs.Close()
	ctx := context.Background()
	cli, _ := mautrix.NewClient(hs.URL, "", "")
	req := &mautrix.ReqLogin{Type: mautrix.AuthTypePassword, Identifier: mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: "cc"}, Password: "nope"}
	if _, err := cli.Login(ctx, req); !errors.Is(err, mautrix.MForbidden) {
		t.Fatalf("wrong password: %v", err)
	}
	req.Password, req.StoreCredentials, req.DeviceID = "pw-cc-123", true, "DEV1"
	resp, err := cli.Login(ctx, req)
	if err != nil || resp.UserID != "@cc:test.local" || resp.DeviceID != "DEV1" {
		t.Fatalf("login: %+v %v", resp, err)
	}
	if w, err := cli.Whoami(ctx); err != nil || w.UserID != "@cc:test.local" {
		t.Fatalf("whoami: %+v %v", w, err)
	}
	q := &mautrix.ReqQueryKeys{DeviceKeys: map[id.UserID]mautrix.DeviceIDList{cli.UserID: {cli.DeviceID}}}
	if r, err := cli.QueryKeys(ctx, q); err != nil || len(r.DeviceKeys[cli.UserID]) != 0 {
		t.Fatalf("no keys yet: %+v %v", r, err)
	}
	keys := &mautrix.ReqUploadKeys{DeviceKeys: &mautrix.DeviceKeys{UserID: cli.UserID, DeviceID: cli.DeviceID, Keys: map[id.DeviceKeyID]string{"ed25519:DEV1": "abc"}}}
	if _, err := cli.UploadKeys(ctx, keys); err != nil {
		t.Fatal(err)
	}
	if r, err := cli.QueryKeys(ctx, q); err != nil || r.DeviceKeys[cli.UserID][cli.DeviceID].Keys.GetEd25519(cli.DeviceID) != "abc" {
		t.Fatalf("keys: %+v %v", r, err)
	}
	if r, err := cli.SyncRequest(ctx, 1000, "s1", "", false, ""); err != nil || r.NextBatch != "s1" {
		t.Fatalf("sync: %+v %v", r, err)
	}
	if _, err := cli.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Whoami(ctx); !errors.Is(err, mautrix.MUnknownToken) {
		t.Fatalf("signed out: %v", err)
	}
}
