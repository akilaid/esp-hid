package ui

import (
	"errors"
	"testing"

	"esp-hid/host/internal/update"
)

type updaterProbe struct {
	notices  []string
	buttons  []bool
	alerts   []string
	quitted  int
	onMainNo int
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
		func() { p.quitted++ },
	)
	return u, p
}

func TestCheckedOffersNewRelease(t *testing.T) {
	u, p := newProbeUpdater("v2.2.0")
	rel := &update.Release{Version: "v2.3.0"}
	u.checked(rel, nil, false)
	if u.available != rel {
		t.Fatal("release not recorded")
	}
	if len(p.notices) != 1 || p.notices[0] != "Update v2.3.0 is available." || !p.buttons[0] {
		t.Fatalf("notices %v buttons %v", p.notices, p.buttons)
	}
	if len(p.alerts) != 0 {
		t.Errorf("a scheduled check must not raise alerts: %v", p.alerts)
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
	if len(p.notices) != 0 {
		t.Errorf("strip rewritten during an install: %v", p.notices)
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
