// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

// App installation from the page:
//
//	POST /apk      body = the APK (or an .apk file's bytes), header X-Session
//	POST /fdroid?id=<package>   downloads the suggested build of that package from f-droid.org and installs it, header X-Session
//
// Both need the session id the WebSocket handed out (so only the signed-in viewer can install), run `adb install -r -g`, and answer {ok, detail}.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const maxAPK = 512 << 20

var pkgRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

func jsonReply(w http.ResponseWriter, code int, ok bool, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": ok, "detail": detail})
}

func installFile(path string) (bool, string) {
	out, err := exec.Command("adb", "-s", serial, "install", "-r", "-g", path).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	return err == nil && strings.Contains(s, "Success"), trunc(s, 300)
}

func handleAPK(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !sessionOK(r.Header.Get("X-Session")) {
		jsonReply(w, 403, false, "not allowed")
		return
	}
	f, err := os.CreateTemp("", "up-*.apk")
	if err != nil {
		jsonReply(w, 500, false, err.Error())
		return
	}
	defer os.Remove(f.Name())
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, maxAPK))
	f.Close()
	if err != nil || n < 1000 {
		jsonReply(w, 400, false, "the upload failed or is not an APK")
		return
	}
	ok, d := installFile(f.Name())
	jsonReply(w, 200, ok, d)
}

func handleFDroid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !sessionOK(r.Header.Get("X-Session")) {
		jsonReply(w, 403, false, "not allowed")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !pkgRe.MatchString(id) || len(id) > 150 {
		jsonReply(w, 400, false, "that is not an Android package name (for example org.fossify.notes)")
		return
	}
	ok, d := fdroidInstall(r.Context(), id)
	code := 200
	if !ok && strings.HasPrefix(d, "F-Droid has no app") {
		code = 404
	}
	jsonReply(w, code, ok, d)
}

// fdroidInstall downloads the suggested build of a package from f-droid.org (the multi-ABI file) and installs it with adb.
func fdroidInstall(ctx context.Context, id string) (bool, string) {
	hc := &http.Client{Timeout: 5 * time.Minute}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://f-droid.org/api/v1/packages/"+id, nil)
	resp, err := hc.Do(req)
	if err != nil {
		return false, "f-droid.org is not reachable"
	}
	var meta struct {
		Suggested int `json:"suggestedVersionCode"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&meta)
	resp.Body.Close()
	if resp.StatusCode == 404 || (err == nil && meta.Suggested == 0) {
		return false, "F-Droid has no app with that package name"
	}
	if err != nil || resp.StatusCode != 200 {
		return false, "F-Droid did not answer"
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://f-droid.org/repo/%s_%d.apk", id, meta.Suggested), nil)
	resp, err = hc.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return false, "the download failed"
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp("", "fd-*.apk")
	if err != nil {
		return false, err.Error()
	}
	defer os.Remove(f.Name())
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxAPK)); err != nil {
		f.Close()
		return false, "the download failed"
	}
	f.Close()
	return installFile(f.Name())
}
