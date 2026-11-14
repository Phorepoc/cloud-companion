// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// The first-owner code opens the passkey registration of an uninitialised server, and only that.
func TestFirstOwnerCodeBeginsRegistration(t *testing.T) {
	st := store.OpenTest(t)
	code, err := st.NewFirstCode()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	newAuth(st).routes(mux)
	var last string
	post := func(c string) int {
		r := httptest.NewRequest("POST", "http://localhost:8080/passkey/register/begin", strings.NewReader(`{"code":"`+c+`"}`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		last = w.Body.String()
		return w.Code
	}
	if got := post("AAAA-BBBB-CCCC"); got != 403 {
		t.Fatalf("wrong code: %d", got)
	}
	if got := post(strings.ToLower(code)); got != 200 {
		t.Fatalf("first-owner code: %d %s", got, last)
	}
	if err := st.ConsumeFirstCode(code); err != nil {
		t.Fatal(err)
	}
	if got := post(code); got != 403 {
		t.Fatalf("used code: %d", got)
	}
}
