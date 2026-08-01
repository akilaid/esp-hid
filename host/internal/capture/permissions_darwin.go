//go:build darwin

package capture

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework CoreGraphics -framework Carbon
#include "capture_darwin.h"
*/
import "C"

import "fmt"

// Permissions reports which macOS input privileges the process currently
// holds. Both are needed for a complete bridge and they are granted
// separately, which is the usual cause of "the mouse works but typing does
// nothing": Accessibility alone lets the tap exist, but keyboard events are
// withheld until Input Monitoring is granted too.
type Permissions struct {
	// Accessibility allows creating an actively filtering event tap — the
	// kind that can swallow input. Without it no capture is possible at all.
	Accessibility bool
	// InputMonitoring allows observing keyboard events (macOS 10.15+).
	InputMonitoring bool
}

// OK reports whether every privilege the given configuration needs is held.
// Keyboard forwarding is the only thing that needs Input Monitoring, so a
// mouse-only setup is not blocked on it.
func (p Permissions) OK(captureKeyboard bool) bool {
	if !p.Accessibility {
		return false
	}
	return p.InputMonitoring || !captureKeyboard
}

// CheckPermissions reads the current grants without prompting, so it is safe
// to poll and safe to call at startup.
func CheckPermissions() Permissions {
	return Permissions{
		Accessibility:   C.ehbHasAccessibility(0) != 0,
		InputMonitoring: C.ehbHasInputMonitoring(0) != 0,
	}
}

// RequestPermissions asks macOS to show the system permission dialogs for
// anything not already granted, then reports the state. The dialogs are
// asynchronous — the user has to visit System Settings and, for
// Accessibility, usually relaunch the app — so the returned value normally
// still shows the privilege as missing.
func RequestPermissions() Permissions {
	current := CheckPermissions()
	if !current.Accessibility {
		C.ehbHasAccessibility(1)
	}
	if !current.InputMonitoring {
		C.ehbHasInputMonitoring(1)
	}
	return CheckPermissions()
}

// SecureInputEnabled reports whether some application has turned on Secure
// Event Input — macOS does this while a password field has focus. It blocks
// keyboard events from reaching every event tap, so the bridge keeps
// forwarding the mouse while typing silently stops working. Surfacing it
// turns a baffling symptom into an explainable one.
func SecureInputEnabled() bool {
	return C.ehbSecureInputEnabled() != 0
}

// PermissionHint describes what is missing, for display to the user.
func (p Permissions) PermissionHint(captureKeyboard bool) string {
	switch {
	case !p.Accessibility && (!p.InputMonitoring && captureKeyboard):
		return "Accessibility and Input Monitoring permission are required"
	case !p.Accessibility:
		return "Accessibility permission is required"
	case !p.InputMonitoring && captureKeyboard:
		return "Input Monitoring permission is required to forward the keyboard"
	default:
		return ""
	}
}

func checkCapturePermissions(captureKeyboard bool) error {
	current := CheckPermissions()
	if current.OK(captureKeyboard) {
		return nil
	}
	return fmt.Errorf("%s (System Settings > Privacy & Security): %w",
		current.PermissionHint(captureKeyboard), ErrPermissionDenied)
}
