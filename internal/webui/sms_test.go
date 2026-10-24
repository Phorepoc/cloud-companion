// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phorepoc/cloud-companion/internal/store"
)

// fakeDevice is a paired device that signs its requests with its key, like the app and a phone service would.
type fakeDevice struct {
	id  string
	key []byte
	srv *httptest.Server
}

func (d *fakeDevice) call(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	u := path
	if i := indexByte(path, '?'); i >= 0 {
		u = path[:i]
	}
	req, _ := http.NewRequest(method, d.srv.URL+path, rd)
	ts := fmt.Sprint(time.Now().Unix())
	m := hmac.New(sha256.New, d.key)
	m.Write([]byte(d.id + "." + ts + "." + method + " " + u))
	req.Header.Set("X-Phe-Auth", d.id+"."+ts+"."+hex.EncodeToString(m.Sum(nil)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func TestSMSSync(t *testing.T) {
	st := store.OpenTest(t)
	mux := http.NewServeMux()
	(&smsAPI{a: newAuth(st), st: st}).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mk := func(name string, admin bool) *fakeDevice {
		id, key, err := st.NewDevice(name, admin)
		if err != nil {
			t.Fatal(err)
		}
		return &fakeDevice{id, key, srv}
	}
	admin, phone, other := mk("web", true), mk("Phone A", false), mk("Phone B", false)

	msgs := func(ids ...string) []map[string]any {
		var out []map[string]any
		for _, id := range ids {
			n := map[string]string{"m1": "+49 171 1112233", "m2": "0171 1112233", "m3": "0171 1112233", "m4": "+49 30 998877"}[id]
			dir := map[string]string{"m1": "in", "m2": "out", "m3": "in", "m4": "in"}[id]
			ts := map[string]int64{"m1": 1000, "m2": 1010, "m3": 1020, "m4": 1030}[id]
			out = append(out, map[string]any{"id": id, "number": n, "dir": dir, "body": "text " + id, "ts": ts})
		}
		return out
	}
	// 1. upload with the phone's own ids; a retry changes nothing; an overlapping batch adds only the new one
	code, r := phone.call(t, "POST", "/sms/sync", map[string]any{"messages": msgs("m1", "m2", "m3")})
	if code != 200 || r["accepted"].(float64) != 3 {
		t.Fatalf("first upload: %d %v", code, r)
	}
	if _, r := phone.call(t, "POST", "/sms/sync", map[string]any{"messages": msgs("m1", "m2", "m3")}); r["accepted"].(float64) != 0 {
		t.Fatalf("a retry was stored again: %v", r)
	}
	if _, r := phone.call(t, "POST", "/sms/sync", map[string]any{"messages": msgs("m3", "m4")}); r["accepted"].(float64) != 1 {
		t.Fatalf("overlap: %v", r)
	}
	// the same phone message id from ANOTHER device is a different message
	if _, r := other.call(t, "POST", "/sms/sync", map[string]any{"messages": msgs("m1")}); r["accepted"].(float64) != 1 {
		t.Fatalf("ids are per device: %v", r)
	}

	// 2. conversations are by number: +49 171 ... and 0171 ... are one thread, across devices
	code, r = admin.call(t, "GET", "/sms/conversations", nil)
	convs := r["conversations"].([]any)
	if code != 200 || len(convs) != 2 {
		t.Fatalf("conversations: %d %v", code, r)
	}
	var key string
	for _, c := range convs {
		cm := c.(map[string]any)
		if cm["count"].(float64) == 4 {
			key = cm["key"].(string)
		}
	}
	if key != store.NumberKey("0171 1112233") {
		t.Fatalf("the two spellings of one number must be one conversation of 4 messages (3 from Phone A, 1 from Phone B): %v", convs)
	}
	_, r = admin.call(t, "GET", "/sms/thread?key="+key, nil)
	th := r["messages"].([]any)
	if len(th) != 4 || th[0].(map[string]any)["ts"].(float64) > th[3].(map[string]any)["ts"].(float64) {
		t.Fatalf("thread: %v", th)
	}
	if code, _ := phone.call(t, "GET", "/sms/conversations", nil); code != 403 {
		t.Fatalf("a phone must not read the conversations (HTTP %d)", code)
	}

	// 3. outbound: the web app queues for one device, only that device sees it, the acks move the status
	if code, _ := admin.call(t, "POST", "/sms/send", map[string]any{"device": "nobody", "number": "0171 1112233", "body": "x"}); code != 404 {
		t.Fatalf("send to an unknown device: %d", code)
	}
	_, r = admin.call(t, "POST", "/sms/send", map[string]any{"device": phone.id, "number": "0171 1112233", "body": "hello from the web"})
	outID := r["id"].(float64)
	_, r2 := admin.call(t, "POST", "/sms/send", map[string]any{"device": phone.id, "number": "0171 9990000", "body": "will fail"})
	failID := r2["id"].(float64)
	_, r = other.call(t, "POST", "/sms/sync", map[string]any{})
	if len(r["pending"].([]any)) != 0 {
		t.Fatalf("another device must not see the queue: %v", r)
	}
	_, r = phone.call(t, "POST", "/sms/sync", map[string]any{})
	pend := r["pending"].([]any)
	if len(pend) != 2 || pend[0].(map[string]any)["out_id"].(float64) != outID || pend[0].(map[string]any)["body"] != "hello from the web" {
		t.Fatalf("pending: %v", pend)
	}
	// the phone sends, reports sent (with its own id), then delivered; the other one fails
	phone.call(t, "POST", "/sms/sync", map[string]any{"acks": []map[string]any{
		{"out_id": outID, "id": "p-100", "status": "sent"},
		{"out_id": failID, "id": "p-101", "status": "failed", "error": "no network"}}})
	_, r = phone.call(t, "POST", "/sms/sync", map[string]any{})
	if len(r["pending"].([]any)) != 0 {
		t.Fatalf("acked messages must leave the queue: %v", r)
	}
	phone.call(t, "POST", "/sms/sync", map[string]any{"acks": []map[string]any{{"out_id": outID, "id": "p-100", "status": "delivered"}}})
	// another device cannot ack it
	other.call(t, "POST", "/sms/sync", map[string]any{"acks": []map[string]any{{"out_id": outID, "status": "failed"}}})
	status := func(id float64) string {
		for _, c := range convs {
			_ = c
		}
		_, r := admin.call(t, "GET", "/sms/thread?key="+key, nil)
		for _, m := range r["messages"].([]any) {
			mm := m.(map[string]any)
			if mm["id"].(float64) == id {
				return mm["status"].(string) + "/" + fmt.Sprint(mm["dev_msg_id"])
			}
		}
		_, r = admin.call(t, "GET", "/sms/thread?key="+store.NumberKey("0171 9990000"), nil)
		for _, m := range r["messages"].([]any) {
			mm := m.(map[string]any)
			if mm["id"].(float64) == id {
				return mm["status"].(string) + "/" + fmt.Sprint(mm["error"])
			}
		}
		return "?"
	}
	if s := status(outID); s != "delivered/p-100" {
		t.Fatalf("outbound status %q, want delivered/p-100", s)
	}
	if s := status(failID); s != "failed/no network" {
		t.Fatalf("failed status %q", s)
	}
	// the phone uploads its own copy of the sent message later: it must not duplicate it
	if _, r := phone.call(t, "POST", "/sms/sync", map[string]any{"messages": []map[string]any{{"id": "p-100", "number": "0171 1112233", "dir": "out", "body": "hello from the web", "ts": 2000}}}); r["accepted"].(float64) != 0 {
		t.Fatalf("the phone's copy of a message sent from the web was stored twice: %v", r)
	}
	// a bad signature and a replayed old timestamp are refused
	req, _ := http.NewRequest("POST", srv.URL+"/sms/sync", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Phe-Auth", phone.id+".1.deadbeef")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 403 {
		t.Fatalf("a forged signature was accepted (%d)", resp.StatusCode)
	}
}

func TestSMSPair(t *testing.T) {
	st := store.OpenTest(t)
	mux := http.NewServeMux()
	(&smsAPI{a: newAuth(st), st: st}).routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	post := func(code string) (int, map[string]any) {
		b, _ := json.Marshal(map[string]string{"code": code, "name": "Pixel"})
		resp, err := http.Post(srv.URL+"/sms/pair", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if c, _ := post("NOPE-NOPE"); c != 403 {
		t.Fatalf("a wrong code: %d", c)
	}
	code, err := st.NewPairCode(time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	c, r := post(code)
	if c != 200 || r["dev"] == nil || len(r["key"].(string)) != 64 {
		t.Fatalf("pair: %d %v", c, r)
	}
	if c, _ := post(code); c != 403 {
		t.Fatalf("a code works once: %d", c)
	}
	// the new device can sync with its key
	key, _ := hex.DecodeString(r["key"].(string))
	d := &fakeDevice{r["dev"].(string), key, srv}
	// (no /sms/sync route differences: the routes are the same mux)
	if c, out := d.call(t, "POST", "/sms/sync", map[string]any{}); c != 200 || out["accepted"].(float64) != 0 {
		t.Fatalf("sync after pairing: %d %v", c, out)
	}
}
