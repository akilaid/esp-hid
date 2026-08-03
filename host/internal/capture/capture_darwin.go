//go:build darwin

// macOS input capture: one session-wide CGEventTap driving the shared
// remote-mode state machine in geometry.go. The edge-activation probe,
// debounce, virtual slave cursor, return-pressure model, and the link-drop
// force-exit invariant behave exactly as they do on Windows.
//
// Three things are done deliberately differently from the legacy
// software-macos implementation, each fixing a real defect:
//
//   - The tap is re-enabled when macOS disables it. An active tap whose
//     callback is slow gets switched off by the window server; the legacy
//     code never noticed, so capture died silently and permanently under
//     load.
//   - Motion is read from the event's own deltas after decoupling the cursor
//     from the mouse, instead of warping to an anchor on every move. Warping
//     suppresses local mouse events for roughly a quarter second afterwards,
//     which made sustained movement stutter.
//   - Modifier keys are forwarded. macOS reports them as flagsChanged rather
//     than key down/up, so the legacy key path never saw them and Shift,
//     Ctrl, Alt and Cmd simply never reached the slave.
package capture

/*
#cgo CFLAGS: -Wall
// AppKit is here only for capture_focus_darwin.m, which needs
// NSRunningApplication to hold the foreground while remote mode is engaged.
// It builds no UI; the AppKit that draws things stays in internal/ui.
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework CoreGraphics -framework Carbon -framework AppKit
#include "capture_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"runtime/cgo"
	"time"
	"unsafe"

	"esp-hid/host/internal/hotkey"
	"esp-hid/host/internal/keymap"
	"esp-hid/host/internal/protocol"
)

const (
	maxDisplays = 32

	// Trackpad and Magic Mouse scrolling arrives as continuous point deltas.
	// Roughly ten points is one notch of a physical wheel.
	trackpadPointsPerStep = 10.0

	// How long -debug-stall-capture blocks the callback. The window server
	// disables a tap whose callback overruns about a second.
	debugStallDuration = 3 * time.Second
)

// session holds every piece of mutable capture state. It is created before
// the tap and touched only from the tap callback, which the run loop
// serializes onto this one locked thread — so none of it needs locking.
type session struct {
	opts              Options
	out               chan<- Event
	activationAllowed func() bool

	toggleKey  uint32
	toggleMods uint32
	hostSide   string

	tap C.CFMachPortRef

	slaveCursor *virtualCursor
	leftward    *leftwardReturnTracker

	remoteModeActive bool
	cursorHidden     bool
	edgeArmed        bool
	hotkeyDown       bool
	remoteAnchor     point
	monitorRects     []monitorRect

	// Shadow of what the slave believes is held, indexed like
	// keymap.Modifiers. macOS reports modifier state, not transitions, so
	// this is what turns one into the other.
	modDown [8]bool

	scrollAccV float64
	scrollAccH float64

	stalled      bool
	tapReenables int
}

// Run installs the event tap and pumps its run loop until ctx is canceled.
// It must own its OS thread. activationAllowedFn gates remote-mode entry AND
// forces an exit when it turns false mid-session (the "never trapped on a
// dead link" invariant) — pass the serial-up && BLE-connected condition.
func Run(ctx context.Context, opts Options, out chan<- Event, activationAllowedFn func() bool) error {
	// CFRunLoopGetCurrent is per-thread, and the run loop must be driven from
	// the same thread that added the tap source. AppKit owns the main thread;
	// this deliberately is not it, so a modal AppKit loop can never stall the
	// tap callback and get the tap disabled.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := checkCapturePermissions(opts.CaptureKeyboard); err != nil {
		return err
	}

	toggleKey, toggleMods := hotkey.ParseDarwin(opts.ToggleHotkey)
	if toggleKey == 0 {
		toggleKey, toggleMods = hotkey.ParseDarwin(hotkey.DefaultName)
	}

	hostSide := normalizeHostSide(opts.HostSide)
	sess := &session{
		opts:              opts,
		out:               out,
		activationAllowed: activationAllowedFn,
		toggleKey:         toggleKey,
		toggleMods:        toggleMods,
		hostSide:          hostSide,
		slaveCursor:       newVirtualCursor(opts.SlaveWidth, opts.SlaveHeight, hostSide),
		leftward:          &leftwardReturnTracker{enabled: opts.LeftwardReturn, hostSide: hostSide},
		edgeArmed:         true,
	}
	if sess.activationAllowed == nil {
		sess.activationAllowed = func() bool { return true }
	}
	sess.refreshMonitorRects()
	if cursor, ok := sess.cursorPoint(); ok {
		sess.setAnchorForPoint(cursor)
	} else {
		sess.remoteAnchor = sess.virtualDesktopRect().centerPoint()
	}
	sess.slaveCursor.resetForActivation("hotkey")

	handle := cgo.NewHandle(sess)
	defer handle.Delete()

	tap := C.ehbTapCreate(C.uintptr_t(handle))
	if C.ehbMachPortIsNull(tap) != 0 {
		return fmt.Errorf("CGEventTapCreate failed — grant Accessibility in "+
			"System Settings > Privacy & Security: %w", ErrPermissionDenied)
	}
	sess.tap = tap
	defer C.ehbReleaseMachPort(tap)

	source := C.ehbRunLoopSourceCreate(tap)
	if C.ehbRunLoopSourceIsNull(source) != 0 {
		return errors.New("CFMachPortCreateRunLoopSource failed")
	}
	defer C.ehbReleaseRunLoopSource(source)

	C.ehbRunLoopAddSource(source)
	defer C.ehbRunLoopRemoveSource(source)

	C.ehbTapEnable(tap, 1)
	defer C.ehbTapEnable(tap, 0)

	watchdog := C.ehbWatchdogCreate(tap)
	defer C.ehbWatchdogInvalidate(watchdog)

	// Declared last so it runs first: whatever happens, the user gets their
	// cursor back and the mouse reconnected to it.
	defer sess.restoreCursor()

	runLoop := C.ehbRunLoopCurrent()
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			C.ehbRunLoopStop(runLoop)
		case <-stop:
		}
	}()
	defer close(stop)

	C.ehbRunLoopRun()
	return nil
}

//export goCaptureTapCallback
func goCaptureTapCallback(_ C.CGEventTapProxy, eventType C.CGEventType, event C.CGEventRef, userInfo unsafe.Pointer) C.CGEventRef {
	if userInfo == nil {
		return event
	}
	sess, ok := cgo.Handle(uintptr(userInfo)).Value().(*session)
	if !ok || sess == nil {
		return event
	}
	return sess.handleEvent(eventType, event)
}

func (s *session) handleEvent(eventType C.CGEventType, event C.CGEventRef) C.CGEventRef {
	switch uint32(eventType) {
	case uint32(C.kCGEventTapDisabledByTimeout), uint32(C.kCGEventTapDisabledByUserInput):
		// The window server switched us off, either because a callback ran
		// long or because of user input policy. Without this, capture is
		// dead until the process restarts.
		C.ehbTapEnable(s.tap, 1)
		s.tapReenables++
		log.Printf("capture: event tap disabled by the system, re-enabled (%d so far)", s.tapReenables)
		return event
	}

	s.maybeStall()
	s.disableRemoteIfDisconnected()

	switch uint32(eventType) {
	case uint32(C.kCGEventMouseMoved),
		uint32(C.kCGEventLeftMouseDragged),
		uint32(C.kCGEventRightMouseDragged),
		uint32(C.kCGEventOtherMouseDragged):
		return s.handleMouseMove(event)
	case uint32(C.kCGEventLeftMouseDown):
		return s.handleButton(protocol.ButtonLeft, true, event)
	case uint32(C.kCGEventLeftMouseUp):
		return s.handleButton(protocol.ButtonLeft, false, event)
	case uint32(C.kCGEventRightMouseDown):
		return s.handleButton(protocol.ButtonRight, true, event)
	case uint32(C.kCGEventRightMouseUp):
		return s.handleButton(protocol.ButtonRight, false, event)
	case uint32(C.kCGEventOtherMouseDown):
		return s.handleOtherButton(event, true)
	case uint32(C.kCGEventOtherMouseUp):
		return s.handleOtherButton(event, false)
	case uint32(C.kCGEventScrollWheel):
		return s.handleScroll(event)
	case uint32(C.kCGEventKeyDown):
		return s.handleKey(event, true)
	case uint32(C.kCGEventKeyUp):
		return s.handleKey(event, false)
	case uint32(C.kCGEventFlagsChanged):
		return s.handleFlagsChanged(event)
	}
	return event
}

// maybeStall exists so tap-disable recovery can be exercised on demand
// instead of only discovered in the field under load.
func (s *session) maybeStall() {
	if !s.opts.DebugStallCapture || s.stalled {
		return
	}
	s.stalled = true
	log.Printf("capture: -debug-stall-capture holding the callback for %s to force a tap disable", debugStallDuration)
	time.Sleep(debugStallDuration)
}

// The invariant: at the top of every event, if the link is gone, warp home
// and exit remote mode. You can never be trapped controlling a device the
// link cannot reach.
func (s *session) disableRemoteIfDisconnected() {
	if !s.remoteModeActive || s.activationAllowed() {
		return
	}
	s.exitRemote(s.returnToHostPointForAnchor(s.remoteAnchor), "serial")
	s.edgeArmed = true
	s.leftward.reset()
	s.slaveCursor.resetPressure()
}

func (s *session) handleMouseMove(event C.CGEventRef) C.CGEventRef {
	if !s.remoteModeActive {
		location := s.eventPoint(event)
		switch {
		case !s.activationAllowed():
			s.edgeArmed = true
			s.leftward.reset()
			s.slaveCursor.resetPressure()
		case s.opts.AutoSwitch && s.canActivateFromHostEdge(location):
			if s.edgeArmed {
				s.setAnchorForPoint(location)
				s.slaveCursor.resetForActivation("edge")
				s.setRemoteMode(true, "edge")
				s.edgeArmed = false
				s.leftward.reset()
				s.slaveCursor.resetPressure()
				return C.ehbNullEvent()
			}
		default:
			s.edgeArmed = true
		}
		return event
	}

	// The cursor is decoupled from the mouse here, so the event's absolute
	// location is meaningless; its deltas are the real motion.
	dx := int(C.ehbEventDeltaX(event))
	dy := int(C.ehbEventDeltaY(event))

	shouldReturn := s.slaveCursor.move(dx, dy)
	if !shouldReturn {
		shouldReturn = s.leftward.update(dx, dy, time.Now())
	}
	if shouldReturn {
		s.exitRemote(s.returnToHostPointForAnchor(s.remoteAnchor), "slave_edge")
		s.edgeArmed = false
		s.leftward.reset()
		s.slaveCursor.resetPressure()
		return C.ehbNullEvent()
	}
	if dx != 0 || dy != 0 {
		publish(s.out, Event{Kind: EventMouseDelta, DX: dx, DY: dy})
	}
	return C.ehbNullEvent()
}

func (s *session) handleButton(button byte, down bool, event C.CGEventRef) C.CGEventRef {
	if !s.remoteModeActive {
		return event
	}
	kind := EventButtonUp
	if down {
		kind = EventButtonDown
	}
	publish(s.out, Event{Kind: kind, Button: button})
	return C.ehbNullEvent()
}

func (s *session) handleOtherButton(event C.CGEventRef, down bool) C.CGEventRef {
	if !s.remoteModeActive {
		return event
	}
	button, ok := buttonForNumber(int64(C.ehbEventButtonNumber(event)))
	if !ok {
		// A button the device cannot represent. Still swallowed, so it does
		// not leak to the host while remote mode is active.
		return C.ehbNullEvent()
	}
	return s.handleButton(button, down, event)
}

func buttonForNumber(number int64) (byte, bool) {
	switch number {
	case 2:
		return protocol.ButtonMiddle, true
	case 3:
		return protocol.ButtonBack, true
	case 4:
		return protocol.ButtonForward, true
	}
	return 0, false
}

func (s *session) handleScroll(event C.CGEventRef) C.CGEventRef {
	if !s.remoteModeActive {
		return event
	}

	var vertical, horizontal int
	if C.ehbEventScrollIsContinuous(event) == 0 {
		// A real wheel: these fields are already in wheel notches.
		vertical = int(C.ehbEventScrollLineV(event))
		horizontal = int(C.ehbEventScrollLineH(event))
	} else {
		// A trackpad: accumulate pixel-ish deltas and emit whole notches,
		// keeping the remainder so slow scrolling still registers.
		s.scrollAccV += float64(C.ehbEventScrollPointV(event))
		s.scrollAccH += float64(C.ehbEventScrollPointH(event))
		vertical, s.scrollAccV = drainScrollAccumulator(s.scrollAccV)
		horizontal, s.scrollAccH = drainScrollAccumulator(s.scrollAccH)
	}

	// Vertical agrees with HID (positive = up). Horizontal does not: macOS
	// reports positive for leftward scrolling, HID AC Pan for rightward.
	horizontal = -horizontal

	if vertical != 0 || horizontal != 0 {
		publish(s.out, Event{Kind: EventScroll, ScrollV: vertical, ScrollH: horizontal})
	}
	return C.ehbNullEvent()
}

func drainScrollAccumulator(accumulated float64) (steps int, remainder float64) {
	steps = int(accumulated / trackpadPointsPerStep)
	if steps == 0 {
		return 0, accumulated
	}
	return steps, accumulated - float64(steps)*trackpadPointsPerStep
}

func (s *session) handleKey(event C.CGEventRef, down bool) C.CGEventRef {
	keyCode := uint32(C.ehbEventKeyCode(event))
	mods := modsFromFlags(uint64(C.ehbEventFlags(event)))

	if keyCode == s.toggleKey && mods == s.toggleMods {
		// While the link is down the hotkey is intentionally inert and passes
		// through to the host, so remote mode can never be entered when it
		// would swallow input that goes nowhere.
		consume := s.remoteModeActive || s.activationAllowed()
		if down {
			if !s.hotkeyDown {
				s.hotkeyDown = true
				if consume {
					s.toggleRemoteMode()
				}
			}
		} else {
			s.hotkeyDown = false
		}
		if consume {
			return C.ehbNullEvent()
		}
	}

	if s.remoteModeActive && s.opts.CaptureKeyboard {
		if usage, mapped := keymap.CGKeyCodeToUsage(keyCode); mapped {
			kind := EventKeyUp
			if down {
				kind = EventKeyDown
			}
			publish(s.out, Event{Kind: kind, Usage: usage})
		}
		return C.ehbNullEvent()
	}
	return event
}

func (s *session) toggleRemoteMode() {
	if s.remoteModeActive {
		s.exitRemote(s.returnToHostPointForAnchor(s.remoteAnchor), "hotkey")
	} else {
		if cursor, ok := s.cursorPoint(); ok {
			s.setAnchorForPoint(cursor)
		}
		s.slaveCursor.resetForActivation("hotkey")
		s.setRemoteMode(true, "hotkey")
	}
	s.edgeArmed = false
	s.leftward.reset()
	s.slaveCursor.resetPressure()
}

// handleFlagsChanged turns macOS's modifier *state* reports into the down/up
// *transitions* the HID protocol needs. Reconciling the whole table rather
// than just the key that changed makes it self-healing: if the tap was
// briefly disabled and a release went missing, the next change corrects it.
func (s *session) handleFlagsChanged(event C.CGEventRef) C.CGEventRef {
	if !s.remoteModeActive || !s.opts.CaptureKeyboard {
		return event
	}
	flags := uint64(C.ehbEventFlags(event))
	keyCode := uint32(C.ehbEventKeyCode(event))
	s.reconcileModifiers(flags, keyCode, true)
	return C.ehbNullEvent()
}

func (s *session) reconcileModifiers(flags uint64, keyCode uint32, emit bool) {
	sideKnown := anyDeviceBitSet(flags)
	for i, modifier := range keymap.Modifiers {
		down := flags&modifier.DeviceBit != 0
		// Some virtual and remapped keyboards populate only the side-agnostic
		// mask. Fall back to attributing it to whichever key actually moved.
		if !sideKnown && keyCode == modifier.CGKeyCode && flags&modifier.BasicMask != 0 {
			down = true
		}
		if down == s.modDown[i] {
			continue
		}
		s.modDown[i] = down
		if !emit {
			continue
		}
		kind := EventKeyUp
		if down {
			kind = EventKeyDown
		}
		publish(s.out, Event{Kind: kind, Usage: modifier.Usage})
	}
}

func anyDeviceBitSet(flags uint64) bool {
	for _, modifier := range keymap.Modifiers {
		if flags&modifier.DeviceBit != 0 {
			return true
		}
	}
	return false
}

func modsFromFlags(flags uint64) uint32 {
	var mods uint32
	if flags&keymap.FlagMaskControl != 0 {
		mods |= hotkey.ModCtrl
	}
	if flags&keymap.FlagMaskAlternate != 0 {
		mods |= hotkey.ModAlt
	}
	if flags&keymap.FlagMaskShift != 0 {
		mods |= hotkey.ModShift
	}
	if flags&keymap.FlagMaskCommand != 0 {
		mods |= hotkey.ModWin
	}
	return mods
}

func (s *session) setRemoteMode(active bool, source string) {
	if s.remoteModeActive == active {
		return
	}
	s.remoteModeActive = active
	if active {
		// Decouple the cursor from the mouse so motion keeps arriving as
		// deltas while the pointer itself stays put.
		//
		// Deliberately no warp here. A warp emits a motion event whose delta
		// is the size of the jump, and entering from a screen edge that jump
		// points straight back at the edge you just crossed — enough to trip
		// the return-pressure model on the very next event, so remote mode
		// switched on and off again within milliseconds. Nothing needs the
		// warp: the cursor is hidden for the duration and is repositioned
		// explicitly on exit.
		C.ehbSetMouseAssociation(0)
		if !s.cursorHidden {
			C.ehbHideCursor()
			s.cursorHidden = true
		}
		// Neither of the two calls above is honoured unless this process is the
		// frontmost application, so take the foreground for the duration. It
		// returns immediately and re-asserts the dissociation once activation
		// has actually landed; nothing here may block the tap callback.
		C.ehbFocusGrab()
		// Seed the shadow without emitting: the toggle hotkey itself often
		// holds modifiers, and pressing Ctrl+Alt+F7 to enter must not send
		// Ctrl and Alt to the slave. The later release produces an "up" for a
		// key never marked down, which core.KeyTracker filters out.
		s.reconcileModifiers(uint64(C.ehbCurrentFlags()), 0, false)
	} else {
		// First, and before the re-association below. The generation bump
		// inside is synchronous, which is the point: a grab still polling for
		// activation to land would otherwise be free to re-dissociate the
		// mouse immediately after we re-associated it, leaving a dead pointer.
		// Handing focus back is the queued half and can take its time.
		C.ehbFocusRelease()
		if s.cursorHidden {
			C.ehbShowCursor()
			s.cursorHidden = false
		}
		C.ehbSetMouseAssociation(1)
		// The bridge sends RELEASE_ALL on deactivate, so the slave is clean;
		// zeroing keeps the local shadow honest for the next activation.
		s.modDown = [8]bool{}
		s.scrollAccV, s.scrollAccH = 0, 0
	}
	publish(s.out, Event{Kind: EventRemoteMode, Active: active, Source: source})
}

func (s *session) exitRemote(returnPoint point, source string) {
	C.ehbWarpCursor(C.double(returnPoint.X), C.double(returnPoint.Y))
	s.setRemoteMode(false, source)
}

// restoreCursor is the unconditional teardown. Both cursor hiding and mouse
// dissociation are scoped to this process's window server connection, so a
// crash would undo them anyway — but a clean exit must not rely on that. The
// foreground is handed back too, so stopping the bridge without exiting the
// process does not leave the app sitting in front of whatever the user was
// actually using.
func (s *session) restoreCursor() {
	C.ehbFocusRelease()
	if s.cursorHidden {
		C.ehbShowCursor()
		s.cursorHidden = false
	}
	C.ehbSetMouseAssociation(1)
}

func (s *session) cursorPoint() (point, bool) {
	var x, y C.double
	C.ehbCursorPosition(&x, &y)
	return point{X: int32(x), Y: int32(y)}, true
}

func (s *session) eventPoint(event C.CGEventRef) point {
	return point{
		X: int32(C.ehbEventLocationX(event)),
		Y: int32(C.ehbEventLocationY(event)),
	}
}

func (s *session) refreshMonitorRects() {
	buffer := make([]C.double, maxDisplays*4)
	count := int(C.ehbDisplayBounds(&buffer[0], C.int(maxDisplays)))
	if count <= 0 {
		return
	}
	rects := make([]monitorRect, 0, count)
	for i := 0; i < count; i++ {
		x := float64(buffer[i*4+0])
		y := float64(buffer[i*4+1])
		width := float64(buffer[i*4+2])
		height := float64(buffer[i*4+3])
		if width <= 0 || height <= 0 {
			continue
		}
		rects = append(rects, monitorRect{
			Left:   int32(x),
			Top:    int32(y),
			Right:  int32(x + width),
			Bottom: int32(y + height),
		})
	}
	if len(rects) > 0 {
		s.monitorRects = rects
	}
}

func (s *session) findMonitor(p point) (monitorRect, bool) {
	if rect, found := findMonitorForPoint(p, s.monitorRects); found {
		return rect, true
	}
	// Displays may have been rearranged since the last lookup.
	s.refreshMonitorRects()
	return findMonitorForPoint(p, s.monitorRects)
}

func (s *session) virtualDesktopRect() monitorRect {
	if len(s.monitorRects) == 0 {
		return monitorRect{Right: 1920, Bottom: 1080}
	}
	bounds := s.monitorRects[0]
	for _, rect := range s.monitorRects[1:] {
		if rect.Left < bounds.Left {
			bounds.Left = rect.Left
		}
		if rect.Top < bounds.Top {
			bounds.Top = rect.Top
		}
		if rect.Right > bounds.Right {
			bounds.Right = rect.Right
		}
		if rect.Bottom > bounds.Bottom {
			bounds.Bottom = rect.Bottom
		}
	}
	return bounds
}

func (s *session) canActivateFromHostEdge(p point) bool {
	rect, found := s.findMonitor(p)
	if !found {
		// No known display contains the cursor: fall back to the whole
		// desktop. With no other rects to probe, only its outer border
		// activates.
		return isOuterActivationEdgePoint(p, s.virtualDesktopRect(), nil, s.hostSide)
	}
	return isOuterActivationEdgePoint(p, rect, s.monitorRects, s.hostSide)
}

func (s *session) returnToHostPointForAnchor(current point) point {
	if rect, found := s.findMonitor(s.remoteAnchor); found {
		return returnPointInRect(current, rect, s.hostSide)
	}
	return returnPointInRect(current, s.virtualDesktopRect(), s.hostSide)
}

func (s *session) setAnchorForPoint(p point) {
	if rect, found := s.findMonitor(p); found {
		s.remoteAnchor = rect.centerPoint()
		return
	}
	s.remoteAnchor = s.virtualDesktopRect().centerPoint()
}
