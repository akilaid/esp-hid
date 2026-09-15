package ui

import (
	"errors"
	"testing"

	"esp-hid/host/internal/update"
)

type updaterProbe struct {
	notices   []string
	buttons   []bool
	alerts    []string
	asked     []string // notes shown with each prompt
	answer    bool     // what the user says to the prompt
	installed int
	quitted   int
	onMainNo  int
}

func newProbeUpdater(version string) (*updater, *updaterProbe) {
	p := &updaterProbe{}
	u := newUpdater(version,
		func(fn func()) { p.onMainNo++; fn() },
		func(text string, installable bool) {
			p.notices = append(p.notices, text)
			p.buttons = append(p.buttons, installable)
		},
		func(title, message string, isError bool) { p.alerts = append(p.alerts, title) },
		func(title, message, notes string) bool { p.asked = append(p.asked, notes); return p.answer },
		func() { p.quitted++ },
	)
	u.installNow = func() { p.installed++ }
	return u, p
}

func TestCheckedOffersNewReleaseAndAsks(t *testing.T) {
	u, p := newProbeUpdater("v2.2.0")
	rel := &update.Release{Version: "v2.3.0", Notes: "Changed\n• things"}
	u.checked(rel, nil, false)
	if u.available != rel {
		t.Fatal("release not recorded")
	}
	if len(p.notices) != 1 || p.notices[0] != "Update v2.3.0 is available." || !p.buttons[0] {
		t.Fatalf("notices %v buttons %v", p.notices, p.buttons)
	}
	if len(p.asked) != 1 || p.asked[0] != "Changed\n• things" {
		t.Fatalf("prompt: %v", p.asked)
	}
	if p.installed != 0 {
		t.Error("a declined prompt started an install")
	}
	if len(p.alerts) != 0 {
		t.Errorf("a scheduled check must not raise alerts: %v", p.alerts)
	}
}

func TestYesToThePromptInstalls(t *testing.T) {
	u, p := newProbeUpdater("v2.2.0")
	p.answer = true
	u.checked(&update.Release{Version: "v2.3.0"}, nil, false)
	if p.installed != 1 {
		t.Fatalf("install started %d times, want 1", p.installed)
	}
}

func TestScheduledCheckAsksOncePerVersion(t *testing.T) {
	u, p := newProbeUpdater("v2.2.0")
	rel := &update.Release{Version: "v2.3.0"}
	u.checked(rel, nil, false)
	u.checked(rel, nil, false) // tomorrow's check, same release
	if len(p.asked) != 1 {
		t.Fatalf("asked %d times for one version, want 1", len(p.asked))
	}
	if len(p.notices) != 2 || !p.buttons[1] {
		t.Errorf("the strip should still offer it: %v %v", p.notices, p.buttons)
	}
	u.checked(&update.Release{Version: "v2.4.0"}, nil, false)
	if len(p.asked) != 2 {
		t.Errorf("a newer version should be asked about: %d", len(p.asked))
	}
	// A manual check always asks, even for the declined version.
	u.checked(rel, nil, true)
	if len(p.asked) != 3 {
		t.Errorf("manual check did not ask: %d", len(p.asked))
	}
}

func TestCheckedQuietWhenUpToDateUnlessAsked(t *testing.T) {
	u, p := newProbeUpdater("v2.2.0")
	u.checked(nil, nil, false)
	if len(p.alerts) != 0 {
		t.Errorf("scheduled up-to-date check alerted: %v", p.alerts)
	}
	if len(p.notices) != 1 || p.notices[0] != "" {
		t.Errorf("up to date should clear the strip, got %v", p.notices)
	}
	u.checked(nil, nil, true)
	if len(p.alerts) != 1 || p.alerts[0] != "Up to date" {
		t.Errorf("interactive check should say so: %v", p.alerts)
	}
}

func TestCheckedDevBuildAndErrors(t *testing.T) {
	u, p := newProbeUpdater("dev")
	u.checked(nil, update.ErrDevBuild, false)
	u.checked(nil, errors.New("boom"), false)
	if len(p.alerts) != 0 || len(p.notices) != 0 {
		t.Errorf("scheduled failures must be silent: alerts %v notices %v", p.alerts, p.notices)
	}
	u.checked(nil, update.ErrDevBuild, true)
	u.checked(nil, errors.New("boom"), true)
	if len(p.alerts) != 2 || p.alerts[0] != "Updates" || p.alerts[1] != "Update check failed" {
		t.Errorf("interactive failures should explain: %v", p.alerts)
	}
}

func TestCheckedDoesNotDisturbAnInstallInProgress(t *testing.T) {
	u, p := newProbeUpdater("v2.2.0")
	u.busy = true
	u.checked(&update.Release{Version: "v2.3.0"}, nil, false)
	u.checked(nil, nil, false)
	if len(p.notices) != 0 || len(p.asked) != 0 {
		t.Errorf("strip rewritten or prompt shown during an install: %v %v", p.notices, p.asked)
	}
}

func TestInstallNeedsAnAvailableRelease(t *testing.T) {
	u, p := newProbeUpdater("v2.2.0")
	u.install()
	if u.busy || len(p.notices) != 0 {
		t.Fatal("install started with nothing to install")
	}
}

func TestSetEnabledIsReadBySchedule(t *testing.T) {
	u, _ := newProbeUpdater("v2.2.0")
	if u.enabled.Load() {
		t.Fatal("enabled by default before the GUI says so")
	}
	u.setEnabled(true)
	if !u.enabled.Load() {
		t.Fatal("setEnabled(true) not observed")
	}
	u.stopSchedule()
	u.stopSchedule() // idempotent
}
