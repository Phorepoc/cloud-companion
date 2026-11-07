// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package webui

// The board's host manager (docs/host-manager.md, cmd/phehost), for the owner's Features and System pages. On a board the manager's
// socket is mounted into this container (docker-compose.board.yml); elsewhere there is none and the pages say the server is set up
// by hand. Only these calls go through, only for an admin device.
//
//	GET    /phe/host                 -> {available, system?}
//	GET    /phe/host/features        -> the manager's /v1/features
//	POST   /phe/host/features/{id}   {settings} -> a job
//	DELETE /phe/host/features/{id}   -> a job
//	GET    /phe/host/jobs/{id}       -> the job (?wait=1: when it has ended)

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

type hostAPI struct {
	a    *auth
	sock string
	c    *http.Client
}

func newHostAPI(a *auth, sock string) *hostAPI {
	return &hostAPI{a: a, sock: sock, c: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}}
}

var hostID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func (h *hostAPI) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /phe/host", func(w http.ResponseWriter, r *http.Request) {
		if !h.a.adminDevice(r) {
			bad(w, 403, "only an admin device can do this")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		res, err := h.do(ctx, "GET", "/v1/system", nil)
		if err != nil {
			reply(w, 200, map[string]any{"available": false})
			return
		}
		defer res.Body.Close()
		var sys map[string]any
		json.NewDecoder(res.Body).Decode(&sys)
		reply(w, 200, map[string]any{"available": true, "system": sys})
	})
	mux.HandleFunc("GET /phe/host/features", func(w http.ResponseWriter, r *http.Request) { h.pass(w, r, "GET", "/v1/features") })
	mux.HandleFunc("POST /phe/host/features/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.passID(w, r, "POST", "/v1/features/")
	})
	mux.HandleFunc("DELETE /phe/host/features/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.passID(w, r, "DELETE", "/v1/features/")
	})
	mux.HandleFunc("GET /phe/host/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		q := ""
		if r.URL.Query().Get("wait") == "1" {
			q = "?wait=1"
		}
		id := r.PathValue("id")
		if !hostID.MatchString(id) {
			bad(w, 400, "bad id")
			return
		}
		h.pass(w, r, "GET", "/v1/jobs/"+id+q)
	})
}

func (h *hostAPI) passID(w http.ResponseWriter, r *http.Request, method, prefix string) {
	id := r.PathValue("id")
	if !hostID.MatchString(id) {
		bad(w, 400, "bad id")
		return
	}
	h.pass(w, r, method, prefix+url.PathEscape(id))
}

// pass sends the request on to the manager and its answer back.
func (h *hostAPI) pass(w http.ResponseWriter, r *http.Request, method, path string) {
	if !h.a.adminDevice(r) {
		bad(w, 403, "only an admin device can do this")
		return
	}
	var body io.Reader
	if method == "POST" {
		body = http.MaxBytesReader(w, r.Body, 64<<10)
	}
	res, err := h.do(r.Context(), method, path, body)
	if err != nil {
		bad(w, 503, "the board's manager is not reachable")
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	io.Copy(w, io.LimitReader(res.Body, 4<<20))
}

func (h *hostAPI) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://phehost"+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return h.c.Do(req)
}
