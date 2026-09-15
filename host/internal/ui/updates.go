package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"esp-hid/host/internal/update"
)

// How long after launch the first check runs — after device discovery has
// had its moment — and how often it repeats while the app stays open.
const (
	updateFirstCheckDelay = 5 * time.Second
	updateCheckInterval   = 24 * time.Hour
)

// updater is the update flow both GUIs share: check, ask, download,
// install. The platform supplies five callbacks; everything else — when to
// check, what to say, what to do on failure — lives here, tested once.
//
// Nothing is ever installed unasked. A newer release is put to the user in
// a dialog that carries its release notes; a no leaves the offer in the
// update strip for later. The startup and daily checks ask once per version;
// "Check for Updates…" asks every time, since the user just requested it.
//
// State is touched only on the GUI thread. Network work runs on goroutines
// and reports back through onMain, the same way device events do.
type updater struct {
	version string
	client  *http.Client
	apiBase string

	// onMain runs fn on the GUI thread.
	onMain func(fn func())
	// notify shows text in the update strip, with the install button when
	// installable; empty text clears it.
	notify func(text string, installable bool)
	alert  func(title, message string, isError bool)
	// ask puts a release to the user — title, a one-line message, the
	// release notes — and reports whether they chose to install.
	ask func(title, message, notes string) bool
	// quit ends the app once the new version is in place and set to relaunch.
	quit func()
	// installNow is install, indirected so tests can see the decision
	// without a download starting.
	installNow func()

	// Read by the schedule goroutine, written by the GUI when the setting
	// changes, so it is atomic rather than a Config field.
	enabled atomic.Bool

	available *update.Release
	// The version the scheduled check last asked about, so it does not ask
	// again tomorrow for a release the user already declined.
	offered string
	busy    bool
	stop    chan struct{}
}

func newUpdater(version string, onMain func(func()),
	notify func(string, bool), alert func(string, string, bool),
	ask func(string, string, string) bool, quit func()) *updater {
	apiBase := update.APIBase
	// Test hook: a local server standing in for GitHub, so the whole flow —
	// notice, download, verify, swap, relaunch — can be exercised against a
	// build that is not on GitHub yet.
	if override := os.Getenv("ESP_HID_UPDATE_API"); override != "" {
		apiBase = override
	}
	u := &updater{
		version: version,
		client:  &http.Client{Timeout: update.Timeout},
		apiBase: apiBase,
		onMain:  onMain,
		notify:  notify,
		alert:   alert,
		ask:     ask,
		quit:    quit,
		stop:    make(chan struct{}),
	}
	u.installNow = u.install
	return u
}

// setEnabled reflects the "check automatically" setting. The schedule keeps
// ticking either way and consults this each time, so turning checks back on
// needs no restart.
func (u *updater) setEnabled(on bool) { u.enabled.Store(on) }

// start runs the periodic check until stopSchedule is called.
func (u *updater) start() {
	go func() {
		timer := time.NewTimer(updateFirstCheckDelay)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
			case <-u.stop:
				return
			}
			if u.enabled.Load() {
				u.check(false)
			}
			timer.Reset(updateCheckInterval)
		}
	}()
}

func (u *updater) stopSchedule() {
	select {
	case <-u.stop:
	default:
		close(u.stop)
	}
}

// checkNow is the menu item: it always runs and always answers, up to date
// included. The scheduled check stays quiet unless there is something to say.
func (u *updater) checkNow() {
	if u.busy {
		return
	}
	go u.check(true)
}

// check runs off the GUI thread and hands the result to checked on it.
func (u *updater) check(interactive bool) {
	ctx, cancel := context.WithTimeout(context.Background(), update.Timeout)
	defer cancel()
	rel, err := update.Check(ctx, u.client, u.apiBase, u.version)
	u.onMain(func() { u.checked(rel, err, interactive) })
}

func (u *updater) checked(rel *update.Release, err error, interactive bool) {
	switch {
	case errors.Is(err, update.ErrDevBuild):
		if interactive {
			u.alert("Updates", "This is a development build ("+u.version+"); updates are not tracked.", false)
		}
	case err != nil:
		log.Printf("update check: %v", err)
		if interactive {
			u.alert("Update check failed", err.Error(), true)
		}
	case rel == nil:
		u.available = nil
		if !u.busy {
			u.notify("", false)
		}
		if interactive {
			u.alert("Up to date", "ESP HID Bridge "+u.version+" is the latest release.", false)
		}
	default:
		u.available = rel
		if u.busy {
			return
		}
		u.notify(availableText(rel), true)
		if !interactive && u.offered == rel.Version {
			return
		}
		u.offered = rel.Version
		if u.ask("Update available",
			"ESP HID Bridge "+rel.Version+" is available; you have "+u.version+".",
			rel.Notes) {
			u.installNow()
		}
	}
}

func availableText(rel *update.Release) string {
	return "Update " + rel.Version + " is available."
}

// install is the button: download, verify, swap, quit for the relaunch. The
// strip narrates progress and the button is withdrawn until it is over, so
// a second click cannot start a second download.
func (u *updater) install() {
	rel := u.available
	if rel == nil || u.busy {
		return
	}
	u.busy = true
	u.notify("Downloading "+rel.Version+"…", false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), update.Timeout)
		defer cancel()
		dir, err := os.MkdirTemp("", "esp-hid-update-")
		var path string
		if err == nil {
			lastPct := int64(-1)
			path, err = update.Download(ctx, u.client, rel, dir, func(done, total int64) {
				if total <= 0 {
					return
				}
				if pct := done * 100 / total; pct != lastPct {
					lastPct = pct
					u.onMain(func() {
						u.notify(fmt.Sprintf("Downloading %s… %d%%", rel.Version, pct), false)
					})
				}
			})
		}
		if err == nil {
			u.onMain(func() { u.notify("Installing "+rel.Version+"…", false) })
			err = update.Install(path)
		}
		if dir != "" {
			// The package has been unpacked or moved into place; on failure
			// it is of no further use either.
			os.RemoveAll(dir)
		}
		u.onMain(func() {
			u.busy = false
			if err != nil {
				log.Printf("update install: %v", err)
				u.notify(availableText(rel), true)
				u.alert("Update failed", err.Error()+"\n\nThe release can be downloaded by hand from\n"+rel.URL, true)
				return
			}
			u.quit()
		})
	}()
}
