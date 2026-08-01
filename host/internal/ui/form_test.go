package ui

import (
	"strings"
	"testing"

	"esp-hid/host/internal/config"
	"esp-hid/host/internal/protocol"
)

func validForm() FormValues {
	return FormValues{
		ToggleHotkey:    "F9",
		MoveRateHz:      "45",
		Resolution:      "1920x1080",
		HostSideIndex:   0,
		CaptureKeyboard: true,
		AutoSwitch:      true,
	}
}

func TestApplyPopulatesConfig(t *testing.T) {
	cfg := config.Defaults()
	form := validForm()
	form.ToggleHotkey = "ctrl+alt+f7"
	form.MoveRateHz = "60"
	form.Resolution = "2560x1440"
	form.HostSideIndex = 1
	form.CaptureKeyboard = false
	form.AutoSwitch = false

	if err := form.Apply(&cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	// The hotkey is stored canonicalized, not as typed.
	if cfg.ToggleHotkey != "Ctrl+Alt+F7" {
		t.Errorf("ToggleHotkey = %q, want %q", cfg.ToggleHotkey, "Ctrl+Alt+F7")
	}
	if cfg.MoveRateHz != 60 {
		t.Errorf("MoveRateHz = %d, want 60", cfg.MoveRateHz)
	}
	if cfg.SlaveWidth != 2560 || cfg.SlaveHeight != 1440 {
		t.Errorf("resolution = %dx%d, want 2560x1440", cfg.SlaveWidth, cfg.SlaveHeight)
	}
	if cfg.HostSide != config.HostSideRight {
		t.Errorf("HostSide = %q, want %q", cfg.HostSide, config.HostSideRight)
	}
	if cfg.CaptureKeyboard || cfg.AutoSwitch {
		t.Error("booleans did not carry through")
	}
}

func TestApplyTrimsWhitespace(t *testing.T) {
	cfg := config.Defaults()
	form := validForm()
	form.ToggleHotkey = "  F8  "
	form.MoveRateHz = " 30 "
	if err := form.Apply(&cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if cfg.ToggleHotkey != "F8" || cfg.MoveRateHz != 30 {
		t.Errorf("got hotkey %q rate %d", cfg.ToggleHotkey, cfg.MoveRateHz)
	}
}

// A rejected form must not half-apply: the user should be able to fix one
// field without discovering the others silently changed underneath them.
func TestApplyLeavesConfigUntouchedOnError(t *testing.T) {
	cfg := config.Defaults()
	original := cfg

	form := validForm()
	form.ToggleHotkey = "F9"
	form.MoveRateHz = "60"
	form.Resolution = "not-a-resolution" // fails after the first two succeed

	if err := form.Apply(&cfg); err == nil {
		t.Fatal("expected an error for an invalid resolution")
	}
	if cfg != original {
		t.Errorf("config was mutated by a failed Apply:\n got %+v\nwant %+v", cfg, original)
	}
}

func TestApplyRejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*FormValues)
		wantWord string
	}{
		{"empty hotkey", func(f *FormValues) { f.ToggleHotkey = "" }, "hotkey"},
		{"nonsense hotkey", func(f *FormValues) { f.ToggleHotkey = "Ctrl+Nope" }, "hotkey"},
		{"rate not a number", func(f *FormValues) { f.MoveRateHz = "fast" }, "rate"},
		{"rate zero", func(f *FormValues) { f.MoveRateHz = "0" }, "rate"},
		{"rate negative", func(f *FormValues) { f.MoveRateHz = "-5" }, "rate"},
		{"rate too high", func(f *FormValues) { f.MoveRateHz = "501" }, "rate"},
		{"resolution empty", func(f *FormValues) { f.Resolution = "" }, ""},
		{"resolution malformed", func(f *FormValues) { f.Resolution = "1920*1080" }, ""},
		{"resolution too small", func(f *FormValues) { f.Resolution = "100x100" }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			form := validForm()
			tc.mutate(&form)
			err := form.Apply(&cfg)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.wantWord != "" && !strings.Contains(strings.ToLower(err.Error()), tc.wantWord) {
				t.Errorf("error %q should mention %q", err, tc.wantWord)
			}
		})
	}
}

