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
	"esp-hid/host/internal/devicedb"
	"esp-hid/host/internal/hotkey"
	"esp-hid/host/internal/protocol"
)

// SlaveResolutionChoices seeds the resolution picker. The list is editable in
// both GUIs, so an unlisted resolution can still be typed in.
var SlaveResolutionChoices = []string{
	"1280x720", "1366x768", "1600x900", "1920x1080", "2560x1440", "3840x2160",
	"720x1280", "768x1366", "900x1600", "1080x1920", "1440x2560", "2160x3840",
}

// HostSideChoices is the persisted vocabulary for where the *host* sits
// relative to the device — "left" means this computer is to the left of the
// phone, so the crossing edge is the computer's right border. Order is
// load-bearing: the GUIs address these by index.
var HostSideChoices = []string{
	config.HostSideLeft, config.HostSideRight, config.HostSideTop, config.HostSideBottom,
}

// OppositeSide converts between the two ways of naming the same layout: the
// side the host is on (what is saved) and the side the device is on (what
// the arrangement picture shows). A phone drawn to the left of the displays
// puts the host on the right.
func OppositeSide(side string) string {
	switch side {
	case config.HostSideLeft:
		return config.HostSideRight
	case config.HostSideRight:
		return config.HostSideLeft
	case config.HostSideTop:
		return config.HostSideBottom
	case config.HostSideBottom:
		return config.HostSideTop
	}
	return side
}

// OrientationChoices is the Portrait/Landscape picker, addressed by index
// like HostSideChoices. Orientation is not a setting of its own: it is read
// off the resolution (portrait when width <= height) and flipping it swaps
// the two numbers. The resolution stays the single thing that is saved.
var OrientationChoices = []string{"Portrait", "Landscape"}

const (
	OrientationPortrait  = 0
	OrientationLandscape = 1
)

// MaxDeviceMatches caps the device picker's dropdown; past this the user is
// better served by typing another word than by scrolling.
const MaxDeviceMatches = 50

// ResolutionHint sits under the resolution field in both GUIs. The device
// table is a starting point: a phone can render below its panel size, and
// then the panel size is the wrong number.
const ResolutionHint = "Panel pixels — if the phone renders lower (e.g. FHD+ on a QHD+ Samsung), enter that instead."

// DeviceMatch is one row of the device picker's list. Label is what the row
// shows; Name is what goes back into the search field once it is picked, so
// the field reads "Samsung Galaxy S24 Ultra" rather than the label with its
// size in brackets.
type DeviceMatch struct {
	Label      string
	Name       string
	Resolution string
	DPI        int
}

// DefaultDeviceDPI stands in for a device whose density nothing knows: a
// resolution typed by hand that matches no table entry. Phones cluster
// around it; a tablet drawn with it comes out somewhat small, which is the
// harmless direction to be wrong in.
const DefaultDeviceDPI = 420

// DeviceDensity is the pixel density the arrangement picture draws a
// resolution at. Only the resolution is saved, so this is looked up from the
// table each time rather than remembered from a pick — a relaunch then shows
// the same size as the session that set it.
func DeviceDensity(resolution string) int {
	width, height, err := config.ParseResolution(resolution)
	if err != nil {
		return DefaultDeviceDPI
	}
	if dpi := devicedb.DensityFor(width, height); dpi > 0 {
		return dpi
	}
	return DefaultDeviceDPI
}

// DeviceMatches runs the picker search. Empty input yields nothing, so the
// dropdown is empty until the user types.
func DeviceMatches(query string) []DeviceMatch {
	devices := devicedb.Search(query, MaxDeviceMatches)
	matches := make([]DeviceMatch, 0, len(devices))
	for _, d := range devices {
		matches = append(matches, DeviceMatch{
			Label:      d.Label(),
			Name:       d.Brand + " " + d.Name,
			Resolution: d.Resolution(),
			DPI:        d.DPI,
		})
	}
	return matches
}

// OrientationIndexOf reads the orientation off a resolution string, or -1 if
// it does not parse. A square counts as portrait so the toggle always shows
// one of its two states for valid input.
func OrientationIndexOf(resolution string) int {
	width, height, err := config.ParseResolution(resolution)
	if err != nil {
		return -1
	}
	if width > height {
		return OrientationLandscape
	}
	return OrientationPortrait
}

// OrientResolution rewrites resolution to the given orientation, swapping the
// two numbers if its shape disagrees. The second result is false when the
// text does not parse or the index is not one of OrientationChoices; the
// caller should then leave the field alone rather than clobber what the user
// is typing.
func OrientResolution(resolution string, orientation int) (string, bool) {
	width, height, err := config.ParseResolution(resolution)
	if err != nil {
		return resolution, false
	}
	switch orientation {
	case OrientationPortrait:
		if width > height {
			width, height = height, width
		}
	case OrientationLandscape:
		if width < height {
			width, height = height, width
		}
	default:
		return resolution, false
	}
	return fmt.Sprintf("%dx%d", width, height), true
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
	EdgeAnyDisplay  bool
	EdgePush        bool
	EdgePushForce   string
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
		EdgeAnyDisplay:  cfg.EdgeAnyDisplay,
		EdgePush:        cfg.EdgePush,
		EdgePushForce:   strconv.Itoa(cfg.EdgePushForce),
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

	force, err := strconv.Atoi(strings.TrimSpace(f.EdgePushForce))
	if err != nil || force < config.MinEdgePushForce || force > config.MaxEdgePushForce {
		return fmt.Errorf("push force must be a number between %d and %d", config.MinEdgePushForce, config.MaxEdgePushForce)
	}
	updated.EdgePushForce = force

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
	updated.EdgeAnyDisplay = f.EdgeAnyDisplay
	updated.EdgePush = f.EdgePush

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

// DeviceText words the connected bridge for the status line. The serial number
// leads because it is the board's identity — the one thing that tells the user
// which of their ESP32s this is. The port name follows as the incidental
// detail it is: it changes whenever the board moves to another USB socket.
func DeviceText(serial, port string) string {
	short := port
	if i := strings.LastIndexByte(short, '/'); i >= 0 {
		short = short[i+1:]
	}
	switch {
	case serial == "" && short == "":
		return "-"
	case serial == "":
		return short
	case short == "":
		return serial
	default:
		return serial + " · " + short
	}
}

// FirmwareText renders the device's HELLO for the status line.
func FirmwareText(hello protocol.Hello) string {
	return fmt.Sprintf("%d.%d.%d (protocol v%d)",
		hello.FwMajor, hello.FwMinor, hello.FwPatch, hello.ProtoVersion)
}

// VersionText words the build's version for the window footer: a release
// tag without its "v", or the bare build string for anything else.
func VersionText(version string) string {
	return "ESP HID Bridge " + strings.TrimPrefix(version, "v")
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
