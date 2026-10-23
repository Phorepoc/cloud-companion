// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

// Desktop mode. A viewer of kind "large" (tablet, laptop, desktop) gets Taskbar (https://github.com/farmerbb/Taskbar, Apache-2.0, installed from F-Droid on first use, never shipped here) as the
// home app: a real taskbar with a start menu, and apps in freeform (movable, overlapping) windows. A phone viewer gets the stock launcher back. It needs a Redroid image that declares the
// freeform window feature (deploy/android/Dockerfile.redroid-native, FREEFORM=1, the default) and a rootable (userdebug) build: Taskbar's preferences are written as root, its HomeActivity
// component is enabled as root, then it is made the home activity. Without the feature or without root the display still follows the viewer and the stock launcher stays.

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	taskbarPkg  = "com.farmerbb.taskbar"
	taskbarHome = taskbarPkg + "/.activity.HomeActivity"
	stockHome   = "com.android.launcher3/.uioverrides.QuickstepLauncher"
)

// Taskbar's preferences: freeform windows, Taskbar as the home screen replacement, running.
const taskbarPrefs = `<?xml version='1.0' encoding='utf-8' standalone='yes' ?>
<map>
    <boolean name="first_run" value="false" />
    <boolean name="freeform_hack" value="true" />
    <boolean name="freeform_hack_override" value="true" />
    <boolean name="desktop_mode" value="false" />
    <boolean name="launcher" value="true" />
    <boolean name="taskbar_active" value="true" />
    <boolean name="save_window_sizes" value="false" />
</map>
`

var desk struct {
	sync.Mutex
	mode        string // "", "desktop", "phone": what the device is set up as right now
	provisioned bool
	unsupported bool // no freeform feature or no root: do not try again until the device restarts
}

// deskReset forgets what was set up (the device restarted: parking, a crash).
func deskReset() {
	desk.Lock()
	desk.mode, desk.provisioned, desk.unsupported = "", false, false
	desk.Unlock()
}

func sh(cmd string) (string, error) {
	out, err := adb("shell", cmd).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ensureRoot restarts adbd as root (Redroid is a userdebug build) and waits until the shell is uid 0.
func ensureRoot() bool {
	if out, _ := sh("id -u"); out == "0" {
		return true
	}
	adb("root").Run()
	for i := 0; i < 40; i++ {
		time.Sleep(500 * time.Millisecond)
		exec.Command("adb", "connect", serial).Run()
		if out, _ := sh("id -u"); out == "0" {
			return true
		}
	}
	return false
}

// setDesktopMode switches the home app and Taskbar for the viewer kind ("large" = desktop, anything else = phone). It is cheap when nothing changes.
func setDesktopMode(kind string) {
	want := "phone"
	if kind == "large" {
		want = "desktop"
	}
	desk.Lock()
	defer desk.Unlock()
	if desk.mode == want || desk.unsupported {
		return
	}
	if want == "desktop" {
		if !desk.provisioned {
			if feats, _ := sh("pm list features"); !strings.Contains(feats, "android.software.freeform_window_management") {
				log.Println("desktop mode: this Android image has no freeform window feature (build it with FREEFORM=1): the stock launcher stays")
				desk.unsupported = true
				return
			}
			if !ensureRoot() {
				log.Println("desktop mode: adb cannot become root on this device: the stock launcher stays")
				desk.unsupported = true
				return
			}
			if err := provisionTaskbar(); err != nil {
				log.Println("desktop mode:", err)
				desk.unsupported = true
				return
			}
			desk.provisioned = true
		}
		sh("cmd package set-home-activity " + taskbarHome)
		sh("am start -n " + taskbarHome)
		sh("input keyevent 3")
	} else if desk.mode != "" || desk.provisioned {
		sh("am broadcast -a com.farmerbb.taskbar.QUIT -p " + taskbarPkg)
		sh("am force-stop " + taskbarPkg)
		sh("cmd package set-home-activity " + stockHome)
		sh("input keyevent 3")
	}
	desk.mode = want
	log.Println("desktop mode:", want)
}

// provisionTaskbar installs Taskbar (from F-Droid, once), grants what it needs and writes its preferences as root.
func provisionTaskbar() error {
	if out, _ := sh("pm path " + taskbarPkg); !strings.HasPrefix(out, "package:") {
		ok, detail := fdroidInstall(context.Background(), taskbarPkg)
		if !ok {
			return fmt.Errorf("installing Taskbar from F-Droid failed: %s", detail)
		}
	}
	sh("settings put global enable_freeform_support 1; settings put global force_resizable_activities 1")
	sh("appops set " + taskbarPkg + " SYSTEM_ALERT_WINDOW allow; appops set " + taskbarPkg + " GET_USAGE_STATS allow")
	sh("am force-stop " + taskbarPkg)
	f, err := os.CreateTemp("", "tb-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	f.WriteString(taskbarPrefs)
	f.Close()
	if o, err := adb("push", f.Name(), "/data/local/tmp/taskbar-prefs.xml").CombinedOutput(); err != nil {
		return fmt.Errorf("push preferences: %v %s", err, strings.TrimSpace(string(o)))
	}
	dir := "/data/data/" + taskbarPkg
	uid, _ := sh("stat -c %u " + dir)
	if uid == "" {
		return fmt.Errorf("Taskbar has no data directory")
	}
	prefs := dir + "/shared_prefs/" + taskbarPkg + "_preferences.xml"
	if out, err := sh(fmt.Sprintf("mkdir -p %s/shared_prefs && cp /data/local/tmp/taskbar-prefs.xml %s && chown -R %s:%s %s/shared_prefs && chmod 660 %s && rm /data/local/tmp/taskbar-prefs.xml", dir, prefs, uid, uid, dir, prefs)); err != nil {
		return fmt.Errorf("write preferences: %v %s", err, out)
	}
	sh("restorecon -R " + dir + "/shared_prefs")
	sh("pm enable " + taskbarPkg + "/.activity.HomeActivity; pm enable " + taskbarPkg + "/.activity.HSLActivity")
	return nil
}