func TestApplyAcceptsRateBounds(t *testing.T) {
	for _, rate := range []string{"1", "500"} {
		cfg := config.Defaults()
		form := validForm()
		form.MoveRateHz = rate
		if err := form.Apply(&cfg); err != nil {
			t.Errorf("rate %s should be accepted: %v", rate, err)
		}
	}
}

// An out-of-range index means "no selection"; it must leave the existing
// host side alone rather than defaulting to the first entry.
func TestApplyIgnoresOutOfRangeHostSideIndex(t *testing.T) {
	cfg := config.Defaults()
	cfg.HostSide = config.HostSideBottom
	form := validForm()
	form.HostSideIndex = -1
	if err := form.Apply(&cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if cfg.HostSide != config.HostSideBottom {
		t.Errorf("HostSide = %q, want it unchanged (%q)", cfg.HostSide, config.HostSideBottom)
	}
}

func TestFormValuesRoundTrip(t *testing.T) {
	cfg := config.Defaults()
	cfg.HostSide = config.HostSideTop
	cfg.SlaveWidth, cfg.SlaveHeight = 1080, 1920
	cfg.MoveRateHz = 90
	cfg.ToggleHotkey = "Ctrl+F5"
	cfg.CaptureKeyboard = false

	form := FormValuesFrom(cfg)
	if form.Resolution != "1080x1920" {
		t.Errorf("Resolution = %q", form.Resolution)
	}
	if form.HostSideIndex != IndexOf(HostSideChoices, config.HostSideTop) {
		t.Errorf("HostSideIndex = %d", form.HostSideIndex)
	}

	restored := config.Defaults()
	if err := form.Apply(&restored); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if restored != cfg {
		t.Errorf("round trip lost data:\n got %+v\nwant %+v", restored, cfg)
	}
}

func TestHostSideChoicesMatchConfigVocabulary(t *testing.T) {
	// The GUIs address host sides by index, so the list must stay complete
	// and every entry must pass config validation.
	if len(HostSideChoices) != 4 {
		t.Fatalf("expected 4 host sides, got %d", len(HostSideChoices))
	}
	for _, side := range HostSideChoices {
		cfg := config.Defaults()
		cfg.HostSide = side
		if err := cfg.Validate(); err != nil {
			t.Errorf("host side %q rejected by config.Validate: %v", side, err)
		}
	}
}

func TestSlaveResolutionChoicesAreAllValid(t *testing.T) {
	for _, choice := range SlaveResolutionChoices {
		if _, _, err := config.ParseResolution(choice); err != nil {
			t.Errorf("preset resolution %q is not parseable: %v", choice, err)
		}
	}
}

func TestBleStateText(t *testing.T) {
	cases := []struct {
		state protocol.BleState
		want  string
	}{
		{protocol.BleState{State: protocol.BleConnected, BondCount: 2}, "Connected (2 paired)"},
		{protocol.BleState{State: protocol.BleAdvertising, BondCount: 0}, `Advertising — pair the phone with "ESP-HID-ME"`},
		{protocol.BleState{State: protocol.BleAdvertising, BondCount: 1}, "Advertising — waiting for phone (1 paired)"},
		{protocol.BleState{State: protocol.BleIdle}, "Bluetooth starting…"},
		{protocol.BleState{State: 99}, "Unknown"},
	}
	for _, tc := range cases {
		if got := BleStateText(tc.state); got != tc.want {
			t.Errorf("BleStateText(%+v) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestFirmwareText(t *testing.T) {
	got := FirmwareText(protocol.Hello{ProtoVersion: 1, FwMajor: 1, FwMinor: 2, FwPatch: 3})
	if got != "1.2.3 (protocol v1)" {
		t.Errorf("FirmwareText = %q", got)
	}
}

func TestIndexOf(t *testing.T) {
	if got := IndexOf(HostSideChoices, "RIGHT"); got != 1 {
		t.Errorf("IndexOf should be case-insensitive, got %d", got)
	}
	if got := IndexOf(HostSideChoices, "sideways"); got != -1 {
		t.Errorf("IndexOf(missing) = %d, want -1", got)
	}
}
