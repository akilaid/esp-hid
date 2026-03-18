//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation -framework ApplicationServices -framework AppKit

#include <ApplicationServices/ApplicationServices.h>
#include <AppKit/AppKit.h>
#include <stdlib.h>

// Forward declaration of the Go callback.
extern CGEventRef goHookEventCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *userInfo);

// Saved run loop reference for stopping from another goroutine.
static CFRunLoopRef gHookRunLoop = NULL;

static CFMachPortRef installHookEventTap(void) {
    CGEventMask mask =
        CGEventMaskBit(kCGEventMouseMoved)         |
        CGEventMaskBit(kCGEventLeftMouseDown)      |
        CGEventMaskBit(kCGEventLeftMouseUp)        |
        CGEventMaskBit(kCGEventRightMouseDown)     |
        CGEventMaskBit(kCGEventRightMouseUp)       |
        CGEventMaskBit(kCGEventScrollWheel)        |
        CGEventMaskBit(kCGEventKeyDown)            |
        CGEventMaskBit(kCGEventKeyUp)              |
        CGEventMaskBit(kCGEventFlagsChanged)       |
        CGEventMaskBit(kCGEventLeftMouseDragged)   |
        CGEventMaskBit(kCGEventRightMouseDragged);

    return CGEventTapCreate(
        kCGSessionEventTap,
        kCGHeadInsertEventTap,
        kCGEventTapOptionDefault,
        mask,
        goHookEventCallback,
        NULL);
}

static void saveHookRunLoop(void) {
    gHookRunLoop = CFRunLoopGetCurrent();
    CFRetain(gHookRunLoop);
}

static void stopHookRunLoop(void) {
    if (gHookRunLoop) {
        CFRunLoopStop(gHookRunLoop);
        CFRelease(gHookRunLoop);
        gHookRunLoop = NULL;
    }
}

static void runHookRunLoop(void) {
    CFRunLoopRun();
}

static void warpCursorTo(double x, double y) {
    CGWarpMouseCursorPosition(CGPointMake(x, y));
}

static double getEventLocX(CGEventRef e) {
    return CGEventGetLocation(e).x;
}

static double getEventLocY(CGEventRef e) {
    return CGEventGetLocation(e).y;
}

static int64_t getEventKeyCode(CGEventRef e) {
    return CGEventGetIntegerValueField(e, kCGKeyboardEventKeycode);
}

static uint64_t getEventFlags(CGEventRef e) {
    return (uint64_t)CGEventGetFlags(e);
}

static int64_t getScrollDeltaY(CGEventRef e) {
    return CGEventGetIntegerValueField(e, kCGScrollWheelEventPointDeltaAxis1);
}

static void hideCursorGlobal(void) {
    [NSCursor hide];
}

static void showCursorGlobal(void) {
    [NSCursor unhide];
}

static int getActiveDisplayCount(void) {
    uint32_t count = 0;
    CGGetActiveDisplayList(0, NULL, &count);
    return (int)count;
}

static void getDisplayBounds(int idx, double *x, double *y, double *w, double *h) {
    uint32_t count = 0;
    CGGetActiveDisplayList(0, NULL, &count);
    if (count == 0) return;
    CGDirectDisplayID *ids = (CGDirectDisplayID *)malloc(sizeof(CGDirectDisplayID) * count);
    if (!ids) return;
    CGGetActiveDisplayList(count, ids, &count);
    if (idx >= 0 && idx < (int)count) {
        CGRect bounds = CGDisplayBounds(ids[idx]);
        *x = bounds.origin.x;
        *y = bounds.origin.y;
        *w = bounds.size.width;
        *h = bounds.size.height;
    }
    free(ids);
}

static void getCurrentCursorPos(double *x, double *y) {
    CGEventRef event = CGEventCreate(NULL);
    CGPoint loc = CGEventGetLocation(event);
    CFRelease(event);
    *x = loc.x;
    *y = loc.y;
}

static uint64_t getCurrentModifierFlags(void) {
    CGEventRef event = CGEventCreate(NULL);
    uint64_t flags = (uint64_t)CGEventGetFlags(event);
    CFRelease(event);
    return flags;
}

