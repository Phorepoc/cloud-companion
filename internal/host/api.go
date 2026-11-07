// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Handler is the manager's API (/v1/), served on a Unix socket that only Cloud Companion's container can reach; Cloud Companion
// lets only the owner's requests through.
func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/system", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"board": m.Board})
	})
	mux.HandleFunc("GET /v1/features", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"features": m.Features()})
	})
	mux.HandleFunc("POST /v1/features/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Settings map[string]string `json:"settings"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		j, err := m.Enable(r.PathValue("id"), body.Settings)
		jobReply(w, j, err)
	})
	mux.HandleFunc("DELETE /v1/features/{id}", func(w http.ResponseWriter, r *http.Request) {
		j, err := m.Disable(r.PathValue("id"))
		jobReply(w, j, err)
	})
	mux.HandleFunc("GET /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		j := m.Job(r.PathValue("id"))
		if j == nil {
			writeError(w, http.StatusNotFound, errors.New("no such job"))
			return
		}
		if r.URL.Query().Get("wait") == "1" { // until it ends (or the caller gives up)
			select {
			case <-j.Done():
			case <-r.Context().Done():
				return
			}
		}
		writeJSON(w, http.StatusOK, j.Status())
	})
	return mux
}

func jobReply(w http.ResponseWriter, j *Job, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusConflict, err)
	default:
		writeJSON(w, http.StatusAccepted, j.Status())
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// DetectBoard reads the board's model, processor and memory.
func DetectBoard() Board {
	b := Board{Arch: runtime.GOARCH}
	if m, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		b.Model = strings.TrimRight(string(m), "\x00\n")
	}
	if mi, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(mi), "\n") {
			if f := strings.Fields(l); len(f) >= 2 && f[0] == "MemTotal:" {
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				b.Memory = kb << 10
			}
		}
	}
	return b
}
