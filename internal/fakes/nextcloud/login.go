// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Login Flow v2 (https://docs.nextcloud.com/server/latest/developer_manual/client_apis/LoginFlow/): the app starts a sign-in and
// polls; the person opens the login address, logs in and grants access on plain pages (Nextcloud's own, reduced to the same
// buttons and fields); the poll then hands over a new app password once.
package nextcloud

import (
	"html/template"
	"net/http"
)

// base is the server's address as the caller reached it.
func base(r *http.Request) string { return "http://" + r.Host }

func (s *Server) flowStart(w http.ResponseWriter, r *http.Request) {
	poll, login := randHex(32), randHex(32)
	s.mu.Lock()
	s.flows[poll] = &flow{}
	s.byLogin[login] = poll
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"poll":  map[string]string{"token": poll, "endpoint": base(r) + "/index.php/login/v2/poll"},
		"login": base(r) + "/index.php/login/v2/flow/" + login,
	})
}

func (s *Server) flowPoll(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	t := r.PostForm.Get("token")
	s.mu.Lock()
	f := s.flows[t]
	if f == nil || f.appPass == "" {
		s.mu.Unlock()
		writeJSON(w, 404, []any{}) // not granted (yet)
		return
	}
	delete(s.flows, t) // handed over once
	s.mu.Unlock()
	writeJSON(w, 200, map[string]string{"server": base(r), "loginName": f.login, "appPassword": f.appPass})
}

var page = template.Must(template.New("p").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>Nextcloud</title></head><body>
{{if eq .Step "start"}}<h1>Connect to your account</h1><p>Please log in before granting Cloud Companion access to your Nextcloud account.</p>
<form method="get"><input type="hidden" name="step" value="login"><button type="submit">Log in</button></form>
{{else if eq .Step "login"}}<h1>Log in to Nextcloud</h1>{{if .Err}}<p role="alert">Wrong login or password.</p>{{end}}
<form method="post"><input type="hidden" name="step" value="login"><input name="user" autocomplete="username"><input name="password" type="password"><button type="submit">Log in</button></form>
{{else if eq .Step "grant"}}<h1>Account access</h1><p>You are about to grant Cloud Companion access to your account {{.User}}.</p>
<form method="post"><input type="hidden" name="step" value="grant"><input type="hidden" name="user" value="{{.User}}"><input type="hidden" name="password" value="{{.Pass}}"><button type="submit">Grant access</button></form>
{{else}}<h1>Account connected</h1><p>Your client should now be connected! You can close this window.</p>{{end}}
</body></html>`))

// flowPage is the browser's side: start → log in → grant access → connected.
func (s *Server) flowPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	poll, ok := s.byLogin[r.PathValue("token")]
	f := s.flows[poll]
	s.mu.Unlock()
	if !ok || f == nil {
		http.Error(w, "This sign-in has expired.", http.StatusNotFound)
		return
	}
	r.ParseForm()
	d := struct {
		Step, User, Pass string
		Err              bool
	}{Step: "start"}
	if r.Method == "GET" && r.Form.Get("step") == "login" {
		d.Step = "login"
	}
	if r.Method == "POST" {
		d.User, d.Pass = r.PostForm.Get("user"), r.PostForm.Get("password")
		s.mu.Lock()
		good := d.User != "" && s.pass[d.User] == d.Pass
		switch {
		case !good:
			d.Step, d.Err = "login", true
		case r.PostForm.Get("step") == "grant":
			d.Step, f.login, f.appPass = "done", d.User, randHex(36)
			s.apps[f.appPass] = d.User
			delete(s.byLogin, r.PathValue("token"))
		default:
			d.Step = "grant"
		}
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Execute(w, d)
}