// Null helpers — CGo opaque types cannot be compared to nil in Go.
static CGEventRef nullCGEvent(void)             { return NULL; }
static int machPortIsNull(CFMachPortRef p)       { return p == NULL; }
static int rlSourceIsNull(CFRunLoopSourceRef s)  { return s == NULL; }
*/
import "C"

import (
	"context"
	"errors"
	"runtime"
	"time"
	"unsafe"
)

const (
	hostEdgeActivationThreshold = 1
	edgeReturnPressureThreshold = 48
	edgeEntryInsetMin           = 24
	edgeEntryInsetMax           = 160
	leftwardReturnMinStep       = 6
	leftwardReturnThreshold     = 900
	leftwardReturnWindow        = 450 * time.Millisecond

	// CGEventFlags constants
	cgFlagShift   = 0x00020000
	cgFlagControl = 0x00040000
	cgFlagOption  = 0x00080000 // Alt
	cgFlagCommand = 0x00100000 // Win/Cmd

	// CGEventType constants
	cgEventMouseMoved         = 5
	cgEventLeftMouseDown      = 1
	cgEventLeftMouseUp        = 2
	cgEventRightMouseDown     = 3
	cgEventRightMouseUp       = 4
	cgEventScrollWheel        = 22
	cgEventKeyDown            = 10
	cgEventKeyUp              = 11
	cgEventFlagsChanged       = 12
	cgEventLeftMouseDragged   = 6
	cgEventRightMouseDragged  = 7
)

// point is a 2D integer coordinate (using int32 for compatibility with macOS CGPoint).
type point struct {
	X int32
	Y int32
}

type monitorRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (r monitorRect) containsPoint(p point) bool {
	return p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

func (r monitorRect) centerPoint() point {
	w := r.Right - r.Left
	h := r.Bottom - r.Top
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return point{X: r.Left + w/2, Y: r.Top + h/2}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampInt32(v, mn, mx int32) int32 {
	if mx < mn {
		return mn
	}
	if v < mn {
		return mn
	}
	if v > mx {
		return mx
	}
	return v
}

func publishInputEvent(out chan<- inputEvent, event inputEvent) {
	select {
	case out <- event:
	default:
	}
}

func publishRemoteModeEvent(out chan<- inputEvent, active bool, source string) {
	publishInputEvent(out, inputEvent{
		kind:   inputRemoteModeChangedEvent,
		active: active,
		source: source,
	})
}

// darwinHookState holds all mutable state for a running hook session.
// It is only accessed from the CFRunLoop goroutine (single-threaded).
type darwinHookState struct {
	// Config (read-only after init)
	captureKeyboard       bool
	toggleHotkeyVK        uint32
	toggleHotkeyMods      uint32
	leftwardReturnEnabled bool
	slaveWidth            int
	slaveHeight           int
	hostSide              string
	autoSwitch            bool
	out                   chan<- inputEvent
	activationAllowed     func() bool

	// Mutable state
	remoteModeActive         bool
	cursorHidden             bool
	edgeArmed                bool
	hotkeyDown               bool
	leftwardReturnDist       int
	leftwardReturnWindowStart time.Time
	edgeReturnPressure       int
	virtualSlaveX            int
	virtualSlaveY            int
	anchorX                  int32
	anchorY                  int32
	monitorRects             []monitorRect
	lastFlags                uint64
}

// gHook is the active hook state, set while hooks are running.
// Access from goHookEventCallback (CGEventTap callback thread).
var gHook *darwinHookState

// currentMods reads the current modifier state from cached event flags.
func currentMods() uint32 {
	flags := uint64(C.getCurrentModifierFlags())
	return flagsToMods(flags)
}

func flagsToMods(flags uint64) uint32 {
	var mods uint32
	if flags&cgFlagControl != 0 {
		mods |= hotkeyModCtrl
	}
	if flags&cgFlagOption != 0 {
		mods |= hotkeyModAlt
	}
	if flags&cgFlagShift != 0 {
		mods |= hotkeyModShift
	}
	if flags&cgFlagCommand != 0 {
		mods |= hotkeyModWin
	}
	return mods
}

func enumerateMonitorRects() ([]monitorRect, error) {
	count := int(C.getActiveDisplayCount())
	if count == 0 {
		return nil, errors.New("no displays found")
	}

	rects := make([]monitorRect, 0, count)
	for i := 0; i < count; i++ {
		var x, y, w, h C.double
		C.getDisplayBounds(C.int(i), &x, &y, &w, &h)
		if w > 0 && h > 0 {
			rects = append(rects, monitorRect{
				Left:   int32(x),
				Top:    int32(y),
				Right:  int32(x + w),
				Bottom: int32(y + h),
			})
		}
	}
	return rects, nil
}

func findMonitorForPoint(p point, rects []monitorRect) (monitorRect, bool) {
	for _, r := range rects {
		if r.containsPoint(p) {
			return r, true
		}
	}
	return monitorRect{}, false
}

func pointInsideAnyMonitor(p point, rects []monitorRect) bool {
	_, found := findMonitorForPoint(p, rects)
	return found
}

func virtualCenterPoint() point {
	rects, err := enumerateMonitorRects()
	if err != nil || len(rects) == 0 {
		return point{X: 960, Y: 540}
	}
	return rects[0].centerPoint()
}

func currentCursorPoint() (point, bool) {
	var x, y C.double
	C.getCurrentCursorPos(&x, &y)
	return point{X: int32(x), Y: int32(y)}, true
}

func setCursorPosition(x, y int32) {
	C.warpCursorTo(C.double(x), C.double(y))
}

func isOuterActivationEdgePoint(p point, monRect monitorRect, monRects []monitorRect, hostSide string) bool {
	if !monRect.containsPoint(p) {
		return false
	}

	switch hostSide {
	case hostSideRight:
		if p.X > monRect.Left+hostEdgeActivationThreshold {
			return false
		}
		samplePoint := point{X: monRect.Left - 1, Y: clampInt32(p.Y, monRect.Top, monRect.Bottom-1)}
		return !pointInsideAnyMonitor(samplePoint, monRects)
	case hostSideTop:
		if p.Y > monRect.Top+hostEdgeActivationThreshold {
			return false
		}
		samplePoint := point{X: clampInt32(p.X, monRect.Left, monRect.Right-1), Y: monRect.Top - 1}
		return !pointInsideAnyMonitor(samplePoint, monRects)
	case hostSideBottom:
		if p.Y < monRect.Bottom-1-hostEdgeActivationThreshold {
			return false
		}
		samplePoint := point{X: clampInt32(p.X, monRect.Left, monRect.Right-1), Y: monRect.Bottom}
		return !pointInsideAnyMonitor(samplePoint, monRects)
	default: // hostSideLeft
		if p.X < monRect.Right-1-hostEdgeActivationThreshold {
			return false
		}
		samplePoint := point{X: monRect.Right, Y: clampInt32(p.Y, monRect.Top, monRect.Bottom-1)}
		return !pointInsideAnyMonitor(samplePoint, monRects)
	}
}

// ── darwinHookState methods ──────────────────────────────────────────────────

func (h *darwinHookState) refreshMonitorRects() {
	rects, err := enumerateMonitorRects()
	if err != nil || len(rects) == 0 {
		return
	}
	h.monitorRects = rects
}

func (h *darwinHookState) findMonitor(p point) (monitorRect, bool) {
	if r, ok := findMonitorForPoint(p, h.monitorRects); ok {
		return r, true
	}
	h.refreshMonitorRects()
	return findMonitorForPoint(p, h.monitorRects)
}

func (h *darwinHookState) canActivateFromEdge(p point) bool {
	monRect, found := h.findMonitor(p)
	if !found {
		// Fallback: use first monitor bounds
		if len(h.monitorRects) == 0 {
			return false
		}
		monRect = h.monitorRects[0]
		switch h.hostSide {
		case hostSideRight:
			return p.X <= monRect.Left+hostEdgeActivationThreshold
		case hostSideTop:
			return p.Y <= monRect.Top+hostEdgeActivationThreshold
		case hostSideBottom:
			return p.Y >= monRect.Bottom-1-hostEdgeActivationThreshold
		default:
			return p.X >= monRect.Right-1-hostEdgeActivationThreshold
		}
	}
	return isOuterActivationEdgePoint(p, monRect, h.monitorRects, h.hostSide)
}

func (h *darwinHookState) setAnchor(p point) {
	if r, ok := h.findMonitor(p); ok {
		center := r.centerPoint()
		h.anchorX = center.X
		h.anchorY = center.Y
		return
	}
	center := virtualCenterPoint()
	h.anchorX = center.X
	h.anchorY = center.Y
}

func (h *darwinHookState) entryInset(axisLen int) int {
	inset := axisLen / 12
	if inset < edgeEntryInsetMin {
		inset = edgeEntryInsetMin
	}
	if inset > edgeEntryInsetMax {
		inset = edgeEntryInsetMax
	}
	if inset >= axisLen {
		inset = axisLen / 2
	}
	if inset < 0 {
		inset = 0
	}
	return inset
}

func (h *darwinHookState) setVirtualCursorForActivation(source string) {
	entryX := h.slaveWidth / 2
	entryY := h.slaveHeight / 2

	if source == "edge" {
		insetX := h.entryInset(h.slaveWidth)
		insetY := h.entryInset(h.slaveHeight)
		switch h.hostSide {
		case hostSideLeft:
			entryX = insetX
		case hostSideRight:
			entryX = h.slaveWidth - 1 - insetX
		case hostSideTop:
			entryY = insetY
		case hostSideBottom:
			entryY = h.slaveHeight - 1 - insetY
		}
	}

	h.virtualSlaveX = clampIntX(entryX, 0, h.slaveWidth-1)
	h.virtualSlaveY = clampIntX(entryY, 0, h.slaveHeight-1)
	h.edgeReturnPressure = 0
}

func clampIntX(v, mn, mx int) int {
	if v < mn {
		return mn
	}
	if v > mx {
		return mx
	}
	return v
}

func (h *darwinHookState) updateVirtualCursor(dx, dy int) bool {
	nextX := h.virtualSlaveX + dx
	nextY := h.virtualSlaveY + dy
	overflow := 0

	switch h.hostSide {
	case hostSideLeft:
		if nextX < 0 && dx < 0 {
			overflow = -nextX
		}
	case hostSideRight:
		if nextX >= h.slaveWidth && dx > 0 {
			overflow = nextX - (h.slaveWidth - 1)
		}
	case hostSideTop:
		if nextY < 0 && dy < 0 {
			overflow = -nextY
		}
	case hostSideBottom:
		if nextY >= h.slaveHeight && dy > 0 {
			overflow = nextY - (h.slaveHeight - 1)
		}
	}

	if overflow > 0 {
		h.edgeReturnPressure += overflow
	} else {
		decay := absInt(dx) + absInt(dy)
		if decay < 1 {
			decay = 1
		}
		h.edgeReturnPressure -= decay * 2
		if h.edgeReturnPressure < 0 {
			h.edgeReturnPressure = 0
		}
	}

	h.virtualSlaveX = clampIntX(nextX, 0, h.slaveWidth-1)
	h.virtualSlaveY = clampIntX(nextY, 0, h.slaveHeight-1)

	return h.edgeReturnPressure >= edgeReturnPressureThreshold
}

func (h *darwinHookState) updateLeftwardReturn(dx, dy int) bool {
	if !h.leftwardReturnEnabled || h.hostSide != hostSideLeft {
		return false
	}
	if dx >= 0 {
		h.leftwardReturnDist = 0
		h.leftwardReturnWindowStart = time.Time{}
		return false
	}
	if absInt(dx) < leftwardReturnMinStep {
		return false
	}
	if absInt(dy) > absInt(dx)*2 {
		h.leftwardReturnDist = 0
		h.leftwardReturnWindowStart = time.Time{}
		return false
	}

	now := time.Now()
	if h.leftwardReturnDist == 0 || h.leftwardReturnWindowStart.IsZero() {
		h.leftwardReturnWindowStart = now
	} else if now.Sub(h.leftwardReturnWindowStart) > leftwardReturnWindow {
		h.leftwardReturnDist = 0
		h.leftwardReturnWindowStart = now
	}

	h.leftwardReturnDist += -dx
	if h.leftwardReturnDist < 0 {
		h.leftwardReturnDist = 0
	}
	return h.leftwardReturnDist >= leftwardReturnThreshold
}

func (h *darwinHookState) resetLeftwardReturn() {
	h.leftwardReturnDist = 0
	h.leftwardReturnWindowStart = time.Time{}
}

func (h *darwinHookState) resetEdgePressure() {
	h.edgeReturnPressure = 0
}

func (h *darwinHookState) returnToHostPoint(current point) point {
	if r, ok := h.findMonitor(point{X: h.anchorX, Y: h.anchorY}); ok {
		tX := clampInt32(current.X, r.Left, r.Right-1)
		tY := clampInt32(current.Y, r.Top, r.Bottom-1)
		switch h.hostSide {
		case hostSideRight:
			tX = r.Left + 1
			if tX >= r.Right {
				tX = r.Left
			}
		case hostSideTop:
			tY = r.Top + 1
			if tY >= r.Bottom {
				tY = r.Top
			}
		case hostSideBottom:
			tY = r.Bottom - 2
			if tY < r.Top {
				tY = r.Top
			}
		default:
			tX = r.Right - 2
			if tX < r.Left {
				tX = r.Left
			}
		}
		return point{X: tX, Y: tY}
	}
	return virtualCenterPoint()
}

func (h *darwinHookState) setRemoteMode(active bool, source string) {
	if h.remoteModeActive == active {
		return
	}
	h.remoteModeActive = active
	if active {
		C.hideCursorGlobal()
		h.cursorHidden = true
	} else {
		if h.cursorHidden {
			C.showCursorGlobal()
			h.cursorHidden = false
		}
	}
	publishRemoteModeEvent(h.out, active, source)
}

func (h *darwinHookState) isActivationAllowed() bool {
	if h.activationAllowed == nil {
		return true
	}
	return h.activationAllowed()
}

func (h *darwinHookState) disableRemoteIfDisconnected() {
	if !h.remoteModeActive {
		return
	}
	if h.isActivationAllowed() {
		return
	}
	returnPt := h.returnToHostPoint(point{X: h.anchorX, Y: h.anchorY})
	setCursorPosition(returnPt.X, returnPt.Y)
	h.setRemoteMode(false, "serial")
	h.edgeArmed = true
	h.resetLeftwardReturn()
	h.resetEdgePressure()
}

// ── CGEventTap exported callback ─────────────────────────────────────────────

//export goHookEventCallback
func goHookEventCallback(proxy C.CGEventTapProxy, eventType C.CGEventType, event C.CGEventRef, userInfo unsafe.Pointer) C.CGEventRef {
	h := gHook
	if h == nil {
		return event
	}

	et := uint32(eventType)

	switch et {
	case cgEventMouseMoved, cgEventLeftMouseDragged, cgEventRightMouseDragged:
		return h.handleMouseMove(event)
	case cgEventLeftMouseDown:
		return h.handleMouseButton(event, inputMouseLeftDownEvent)
	case cgEventLeftMouseUp:
		return h.handleMouseButton(event, inputMouseLeftUpEvent)
	case cgEventRightMouseDown:
		return h.handleMouseButton(event, inputMouseRightDownEvent)
	case cgEventRightMouseUp:
		return h.handleMouseButton(event, inputMouseRightUpEvent)
	case cgEventScrollWheel:
		return h.handleScroll(event)
	case cgEventKeyDown:
		return h.handleKey(event, true)
	case cgEventKeyUp:
		return h.handleKey(event, false)
	case cgEventFlagsChanged:
		h.lastFlags = uint64(C.getEventFlags(event))
		return event
	}

	return event
}

func (h *darwinHookState) handleMouseMove(event C.CGEventRef) C.CGEventRef {
	x := int32(C.getEventLocX(event))
	y := int32(C.getEventLocY(event))
	p := point{X: x, Y: y}

	h.disableRemoteIfDisconnected()

	if !h.remoteModeActive {
		if !h.isActivationAllowed() {
			h.edgeArmed = true
			h.resetLeftwardReturn()
			h.resetEdgePressure()
		} else if h.autoSwitch && h.canActivateFromEdge(p) {
			if h.edgeArmed {
				h.setAnchor(p)
				h.setVirtualCursorForActivation("edge")
				h.setRemoteMode(true, "edge")
				h.edgeArmed = false
				h.resetLeftwardReturn()
				h.resetEdgePressure()
				C.warpCursorTo(C.double(h.anchorX), C.double(h.anchorY))
				return C.nullCGEvent() // swallow
			}
		} else {
			h.edgeArmed = true
		}
		return event
	}

	// Remote mode active
	dx := int(x - h.anchorX)
	dy := int(y - h.anchorY)

	shouldReturn := h.updateVirtualCursor(dx, dy)
	if !shouldReturn {
		shouldReturn = h.updateLeftwardReturn(dx, dy)
	}

	if shouldReturn {
		returnPt := h.returnToHostPoint(p)
		C.warpCursorTo(C.double(returnPt.X), C.double(returnPt.Y))
		h.setRemoteMode(false, "slave_edge")
		h.edgeArmed = false
		h.resetLeftwardReturn()
		h.resetEdgePressure()
		return C.nullCGEvent() // swallow
	}

	if dx != 0 || dy != 0 {
		publishInputEvent(h.out, inputEvent{kind: inputMouseDeltaEvent, x: dx, y: dy})
	}

	C.warpCursorTo(C.double(h.anchorX), C.double(h.anchorY))
	return C.nullCGEvent() // swallow
}

func (h *darwinHookState) handleMouseButton(event C.CGEventRef, kind inputEventKind) C.CGEventRef {
	h.disableRemoteIfDisconnected()

	if h.remoteModeActive {
		publishInputEvent(h.out, inputEvent{kind: kind})
		return C.nullCGEvent() // swallow
	}
	return event
}

func (h *darwinHookState) handleScroll(event C.CGEventRef) C.CGEventRef {
	h.disableRemoteIfDisconnected()

	if !h.remoteModeActive {
		return event
	}

	delta := int(C.getScrollDeltaY(event))
	if delta != 0 {
		// Normalize: macOS scroll delta is already in points; map to ±1 steps
		steps := delta / 3
		if steps == 0 {
			if delta > 0 {
				steps = 1
			} else {
				steps = -1
			}
		}
		publishInputEvent(h.out, inputEvent{kind: inputMouseScrollEvent, scroll: steps})
	}
	return C.nullCGEvent() // swallow
}

func (h *darwinHookState) handleKey(event C.CGEventRef, isDown bool) C.CGEventRef {
	h.disableRemoteIfDisconnected()

	keyCode := uint32(C.getEventKeyCode(event))
	flags := uint64(C.getEventFlags(event))
	mods := flagsToMods(flags)

	// Check for toggle hotkey
	if keyCode == h.toggleHotkeyVK && mods == h.toggleHotkeyMods {
		consumeHotkey := h.remoteModeActive || h.isActivationAllowed()

		if isDown {
			if !h.hotkeyDown {
				h.hotkeyDown = true
				if consumeHotkey {
					if !h.remoteModeActive {
						if cp, ok := currentCursorPoint(); ok {
							h.setAnchor(cp)
						}
						h.setVirtualCursorForActivation("hotkey")
						h.setRemoteMode(true, "hotkey")
						C.warpCursorTo(C.double(h.anchorX), C.double(h.anchorY))
					} else {
						returnPt := h.returnToHostPoint(point{X: h.anchorX, Y: h.anchorY})
						C.warpCursorTo(C.double(returnPt.X), C.double(returnPt.Y))
						h.setRemoteMode(false, "hotkey")
					}
					h.edgeArmed = false
					h.resetLeftwardReturn()
					h.resetEdgePressure()
				}
			}
		} else {
			h.hotkeyDown = false
		}

		if consumeHotkey {
			return C.nullCGEvent() // swallow
		}
	}

	// Forward keyboard if in remote mode
	if h.remoteModeActive && h.captureKeyboard {
		bleCode, mapped := vkToBleKeyCode(keyCode)
		if mapped {
			kind := inputKeyboardUpEvent
			if isDown {
				kind = inputKeyboardDownEvent
			}
			publishInputEvent(h.out, inputEvent{kind: kind, keyCode: bleCode})
		}
		return C.nullCGEvent() // swallow
	}

	return event
}

// ── runInputHooks ─────────────────────────────────────────────────────────────

func runInputHooks(
	ctx context.Context,
	captureKeyboard bool,
	toggleHotkeyVK uint32,
	toggleHotkeyMods uint32,
	leftwardReturnEnabled bool,
	slaveWidth int,
	slaveHeight int,
	hostSide string,
	autoSwitchEnabled bool,
	out chan<- inputEvent,
	remoteActivationAllowed func() bool,
) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if toggleHotkeyVK == 0 {
		defaultVK, _ := toggleHotkeyNameToVK(defaultToggleHotkeyName)
		toggleHotkeyVK = defaultVK
	}
	if slaveWidth <= 0 {
		slaveWidth = defaultSlaveWidth
	}
	if slaveHeight <= 0 {
		slaveHeight = defaultSlaveHeight
	}
	if normalizedHostSide, ok := normalizeHostSide(hostSide); ok {
		hostSide = normalizedHostSide
	} else {
		hostSide = defaultHostSide
	}

	monRects, _ := enumerateMonitorRects()

	anchorPt := virtualCenterPoint()
	if cp, ok := currentCursorPoint(); ok {
		anchorPt = cp
	}

	h := &darwinHookState{
		captureKeyboard:       captureKeyboard,
		toggleHotkeyVK:        toggleHotkeyVK,
		toggleHotkeyMods:      toggleHotkeyMods,
		leftwardReturnEnabled: leftwardReturnEnabled,
		slaveWidth:            slaveWidth,
		slaveHeight:           slaveHeight,
		hostSide:              hostSide,
		autoSwitch:            autoSwitchEnabled,
		out:                   out,
		activationAllowed:     remoteActivationAllowed,
		edgeArmed:             true,
		anchorX:               anchorPt.X,
		anchorY:               anchorPt.Y,
		monitorRects:          monRects,
	}
	h.setVirtualCursorForActivation("hotkey")

	gHook = h
	defer func() {
		gHook = nil
		if h.cursorHidden {
			C.showCursorGlobal()
		}
		if h.remoteModeActive {
			C.showCursorGlobal()
		}
	}()

	tap := C.installHookEventTap()
	if C.machPortIsNull(tap) != 0 {
		return errors.New("CGEventTapCreate failed — grant Accessibility permission in System Settings > Privacy & Security > Accessibility")
	}
	defer C.CFRelease(C.CFTypeRef(unsafe.Pointer(tap)))

	source := C.CFMachPortCreateRunLoopSource(C.kCFAllocatorDefault, tap, 0)
	if C.rlSourceIsNull(source) != 0 {
		return errors.New("CFMachPortCreateRunLoopSource failed")
	}
	defer C.CFRelease(C.CFTypeRef(unsafe.Pointer(source)))

	C.CFRunLoopAddSource(C.CFRunLoopGetCurrent(), source, C.kCFRunLoopCommonModes)
	defer C.CFRunLoopRemoveSource(C.CFRunLoopGetCurrent(), source, C.kCFRunLoopCommonModes)

	C.CGEventTapEnable(tap, C.bool(true))
	defer C.CGEventTapEnable(tap, C.bool(false))

	C.saveHookRunLoop()

	stopCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			C.stopHookRunLoop()
		case <-stopCh:
		}
	}()
	defer close(stopCh)

	C.runHookRunLoop()
	return nil
}
