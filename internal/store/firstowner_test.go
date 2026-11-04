// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

var codeRe = regexp.MustCompile(`\b([2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4})\b`)

func start(t *testing.T, st *Store, env string) (printed string, logs string) {
	t.Helper()
	var out, lg bytes.Buffer
	l := slog.New(slog.NewTextHandler(&lg, nil))
	if err := FirstStart(st, env, "http://127.0.0.1:8080", &out, l); err != nil {
		t.Fatal(err)
	}
	return out.String(), lg.String()
}

func firstCode(t *testing.T, out string) string {
	t.Helper()
	m := codeRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no code printed: %q", out)
	}
	return m[1]
}

func TestFirstStartPrintsCodeOnceUsable(t *testing.T) {
	st := OpenTest(t)
	out, _ := start(t, st, "")
	if !strings.Contains(out, "http://127.0.0.1:8080") || !strings.Contains(out, "FIRST START") {
		t.Fatalf("banner: %q", out)
	}
	code := firstCode(t, out)
	if !st.CheckFirstCode(code) || !st.CheckFirstCode(strings.ToLower(strings.ReplaceAll(code, "-", " "))) {
		t.Fatal("the printed code must check out (case, dashes, spaces do not matter)")
	}
	if st.CheckFirstCode("AAAA-BBBB-CCCC") {
		t.Fatal("a wrong code passed")
	}
	if err := st.ConsumeFirstCode(code); err != nil {
		t.Fatal(err)
	}
	if st.ConsumeFirstCode(code) == nil || st.CheckFirstCode(code) {
		t.Fatal("the code works once")
	}
}

func TestFirstCodeRestartRotates(t *testing.T) {
	st := OpenTest(t)
	out1, _ := start(t, st, "")
	c1 := firstCode(t, out1)
	out2, _ := start(t, st, "")
	c2 := firstCode(t, out2)
	if c1 == c2 {
		t.Fatal("a restart must make a new code")
	}
	if st.CheckFirstCode(c1) || !st.CheckFirstCode(c2) {
		t.Fatal("only the newest code works")
	}
}

func TestFirstCodeParallelConsume(t *testing.T) {
	st := OpenTest(t)
	out, _ := start(t, st, "")
	code := firstCode(t, out)
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if st.ConsumeFirstCode(code) == nil {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d parallel consumers won, want 1", wins)
	}
}

func TestFirstCodeStoredHashed(t *testing.T) {
	st := OpenTest(t)
	out, _ := start(t, st, "")
	code := firstCode(t, out)
	var salt, hash string
	if err := st.db.QueryRow(`SELECT salt,hash FROM first_owner_code WHERE id=1`).Scan(&salt, &hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, strings.ReplaceAll(code, "-", "")) || len(hash) != 64 {
		t.Fatal("the code must be stored as a hash")
	}
}

func TestFirstCodePairsFirstDeviceAsAdmin(t *testing.T) {
	st := OpenTest(t)
	out, _ := start(t, st, "")
	code := firstCode(t, out)
	if _, _, admin, err := st.Pair("AAAA-BBBB-CCCC", "x"); err == nil || admin {
		t.Fatal("wrong code paired")
	}
	_, _, admin, err := st.Pair(code, "phone")
	if err != nil || !admin {
		t.Fatalf("first device with the first code: admin=%v err=%v", admin, err)
	}
	if _, _, _, err := st.Pair(code, "again"); err == nil {
		t.Fatal("the code works once")
	}
	if st.Uninitialised() {
		t.Fatal("an admin device initialises the server")
	}
}

func TestFirstCodeDeadAfterOwner(t *testing.T) {
	st := OpenTest(t)
	out, _ := start(t, st, "")
	code := firstCode(t, out)
	if _, err := st.AddPasskey(NewHandle(), &webauthn.Credential{ID: []byte("c1")}, "k"); err != nil {
		t.Fatal(err)
	}
	if st.CheckFirstCode(code) || st.ConsumeFirstCode(code) == nil {
		t.Fatal("the first-owner code must be dead once an owner exists, even if it was never used")
	}
	if _, _, _, err := st.Pair(code, "late"); err == nil {
		t.Fatal("pairing with a dead first-owner code")
	}
	out2, logs := start(t, st, "")
	if out2 != "" || codeRe.MatchString(logs) {
		t.Fatalf("no code may be printed after init: %q %q", out2, logs)
	}
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM first_owner_code`).Scan(&n)
	if n != 0 {
		t.Fatal("the stored code must be removed")
	}
}

func TestFirstCodeEnv(t *testing.T) {
	const env = "my-Own_Secret-42xz"
	st := OpenTest(t)
	out, logs := start(t, st, env)
	if out != "" || strings.Contains(logs, env) || strings.Contains(logs, "42xz") || !strings.Contains(logs, "PHECLOUD_FIRST_OWNER_CODE is active") {
		t.Fatalf("env code must be active and never printed: %q / %q", out, logs)
	}
	if st.CheckFirstCode("wrong-wrong-wrong") || !st.CheckFirstCode(strings.ToUpper(env)) {
		t.Fatal("env code check")
	}
	if _, _, admin, err := st.Pair(env, "p"); err != nil || !admin {
		t.Fatalf("pair with env code: %v %v", admin, err)
	}
	if st.CheckFirstCode(env) {
		t.Fatal("env code works once")
	}
}

func TestFirstCodeEnvWeakFallsBack(t *testing.T) {
	for _, weak := range []string{"short", "aaaaaaaaaaaaaaaa", "has a bad char!!!!", "1234567890"} {
		st := OpenTest(t)
		out, logs := start(t, st, weak)
		if !strings.Contains(logs, "too weak") || strings.Contains(logs, weak) {
			t.Fatalf("%q: weak code must be rejected without echoing it: %q", weak, logs)
		}
		if st.CheckFirstCode(weak) {
			t.Fatalf("%q: weak code accepted", weak)
		}
		if !st.CheckFirstCode(firstCode(t, out)) {
			t.Fatal("generated fallback must work")
		}
	}
}

func TestFirstCodeEnvIgnoredAfterInit(t *testing.T) {
	const env = "my-Own_Secret-42xz"
	st := OpenTest(t)
	if _, err := st.AddPasskey(NewHandle(), &webauthn.Credential{ID: []byte("c1")}, "k"); err != nil {
		t.Fatal(err)
	}
	out, logs := start(t, st, env)
	if out != "" || strings.Contains(logs, env) || !strings.Contains(logs, "ignored") {
		t.Fatalf("ignored env: %q / %q", out, logs)
	}
	if st.CheckFirstCode(env) || st.ConsumeFirstCode(env) == nil {
		t.Fatal("env code must not work after init")
	}
}
