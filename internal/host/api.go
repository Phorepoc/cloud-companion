// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package host

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
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
	// backups (backup.go): the destination without secrets, the last run, the SFTP key
	mux.HandleFunc("GET /v1/backup", func(w http.ResponseWriter, r *http.Request) {
		c := m.BackupConfig()
		out := map[string]any{"on": c.On, "dest": c.Dest.Public(), "at": c.At, "last": c.Last, "hasPassword": c.Dest.Password != ""}
		if k, err := m.SSHKey(); err == nil {
			out["sshKey"] = k
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("PUT /v1/backup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			On   bool        `json:"on"`
			Dest Destination `json:"dest"`
			At   string      `json:"at"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		j, err := m.SetBackup(in.On, in.Dest, in.At)
		if err == nil && j == nil {
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
		jobReply(w, j, err)
	})
	mux.HandleFunc("POST /v1/backup/run", func(w http.ResponseWriter, r *http.Request) {
		j, err := m.BackupNow()
		jobReply(w, j, err)
	})
	mux.HandleFunc("GET /v1/backup/snapshots", func(w http.ResponseWriter, r *http.Request) {
		snaps, err := m.Snapshots(r.Context(), m.BackupConfig().Dest)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"snapshots": snaps})
	})
	mux.HandleFunc("GET /v1/backup/usb", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"disks": m.USBDisks(r.Context())})
	})
	// remote login (ssh.go)
	mux.HandleFunc("GET /v1/ssh", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"available": m.SSHUser != "", "ssh": m.SSH()})
	})
	mux.HandleFunc("PUT /v1/ssh", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			On       bool   `json:"on"`
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s, err := m.SetSSH(r.Context(), in.On, in.User, in.Password)
		if err != nil && !in.On && !s.On {
			// switched off, but locking the account or stopping the service failed: the page shows Off and says what went wrong
			writeJSON(w, http.StatusOK, map[string]any{"available": true, "ssh": s, "warning": "Remote login is off, but something didn't stop cleanly: " + err.Error()})
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"available": true, "ssh": s})
	})
	// updates (update.go)
	mux.HandleFunc("GET /v1/update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.updateInfo())
	})
	mux.HandleFunc("POST /v1/update/check", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		m.CheckUpdate(ctx) // a failure is in the answer (error)
		writeJSON(w, http.StatusOK, m.updateInfo())
	})
	mux.HandleFunc("PUT /v1/update", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Auto bool `json:"auto"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := m.SetAutoUpdate(in.Auto); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, m.updateInfo())
	})
	mux.HandleFunc("POST /v1/update", func(w http.ResponseWriter, r *http.Request) {
		j, err := m.Update()
		jobReply(w, j, err)
	})
	mux.HandleFunc("GET /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		out := []JobStatus{}
		for _, j := range m.Running() {
			out = append(out, j.Status())
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
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

// updateInfo is what the Updates page shows.
func (m *Manager) updateInfo() map[string]any {
	c := m.UpdateConfig()
	return map[string]any{"version": m.Version(), "auto": c.Auto, "checked": c.Checked, "available": c.Available, "last": c.Last,
		"error": c.Error, "at": m.BackupConfig().At, "backups": m.BackupConfig().On}
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
	if mi, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		b.Storage = rootStorage(string(mi))
	}
	return b
}

// rootStorage is the kind of disk the system runs from, from /proc/self/mountinfo: "sd" (an SD card, which wears out under a
// server's writes), "nvme", "usb" (or another disk), or "" when unknown.
func rootStorage(mountinfo string) string {
	for _, l := range strings.Split(mountinfo, "\n") {
		f := strings.Fields(l)
		if len(f) < 10 || f[4] != "/" {
			continue
		}
		for i, x := range f {
			if x == "-" && i+2 < len(f) {
				src := f[i+2]
				switch {
				case strings.HasPrefix(src, "/dev/mmcblk"):
					return "sd"
				case strings.HasPrefix(src, "/dev/nvme"):
					return "nvme"
				case strings.HasPrefix(src, "/dev/sd"):
					return "usb"
				}
				return ""
			}
		}
	}
	return ""
}
