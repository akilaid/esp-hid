//go:build darwin && capture_integration

package capture

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"esp-hid/host/internal/keymap"
	"esp-hid/host/internal/protocol"
)

// macOS key codes used by the test.
const (
	testKeyF9        = 0x65
	testKeyF19       = 0x50
	testKeyLeftShift = 0x38
)

// TestIntegrationF19Toggle checks a function key above F12 works as the
// toggle. F13..F20 exist on full-size Apple keyboards but the combo grammar
// used to stop at F12, so binding F19 silently fell back to the default.
//
// Note this has to drive the tap from inside the test process: macOS only
// lets a process post synthetic *keystrokes* if it is itself trusted, so a
// separate helper binary cannot stand in here (synthetic mouse events are
// not restricted the same way).
func TestIntegrationF19Toggle(t *testing.T) {
	if os.Getenv("ESP_HID_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set ESP_HID_CAPTURE_INTEGRATION=1 to run (briefly grabs system input)")
	}
	if perms := CheckPermissions(); !perms.OK(true) {
		t.Skipf("missing permissions: %s", perms.PermissionHint(true))
	}

	events := make(chan Event, 256)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Options{
			CaptureKeyboard: true,
			ToggleHotkey:    "F19",
			SlaveWidth:      1920,
			SlaveHeight:     1080,
			HostSide:        HostSideLeft,
			AutoSwitch:      false,
		}, events, func() bool { return true })
	}()
	time.Sleep(500 * time.Millisecond)

	syntheticKey(testKeyF19, true)
	syntheticKey(testKeyF19, false)
	time.Sleep(300 * time.Millisecond)
	syntheticKey(testKeyF19, true)
	syntheticKey(testKeyF19, false)
	time.Sleep(300 * time.Millisecond)

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	close(events)

	var on, off bool
	for event := range events {
		if event.Kind != EventRemoteMode || event.Source != "hotkey" {
			continue
		}
		if event.Active {
			on = true
		} else if on {
			off = true
		}
	}
	if !on {
		t.Error("F19 did not activate remote mode")
	}
	if !off {
		t.Error("a second F19 press did not deactivate remote mode")
	}
}

// TestIntegrationEdgeEntryPersists checks that reaching the host-side screen
// edge activates remote mode and that it stays activated.
//
// Motivation: entering remote mode used to warp the cursor to the middle of
// the monitor, and a warp emits a motion event whose delta is the size of the
// jump. Coming in from a screen edge, that jump points straight back at the
// edge just crossed — enough to trip the return-pressure model immediately, so
// remote mode switched on and back off within milliseconds and edge switching
// appeared not to work at all.
//
// Honest limitation: this test does NOT reproduce that bug. Re-introducing the
// warp leaves it passing, because a single synthetic absolute-position event
// does not put the window server in the same state a real mouse sweep does;
// the fault was found and confirmed by driving the built binary by hand. What
// this test does buy is a guard against edge entry breaking outright, or
// against it bouncing straight back out for some other reason.
//
// Host side "right" is used because it puts the entry edge at x<=1, which is
// reachable regardless of display resolution.
//
// See also TestIntegrationEdgeTouchAloneDoesNotEnter, which asserts the other
// half of the contract: that merely reaching the edge does nothing.
func TestIntegrationEdgeEntryPersists(t *testing.T) {
	if os.Getenv("ESP_HID_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set ESP_HID_CAPTURE_INTEGRATION=1 to run (briefly grabs system input)")
	}
	if perms := CheckPermissions(); !perms.OK(true) {
		t.Skipf("missing permissions: %s", perms.PermissionHint(true))
	}

	events := make(chan Event, 512)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Options{
			CaptureKeyboard: true,
			ToggleHotkey:    "F9",
			SlaveWidth:      1080,
			SlaveHeight:     1920,
			HostSide:        HostSideRight,
			AutoSwitch:      true,
		}, events, func() bool { return true })
	}()
	time.Sleep(500 * time.Millisecond)

	// Sweep leftward into the entry edge.
	syntheticMouseMove(-5, 0)
	time.Sleep(100 * time.Millisecond)

	// Arriving is not enough any more: entry is armed by pushing against the
	// border, so this has to keep shoving outward the way a real hand would.
	// One touch deliberately does nothing — that is the whole point of the
	// gate, and TestIntegrationEdgeTouchAloneDoesNotEnter covers it.
	steps := (edgeEntryPressureThreshold / 40) + 2
	for i := 0; i < steps; i++ {
		syntheticMouseMoveTo(0, 400, -40)
		time.Sleep(10 * time.Millisecond)
	}
	// Hold: any spurious motion in this window would trip the return.
	time.Sleep(1200 * time.Millisecond)

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	close(events)

	var activatedAt, deactivatedAt int = -1, -1
	index := 0
	for event := range events {
		if event.Kind == EventRemoteMode {
			if event.Active && activatedAt < 0 && event.Source == "edge" {
				activatedAt = index
			} else if !event.Active && activatedAt >= 0 && deactivatedAt < 0 {
				deactivatedAt = index
			}
		}
		index++
	}

	if activatedAt < 0 {
		t.Fatal("reaching the host-side edge did not activate remote mode")
	}
	if deactivatedAt >= 0 {
		t.Errorf("remote mode deactivated on its own %d events after entry; "+
			"entering must not emit motion that trips the return-pressure model",
			deactivatedAt-activatedAt)
	}
}

