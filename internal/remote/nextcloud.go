// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Nextcloud's files are WebDAV at remote.php/dav/files/<user ID>/; the user ID (not always the login name: people may sign in
// with their email address) comes from Nextcloud's user API.
func Nextcloud(ctx context.Context, server, login, pass string, insecure bool) (*WebDAV, error) {
	base, err := ncBase(server)
	if err != nil {
		return nil, err
	}
	hc := client(insecure)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", base+"/ocs/v2.php/cloud/user?format=json", nil)
	r.SetBasicAuth(login, pass)
	r.Header.Set("OCS-APIRequest", "true")
	res, err := hc.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return nil, ErrAuth
	}
	var me struct {
		OCS struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"ocs"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&me) != nil || me.OCS.Data.ID == "" {
		return nil, fmt.Errorf("this does not look like a Nextcloud server (%s)", res.Status)
	}
	d, err := NewWebDAV(base+"/remote.php/dav/files/"+url.PathEscape(me.OCS.Data.ID)+"/", login, pass, insecure)
	if err != nil {
		return nil, err
	}
	d.HTTP = hc
	return d, nil
}

func ncBase(server string) (string, error) {
	s := strings.TrimRight(strings.TrimSpace(server), "/")
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("the Nextcloud address is needed, as https://cloud.example.org")
	}
	return s, nil
}

// NCLogin is Nextcloud's sign-in from another app (Login Flow v2): the person opens Login in a browser and approves Cloud
// Companion there; Poll then hands over an app password made for it.
type NCLogin struct {
	Login    string `json:"login"` // to open in the browser
	token    string
	endpoint string
	insecure bool
}

// NCLoginResult is what Nextcloud hands over once approved.
type NCLoginResult struct {
	Server, LoginName, AppPassword string
}

// StartNCLogin asks the server for a sign-in.
func StartNCLogin(ctx context.Context, server string, insecure bool) (*NCLogin, error) {
	base, err := ncBase(server)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", base+"/index.php/login/v2", nil)
	r.Header.Set("User-Agent", "Cloud Companion") // Nextcloud names the app password after it
	res, err := client(insecure).Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var j struct {
		Poll struct {
			Token    string `json:"token"`
			Endpoint string `json:"endpoint"`
		} `json:"poll"`
		Login string `json:"login"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&j) != nil || j.Login == "" || j.Poll.Token == "" {
		return nil, fmt.Errorf("this does not look like a Nextcloud server (%s)", res.Status)
	}
	return &NCLogin{Login: j.Login, token: j.Poll.Token, endpoint: j.Poll.Endpoint, insecure: insecure}, nil
}

// Poll asks once whether the person has approved: nil and no error while they have not (yet).
func (l *NCLogin) Poll(ctx context.Context) (*NCLoginResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", l.endpoint, strings.NewReader("token="+url.QueryEscape(l.token)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client(l.insecure).Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, nil // not approved yet
	}
	var j struct {
		Server      string `json:"server"`
		LoginName   string `json:"loginName"`
		AppPassword string `json:"appPassword"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&j) != nil || j.AppPassword == "" {
		return nil, fmt.Errorf("the sign-in did not complete (%s)", res.Status)
	}
	return &NCLoginResult{Server: j.Server, LoginName: j.LoginName, AppPassword: j.AppPassword}, nil
}
