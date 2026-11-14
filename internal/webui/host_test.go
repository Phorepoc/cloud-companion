// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/host"
	"github.com/phorepoc/cloud-companion/internal/store"
)

type okDocker struct{}

func (okDocker) Run(ctx context.Context, dir string, args []string, out io.Writer) error { return nil }

func TestHostAPI(t *testing.T) {
	st := store.OpenTest(t)
	sock := filepath.Join(t.TempDir(), "api.sock")
	mux := http.NewServeMux()
	newHostAPI(newAuth(st), sock).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(admin bool) *fakeDevice {
		id, key, err := st.NewDevice("d", admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	owner, phone := mk(true), mk(false)

	// a server set up by hand: no manager
	if code, j := owner.call(t, "GET", "/phe/host", nil); code != 200 || j["available"] != false {
		t.Fatalf("without a manager: %d %v", code, j)
	}
	if code, _ := owner.call(t, "GET", "/phe/host/features", nil); code != 503 {
		t.Fatalf("features without a manager: %d", code)
	}

	// a board: the manager on its socket
	cat, err := host.LoadCatalogue(os.DirFS("../../deploy/features"), ".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	m := &host.Manager{Cat: cat, Board: host.Board{Model: "Raspberry Pi 5", Arch: "arm64", Memory: 8 << 30}, ComposeDir: dir, StateDir: dir, Run: okDocker{}}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: m.Handler()}
	go hs.Serve(l)
	defer hs.Close()

	for _, path := range []string{"/phe/host", "/phe/host/features"} {
		if code, _ := phone.call(t, "GET", path, nil); code != 403 {
			t.Errorf("%s for a device that is not an admin: %d", path, code)
		}
	}
	if code, _ := phone.call(t, "POST", "/phe/host/features/signal", map[string]any{}); code != 403 {
		t.Errorf("a device that is not an admin switched a feature: %d", code)
	}
	code, j := owner.call(t, "GET", "/phe/host", nil)
	if sys, _ := j["system"].(map[string]any); code != 200 || j["available"] != true || sys["board"].(map[string]any)["model"] != "Raspberry Pi 5" {
		t.Fatalf("with a manager: %d %v", code, j)
	}
	code, j = owner.call(t, "POST", "/phe/host/features/signal", map[string]any{"settings": map[string]string{}})
	if code != 202 {
		t.Fatalf("switch Signal on: %d %v", code, j)
	}
	if code, j = owner.call(t, "GET", "/phe/host/jobs/"+j["id"].(string)+"?wait=1", nil); code != 200 || j["state"] != "done" {
		t.Fatalf("job: %d %v", code, j)
	}
	_, j = owner.call(t, "GET", "/phe/host/features", nil)
	on := false
	for _, f := range j["features"].([]any) {
		if f := f.(map[string]any); f["id"] == "signal" {
			on = f["on"].(bool)
		}
	}
	if !on {
		t.Error("Signal is not on")
	}
	if code, _ := owner.call(t, "DELETE", "/phe/host/features/core", nil); code != 409 {
		t.Errorf("core switched off: %d", code)
	}
	if code, _ := owner.call(t, "POST", "/phe/host/features/..%2Fsystem", map[string]any{}); code == 202 {
		t.Error("a bad id went through")
	}
}