// TestIntegrationEdgeTouchAloneDoesNotEnter is the reason edge switching is
// offered on macOS at all. A single-display Mac puts the Dock, the menu bar and
// every window's close button on the same borders remote mode would cross, so
// arriving at one must not be a crossing — only pushing against it is.
//
// The pointer is placed on the entry edge with a delta far below the pressure
// threshold, several times over, with gaps longer than the pressure window so
// nothing accumulates between them. That is what reaching for something at the
// edge of the screen looks like.
func TestIntegrationEdgeTouchAloneDoesNotEnter(t *testing.T) {
	if os.Getenv("ESP_HID_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set ESP_HID_CAPTURE_INTEGRATION=1 to run (briefly grabs system input)")
	}
	if perms := CheckPermissions(); !perms.OK(true) {
		t.Skipf("missing permissions: %s", perms.PermissionHint(true))
	}

	events := make(chan Event, 512)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Options{
			CaptureKeyboard: true,
			ToggleHotkey:    "F9",
			SlaveWidth:      1080,
			SlaveHeight:     1920,
			HostSide:        HostSideRight,
			AutoSwitch:      true,
		}, events, func() bool { return true })
	}()
	time.Sleep(500 * time.Millisecond)

	for i := 0; i < 4; i++ {
		syntheticMouseMoveTo(0, 300+float64(i)*40, -8)
		time.Sleep(edgeEntryPressureWindow + 100*time.Millisecond)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	close(events)

	for event := range events {
		if event.Kind == EventRemoteMode && event.Active && event.Source == "edge" {
			t.Fatal("touching the edge without pushing against it entered remote mode")
		}
	}
}

