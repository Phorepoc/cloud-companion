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
	hc := &http.Client{Timeout: 5 * time.Minute}
	resp, err := hc.Get("https://f-droid.org/api/v1/packages/" + id)
	if err != nil {
		jsonReply(w, 502, false, "f-droid.org is not reachable")
		return
	}
	var meta struct {
		Suggested int `json:"suggestedVersionCode"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&meta)
	resp.Body.Close()
	if resp.StatusCode == 404 || (err == nil && meta.Suggested == 0) {
		jsonReply(w, 404, false, "F-Droid has no app with that package name")
		return
	}
	if err != nil || resp.StatusCode != 200 {
		jsonReply(w, 502, false, "F-Droid did not answer")
		return
	}
	// the multi-ABI build is the file without an ABI suffix; the suggested versionCode names it
	resp, err = hc.Get(fmt.Sprintf("https://f-droid.org/repo/%s_%d.apk", id, meta.Suggested))
	if err != nil || resp.StatusCode != 200 {
		jsonReply(w, 502, false, "the download failed")
		return
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp("", "fd-*.apk")
	if err != nil {
		jsonReply(w, 500, false, err.Error())
		return
	}
	defer os.Remove(f.Name())
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxAPK)); err != nil {
		f.Close()
		jsonReply(w, 502, false, "the download failed")
		return
	}
	f.Close()
	ok, d := installFile(f.Name())
	jsonReply(w, 200, ok, d)
}
