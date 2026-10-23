// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

// Resource parking. With PHE_ANDROID_DOCKER set (the URL of a Docker API that may only start / stop / inspect containers, for example tecnativa/docker-socket-proxy)
// and PHE_ANDROID_CONTAINER (default phe-redroid), the bridge stops the Android container once no viewer has been connected for PHE_ANDROID_PARK_AFTER
// (a Go duration, default 15m, 0 disables) and starts it again when the next viewer connects: the page shows "Starting Android" while the bridge waits for
// sys.boot_completed. The container's data volume is kept, so installed apps and their data survive; running apps and their in-memory state do not.
// PHE_ANDROID_PARK=pause freezes the container instead of stopping it (instant resume, but its memory stays in use).

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	dockerURL = strings.TrimRight(env("PHE_ANDROID_DOCKER", ""), "/")
	container = env("PHE_ANDROID_CONTAINER", "phe-redroid")
	parkMode  = env("PHE_ANDROID_PARK", "stop")
	parkAfter = func() time.Duration {
		d, err := time.ParseDuration(env("PHE_ANDROID_PARK_AFTER", "15m"))
		if err != nil || d < 0 {
			return 15 * time.Minute
		}
		return d
	}()
)

func parkingOn() bool { return dockerURL != "" && parkAfter > 0 }

var (
	dev struct {
		sync.Mutex
		ready bool
	}
	viewers   atomic.Int32
	lastSeen  atomic.Int64 // unix nanoseconds of the last time a viewer was connected
	dockerCli = &http.Client{Timeout: 60 * time.Second}
)

func touchViewer() { lastSeen.Store(time.Now().UnixNano()) }

type ctState struct{ Running, Paused bool }

func dockerCall(method, path string) (int, []byte, error) {
	req, err := http.NewRequest(method, dockerURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := dockerCli.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var b []byte
	buf := make([]byte, 1<<16)
	for {
		n, err := resp.Body.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil || len(b) > 1<<20 {
			break
		}
	}
	return resp.StatusCode, b, nil
}

func containerState() (ctState, error) {
	code, b, err := dockerCall("GET", "/containers/"+url.PathEscape(container)+"/json")
	if err != nil {
		return ctState{}, err
	}
	if code != 200 {
		return ctState{}, fmt.Errorf("docker: container %s: HTTP %d", container, code)
	}
	var j struct{ State ctState }
	if err := json.Unmarshal(b, &j); err != nil {
		return ctState{}, err
	}
	return j.State, nil
}

// ensureDevice makes the Android device ready: starts a parked container, waits for sys.boot_completed (polling), pushes the server jar.
// notify (may be nil) hears "starting" (container being started) and "booting" (waiting for the boot to finish).
func ensureDevice(ctx context.Context, notify func(phase string)) error {
	say := func(p string) {
		if notify != nil {
			notify(p)
		}
	}
	dev.Lock()
	defer dev.Unlock()
	if dev.ready {
		return nil
	}
	if dockerURL != "" {
		st, err := containerState()
		if err != nil {
			return err
		}
		switch {
		case st.Paused:
			say("starting")
			if code, _, err := dockerCall("POST", "/containers/"+url.PathEscape(container)+"/unpause"); err != nil || code/100 != 2 {
				return fmt.Errorf("docker unpause: %v %d", err, code)
			}
		case !st.Running:
			say("starting")
			if code, _, err := dockerCall("POST", "/containers/"+url.PathEscape(container)+"/start"); err != nil || (code/100 != 2 && code != 304) {
				return fmt.Errorf("docker start: %v %d", err, code)
			}
		}
	}
	say("booting")
	deadline := time.Now().Add(4 * time.Minute)
	for ctx.Err() == nil && time.Now().Before(deadline) {
		exec.Command("adb", "connect", serial).Run()
		out, _ := adb("shell", "getprop", "sys.boot_completed").Output()
		if strings.TrimSpace(string(out)) == "1" {
			if o, err := adb("push", jar, "/data/local/tmp/scrcpy-server.jar").CombinedOutput(); err != nil {
				log.Println("push:", err, string(o))
			} else {
				log.Println("device ready, server jar pushed")
				dev.ready = true
				deskReset() // a (re)started device: display override and home app are checked again
				disp.Lock()
				disp.w, disp.h, disp.dpi = 0, 0, 0
				disp.Unlock()
				return nil
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("the Android container did not finish booting in 4 minutes")
}

// parker stops (or pauses) the container after parkAfter without a viewer.
func parker() {
	touchViewer()
	for range time.Tick(10 * time.Second) {
		if viewers.Load() > 0 {
			touchViewer()
			continue
		}
		if time.Since(time.Unix(0, lastSeen.Load())) < parkAfter {
			continue
		}
		dev.Lock()
		st, err := containerState()
		if err == nil && st.Running && !st.Paused && viewers.Load() == 0 {
			exec.Command("adb", "disconnect", serial).Run()
			path, what := "/containers/"+url.PathEscape(container)+"/stop?t=10", "stopped"
			if parkMode == "pause" {
				path, what = "/containers/"+url.PathEscape(container)+"/pause", "paused"
			}
			code, _, cerr := dockerCall("POST", path)
			log.Printf("no viewer for %s: %s %s (HTTP %d, %v)", parkAfter, what, container, code, cerr)
			dev.ready = false
		}
		dev.Unlock()
	}
}
