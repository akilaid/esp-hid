// Package ui hosts the desktop GUIs. The platform files (gui_windows.go,
// gui_darwin.go) own widgets and event loops; this file owns everything they
// have in common — the settings the form exposes, how raw form text becomes a
// validated config, and how device state is worded for the user.
//
// Keeping this untagged means both GUIs validate input through exactly one
// code path, and that path is tested on every platform's CI rather than only
// where a window can be opened.
package ui

import (
	"fmt"
	"strconv"
	"strings"

	"esp-hid/host/internal/config"
	"esp-hid/host/internal/hotkey"
	"esp-hid/host/internal/protocol"
)

// SlaveResolutionChoices seeds the resolution picker. The list is editable in
// both GUIs, so an unlisted resolution can still be typed in.
var SlaveResolutionChoices = []string{
	"1280x720", "1366x768", "1600x900", "1920x1080", "2560x1440", "3840x2160",
	"720x1280", "768x1366", "900x1600", "1080x1920", "1440x2560", "2160x3840",
}

// HostSideChoices lists which edge of the host's screen the slave sits on.
// Order is load-bearing: the GUIs address these by index.
var HostSideChoices = []string{
	config.HostSideLeft, config.HostSideRight, config.HostSideTop, config.HostSideBottom,
}

// Limits on the move send rate. Below 1 nothing would ever be sent; above 500
// the device cannot keep up and the queue just backs up.
const (
	MinMoveRateHz = 1
	MaxMoveRateHz = 500
)

// FormValues is the raw, unvalidated content of the settings form. Text
// fields stay strings so validation errors can quote exactly what the user
// typed.
type FormValues struct {
	ToggleHotkey    string
	MoveRateHz      string
	Resolution      string
	HostSideIndex   int
	CaptureKeyboard bool
	AutoSwitch      bool
}

// FormValuesFrom renders a config back into form fields, for populating the
// widgets at startup.
func FormValuesFrom(cfg config.Config) FormValues {
	return FormValues{
		ToggleHotkey:    cfg.ToggleHotkey,
		MoveRateHz:      strconv.Itoa(cfg.MoveRateHz),
		Resolution:      fmt.Sprintf("%dx%d", cfg.SlaveWidth, cfg.SlaveHeight),
		HostSideIndex:   IndexOf(HostSideChoices, cfg.HostSide),
		CaptureKeyboard: cfg.CaptureKeyboard,
		AutoSwitch:      cfg.AutoSwitch,
	}
}

// Apply validates the form and writes it into cfg. cfg is left untouched if
// anything fails, so a rejected form can never half-apply.
func (f FormValues) Apply(cfg *config.Config) error {
	updated := *cfg

	combo, ok := hotkey.Normalize(strings.TrimSpace(f.ToggleHotkey))
	if !ok {
		return fmt.Errorf("invalid hotkey %q (examples: F9, Ctrl+Alt+F7)", strings.TrimSpace(f.ToggleHotkey))
	}
	updated.ToggleHotkey = combo

	rate, err := strconv.Atoi(strings.TrimSpace(f.MoveRateHz))
	if err != nil || rate < MinMoveRateHz || rate > MaxMoveRateHz {
		return fmt.Errorf("send rate must be a number between %d and %d", MinMoveRateHz, MaxMoveRateHz)
	}
	updated.MoveRateHz = rate

	width, height, err := config.ParseResolution(f.Resolution)
	if err != nil {
		return err
	}
	updated.SlaveWidth = width
	updated.SlaveHeight = height

	if f.HostSideIndex >= 0 && f.HostSideIndex < len(HostSideChoices) {
		updated.HostSide = HostSideChoices[f.HostSideIndex]
	}
	updated.CaptureKeyboard = f.CaptureKeyboard
	updated.AutoSwitch = f.AutoSwitch

	if err := updated.Validate(); err != nil {
		return err
	}
	*cfg = updated
	return nil
}

// BleStateText words the device's Bluetooth state for the status line. The
// advertising case names the device so the user knows what to look for in
// their phone's Bluetooth settings.
func BleStateText(state protocol.BleState) string {
	switch state.State {
	case protocol.BleConnected:
		return fmt.Sprintf("Connected (%d paired)", state.BondCount)
	case protocol.BleAdvertising:
		if state.BondCount == 0 {
			return "Advertising — pair the phone with \"ESP-HID-ME\""
		}
		return fmt.Sprintf("Advertising — waiting for phone (%d paired)", state.BondCount)
	case protocol.BleIdle:
		return "Bluetooth starting…"
	default:
		return "Unknown"
	}
}

// FirmwareText renders the device's HELLO for the status line.
func FirmwareText(hello protocol.Hello) string {
	return fmt.Sprintf("%d.%d.%d (protocol v%d)",
		hello.FwMajor, hello.FwMinor, hello.FwPatch, hello.ProtoVersion)
}

// IndexOf returns the position of value in values, or -1.
func IndexOf(values []string, value string) int {
	for i, v := range values {
		if strings.EqualFold(v, value) {
			return i
		}
	}
	return -1
}
