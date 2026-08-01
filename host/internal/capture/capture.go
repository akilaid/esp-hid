// Package capture owns the OS input hooks and the remote-mode state machine.
//
// Each supported platform provides a Run implementation against the same
// contract — capture_windows.go (low-level hooks) and capture_darwin.go
// (CGEventTap). Everything they share lives in the untagged files: this one
// (the event/option types) and geometry.go (the edge-activation probe, the
// dead-reckoned slave cursor, and the return-pressure model).
//
// The state machine is the interesting part, and it is identical on both
// platforms: remote mode may be entered by hotkey or by pushing the cursor
// into the host-side screen edge, and left by hotkey, by pushing the virtual
// slave cursor past its far edge, or by an optional left-swipe gesture. If
// the link drops, the callback force-exits remote mode and restores the
// cursor — you can never be trapped controlling a device you cannot reach.
package capture

// EventKind discriminates capture events.
type EventKind int

const (
	EventMouseDelta EventKind = iota
	EventButtonDown
	EventButtonUp
	EventScroll
	EventKeyDown
	EventKeyUp
	EventRemoteMode
)

// Event is one captured input event.
type Event struct {
	Kind    EventKind
	DX, DY  int  // EventMouseDelta
	Button  byte // EventButtonDown/Up: protocol.Button* mask bit
	ScrollV int  // EventScroll
	ScrollH int
	Usage   byte   // EventKeyDown/Up: HID usage
	Active  bool   // EventRemoteMode
	Source  string // EventRemoteMode: hotkey|edge|slave_edge|serial
}

// Options configures the hook state machine.
type Options struct {
	CaptureKeyboard bool
	ToggleHotkey    string // combo name; falls back to hotkey.DefaultName
	LeftwardReturn  bool
	SlaveWidth      int
	SlaveHeight     int
	HostSide        string
	AutoSwitch      bool

	// DebugStallCapture makes the first captured event sleep long enough for
	// the OS to disable the tap/hook, so the recovery path can be exercised
	// deliberately instead of only under real load. Never set in production.
	DebugStallCapture bool
}

// Host side names (shared vocabulary with config).
const (
	HostSideLeft   = "left"
	HostSideRight  = "right"
	HostSideTop    = "top"
	HostSideBottom = "bottom"
)

// publish is deliberately lossy: the hook/tap callback runs synchronously
// inside the OS input path, so it must never block on a slow consumer.
func publish(out chan<- Event, event Event) {
	select {
	case out <- event:
	default:
	}
}