// TestIntegrationCaptureForwardsRealEvents drives the real system event tap.
// It needs Accessibility and Input Monitoring, and it *swallows the machine's
// input* for the second or so that remote mode is engaged — unacceptable in
// CI and surprising on a desktop. So it is gated twice: behind the
// capture_integration build tag, and behind an environment variable.
//
//	ESP_HID_CAPTURE_INTEGRATION=1 go test -tags capture_integration \
//	    ./internal/capture/ -run Integration -v
//
// What it covers cannot be verified any other way: that the tap actually
// receives events, and that the scroll signs, button numbers, and modifier
// reconciliation match what the firmware expects.
func TestIntegrationCaptureForwardsRealEvents(t *testing.T) {
	if os.Getenv("ESP_HID_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set ESP_HID_CAPTURE_INTEGRATION=1 to run (briefly grabs system input)")
	}
	if perms := CheckPermissions(); !perms.OK(true) {
		t.Skipf("missing permissions: %s", perms.PermissionHint(true))
	}

	events := make(chan Event, 512)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Options{
			CaptureKeyboard: true,
			ToggleHotkey:    "F9",
			SlaveWidth:      1920,
			SlaveHeight:     1080,
			HostSide:        HostSideLeft,
			AutoSwitch:      false, // never grab input from a stray screen-edge touch
		}, events, func() bool { return true })
	}()

	// Let the tap install before anything is posted at it.
	time.Sleep(500 * time.Millisecond)

	syntheticKey(testKeyF9, true)
	syntheticKey(testKeyF9, false)
	time.Sleep(200 * time.Millisecond)

	syntheticMouseMove(17, -9)
	syntheticScrollLines(3, 0)
	syntheticScrollLines(0, 2)
	syntheticLeftButton(true)
	syntheticLeftButton(false)
	syntheticOtherButton(2, true) // middle
	syntheticOtherButton(2, false)
	syntheticFlagsChanged(testKeyLeftShift, keymap.FlagMaskShift|0x00000002)
	syntheticFlagsChanged(testKeyLeftShift, 0)
	time.Sleep(400 * time.Millisecond)

	// Leave remote mode no matter what the assertions below decide.
	syntheticKey(testKeyF9, true)
	syntheticKey(testKeyF9, false)
	time.Sleep(250 * time.Millisecond)

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	close(events)

	var collected []Event
	for event := range events {
		collected = append(collected, event)
	}
	if len(collected) == 0 {
		t.Fatal("no events captured — the tap received nothing")
	}
	t.Logf("captured %d events", len(collected))

	find := func(match func(Event) bool) (Event, bool) {
		for _, event := range collected {
			if match(event) {
				return event, true
			}
		}
		return Event{}, false
	}

	if _, ok := find(func(e Event) bool {
		return e.Kind == EventRemoteMode && e.Active && e.Source == "hotkey"
	}); !ok {
		t.Error("the toggle hotkey did not activate remote mode")
	}
	if _, ok := find(func(e Event) bool { return e.Kind == EventRemoteMode && !e.Active }); !ok {
		t.Error("remote mode never deactivated")
	}

	// Real mouse motion can interleave with the synthetic event, so look for
	// the posted delta anywhere in the stream rather than assuming it is
	// first. Matching it exactly is what proves deltas pass through unscaled.
	if _, ok := find(func(e Event) bool { return e.Kind == EventMouseDelta }); !ok {
		t.Error("no mouse delta captured")
	} else if _, ok := find(func(e Event) bool {
		return e.Kind == EventMouseDelta && e.DX == 17 && e.DY == -9
	}); !ok {
		var deltas []string
		for _, e := range collected {
			if e.Kind == EventMouseDelta {
				deltas = append(deltas, fmt.Sprintf("(%d,%d)", e.DX, e.DY))
			}
		}
		t.Errorf("posted delta (17,-9) never arrived; captured deltas: %s", strings.Join(deltas, " "))
	}

	// Vertical scroll shares HID's sign convention; horizontal does not.
	if scroll, ok := find(func(e Event) bool { return e.Kind == EventScroll && e.ScrollV != 0 }); !ok {
		t.Error("no vertical scroll captured")
	} else if scroll.ScrollV != 3 {
		t.Errorf("vertical scroll = %d, want 3 (positive = up on both sides)", scroll.ScrollV)
	}
	if scroll, ok := find(func(e Event) bool { return e.Kind == EventScroll && e.ScrollH != 0 }); !ok {
		t.Error("no horizontal scroll captured")
	} else if scroll.ScrollH != -2 {
		t.Errorf("horizontal scroll = %d, want -2 (macOS positive = left, HID AC Pan positive = right)", scroll.ScrollH)
	}

	if _, ok := find(func(e Event) bool {
		return e.Kind == EventButtonDown && e.Button == protocol.ButtonLeft
	}); !ok {
		t.Error("left button down not captured")
	}
	if _, ok := find(func(e Event) bool {
		return e.Kind == EventButtonDown && e.Button == protocol.ButtonMiddle
	}); !ok {
		t.Error("middle button (button number 2) not captured")
	}

	// The whole point of the flagsChanged path: modifiers must become real
	// key events, which the legacy macOS app never managed at all.
	if _, ok := find(func(e Event) bool {
		return e.Kind == EventKeyDown && e.Usage == keymap.UsageLeftShift
	}); !ok {
		t.Error("left shift press was not forwarded as a HID modifier usage")
	}
	if _, ok := find(func(e Event) bool {
		return e.Kind == EventKeyUp && e.Usage == keymap.UsageLeftShift
	}); !ok {
		t.Error("left shift release was not forwarded")
	}
}
