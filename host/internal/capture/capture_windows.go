//go:build windows

// Package capture owns the Windows low-level input hooks and the remote-mode
// state machine. Ported nearly verbatim from the legacy
// software/hooks_windows.go — the edge-activation probe, debounce, virtual
// slave cursor, return-pressure model, and the serial-drop force-exit
// invariant are preserved exactly. New over legacy: middle mouse button and
// horizontal wheel capture.
package capture

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"esp-hid/host/internal/hotkey"
	"esp-hid/host/internal/keymap"
	"esp-hid/host/internal/protocol"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procGetCursorInfo       = user32.NewProc("GetCursorInfo")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procShowCursor          = user32.NewProc("ShowCursor")
	procCreateCursor        = user32.NewProc("CreateCursor")
	procSetSystemCursor     = user32.NewProc("SetSystemCursor")
	procSystemParametersW   = user32.NewProc("SystemParametersInfoW")
	procSetCursorPos        = user32.NewProc("SetCursorPos")
	procGetKeyState         = user32.NewProc("GetKeyState")
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14

	wmQuit         = 0x0012
	wmKeyDown      = 0x0100
	wmKeyUp        = 0x0101
	wmSysKeyDown   = 0x0104
	wmSysKeyUp     = 0x0105
	wmMouseMove    = 0x0200
	wmLButtonDown  = 0x0201
	wmLButtonUp    = 0x0202
	wmRButtonDown  = 0x0204
	wmRButtonUp    = 0x0205
	wmMButtonDown  = 0x0207
	wmMButtonUp    = 0x0208
	wmMouseWheel   = 0x020A
	wmMouseHWheel  = 0x020E

	llkhfInjected = 0x10
	llmhfInjected = 0x01
	wheelDelta    = 120

	smCXScreen        = 0
	smCYScreen        = 1
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	cursorShowing = 0x00000001
	spiSetCursors = 0x0057

	ocrNormal      = 32512
	ocrIBeam       = 32513
	ocrWait        = 32514
	ocrCross       = 32515
	ocrUp          = 32516
	ocrSizeNWSE    = 32642
	ocrSizeNESW    = 32643
	ocrSizeWE      = 32644
	ocrSizeNS      = 32645
	ocrSizeAll     = 32646
	ocrNo          = 32648
	ocrHand        = 32649
	ocrAppStarting = 32650

	// Tuning constants preserved from the legacy implementation.
	hostEdgeActivationThreshold = 1
	edgeReturnPressureThreshold = 48
	edgeEntryInsetMin           = 24
	edgeEntryInsetMax           = 160
	leftwardReturnMinStep       = 6
	leftwardReturnThreshold     = 900
	leftwardReturnWindow        = 450 * time.Millisecond
)

// Host side names (shared vocabulary with config).
const (
	HostSideLeft   = "left"
	HostSideRight  = "right"
	HostSideTop    = "top"
	HostSideBottom = "bottom"
)

var hideSystemCursorIDs = [...]uintptr{
	ocrNormal, ocrIBeam, ocrWait, ocrCross, ocrUp, ocrSizeNWSE, ocrSizeNESW,
	ocrSizeWE, ocrSizeNS, ocrSizeAll, ocrNo, ocrHand, ocrAppStarting,
}

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
}

type point struct {
	X int32
	Y int32
}

type windowsMessage struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type msllHookStruct struct {
	Pt          point
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type kbdllHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type cursorInfo struct {
	CbSize      uint32
	Flags       uint32
	HCursor     uintptr
	PtScreenPos point
}

type monitorRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (rect monitorRect) containsPoint(p point) bool {
	return p.X >= rect.Left && p.X < rect.Right && p.Y >= rect.Top && p.Y < rect.Bottom
}

func (rect monitorRect) centerPoint() point {
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	return point{X: rect.Left + width/2, Y: rect.Top + height/2}
}

// currentMods samples the live modifier state into a hotkey.Mod* bitmask.
func currentMods() uint32 {
	isDown := func(vk uintptr) bool {
		ret, _, _ := procGetKeyState.Call(vk)
		return (ret & 0x8000) != 0
	}
	var mods uint32
	if isDown(hotkey.VKLControl) || isDown(hotkey.VKRControl) {
		mods |= hotkey.ModCtrl
	}
	if isDown(hotkey.VKLMenu) || isDown(hotkey.VKRMenu) {
		mods |= hotkey.ModAlt
	}
	if isDown(hotkey.VKLShift) || isDown(hotkey.VKRShift) {
		mods |= hotkey.ModShift
	}
	if isDown(hotkey.VKLWin) || isDown(hotkey.VKRWin) {
		mods |= hotkey.ModWin
	}
	return mods
}

func enumerateMonitorRects() ([]monitorRect, error) {
	monitorRects := make([]monitorRect, 0, 4)
	enumCallback := windows.NewCallback(func(_ uintptr, _ uintptr, rect *monitorRect, _ uintptr) uintptr {
		if rect == nil {
			return 1
		}
		if rect.Right > rect.Left && rect.Bottom > rect.Top {
			monitorRects = append(monitorRects, *rect)
		}
		return 1
	})
	enumerated, _, enumerateErr := procEnumDisplayMonitors.Call(0, 0, enumCallback, 0)
	if enumerated == 0 {
		if enumerateErr != syscall.Errno(0) {
			return nil, enumerateErr
		}
		return nil, errors.New("EnumDisplayMonitors failed")
	}
	return monitorRects, nil
}

func findMonitorForPoint(p point, monitorRects []monitorRect) (monitorRect, bool) {
	for _, rect := range monitorRects {
		if rect.containsPoint(p) {
			return rect, true
		}
	}
	return monitorRect{}, false
}

func pointInsideAnyMonitor(p point, monitorRects []monitorRect) bool {
	_, found := findMonitorForPoint(p, monitorRects)
	return found
}

func clampInt32(value, minValue, maxValue int32) int32 {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// isOuterActivationEdgePoint reports whether p sits on the activation edge of
// its monitor AND that edge is the outer boundary of the whole desktop — it
// probes one pixel past the edge and requires that point to be outside every
// monitor, so seams between physical monitors never activate.
func isOuterActivationEdgePoint(p point, rect monitorRect, monitorRects []monitorRect, hostSide string) bool {
	if !rect.containsPoint(p) {
		return false
	}
	switch hostSide {
	case HostSideRight:
		activationX := rect.Left + hostEdgeActivationThreshold
		if p.X > activationX {
			return false
		}
		sampleY := clampInt32(p.Y, rect.Top, rect.Bottom-1)
		return !pointInsideAnyMonitor(point{X: rect.Left - 1, Y: sampleY}, monitorRects)
	case HostSideTop:
		activationY := rect.Top + hostEdgeActivationThreshold
		if p.Y > activationY {
			return false
		}
		sampleX := clampInt32(p.X, rect.Left, rect.Right-1)
		return !pointInsideAnyMonitor(point{X: sampleX, Y: rect.Top - 1}, monitorRects)
	case HostSideBottom:
		activationY := rect.Bottom - 1 - hostEdgeActivationThreshold
		if p.Y < activationY {
			return false
		}
		sampleX := clampInt32(p.X, rect.Left, rect.Right-1)
		return !pointInsideAnyMonitor(point{X: sampleX, Y: rect.Bottom}, monitorRects)
	default: // host on the left: activation edge is the right border
		activationX := rect.Right - 1 - hostEdgeActivationThreshold
		if p.X < activationX {
			return false
		}
		sampleY := clampInt32(p.Y, rect.Top, rect.Bottom-1)
		return !pointInsideAnyMonitor(point{X: rect.Right, Y: sampleY}, monitorRects)
	}
}

func currentCursorPoint() (point, bool) {
	var cursorPoint point
	fetched, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursorPoint)))
	if fetched == 0 {
		return point{}, false
	}
	return cursorPoint, true
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func publish(out chan<- Event, event Event) {
	select {
	case out <- event:
	default:
	}
}

func normalizeWheel(mouseData uint32) int {
	delta := int16(mouseData >> 16)
	if delta == 0 {
		return 0
	}
	steps := int(delta) / wheelDelta
	if steps == 0 {
		if delta > 0 {
			return 1
		}
		return -1
	}
	return steps
}

func getSystemMetric(index int32) int32 {
	value, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(value)
}

func virtualCenterPoint() point {
	left := getSystemMetric(smXVirtualScreen)
	top := getSystemMetric(smYVirtualScreen)
	width := getSystemMetric(smCXVirtualScreen)
	height := getSystemMetric(smCYVirtualScreen)
	if width <= 0 {
		left = 0
		width = getSystemMetric(smCXScreen)
	}
	if height <= 0 {
		top = 0
		height = getSystemMetric(smCYScreen)
	}
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	return point{X: left + width/2, Y: top + height/2}
}

func setCursorPosition(x, y int32) {
	procSetCursorPos.Call(uintptr(int(x)), uintptr(int(y)))
}

func currentCursorVisible() (bool, bool) {
	info := cursorInfo{CbSize: uint32(unsafe.Sizeof(cursorInfo{}))}
	fetched, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&info)))
	if fetched == 0 {
		return false, false
	}
	return (info.Flags & cursorShowing) != 0, true
}

func setCursorVisible(visible bool) {
	const maxAttempts = 16
	showValue := uintptr(0)
	if visible {
		showValue = 1
	}
	currentVisible, ok := currentCursorVisible()
	if ok && currentVisible == visible {
		return
	}
	for i := 0; i < maxAttempts; i++ {
		procShowCursor.Call(showValue)
		currentVisible, ok = currentCursorVisible()
		if !ok || currentVisible == visible {
			return
		}
	}
}

func createTransparentCursor() (uintptr, error) {
	const cursorSize = 32
	var andMask [cursorSize * cursorSize / 8]byte
	var xorMask [cursorSize * cursorSize / 8]byte
	for i := range andMask {
		andMask[i] = 0xFF
	}
	cursorHandle, _, createErr := procCreateCursor.Call(
		0, 0, 0, cursorSize, cursorSize,
		uintptr(unsafe.Pointer(&andMask[0])),
		uintptr(unsafe.Pointer(&xorMask[0])),
	)
	if cursorHandle == 0 {
		if createErr != syscall.Errno(0) {
			return 0, createErr
		}
		return 0, errors.New("CreateCursor failed")
	}
	return cursorHandle, nil
}

func hideSystemCursors() bool {
	for _, cursorID := range hideSystemCursorIDs {
		cursorHandle, err := createTransparentCursor()
		if err != nil {
			return false
		}
		updated, _, _ := procSetSystemCursor.Call(cursorHandle, cursorID)
		if updated == 0 {
			return false
		}
	}
	return true
}

func restoreSystemCursors() {
	procSystemParametersW.Call(spiSetCursors, 0, 0, 0)
}

// Run installs the hooks and pumps messages until ctx is canceled. It must
// own its OS thread. activationAllowed gates remote-mode entry AND forces an
// exit when it turns false mid-session (the "never trapped on a dead link"
// invariant) — pass the serial-up && BLE-connected condition.
func Run(ctx context.Context, opts Options, out chan<- Event, activationAllowedFn func() bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	toggleVK, toggleMods := hotkey.Parse(opts.ToggleHotkey)
	if toggleVK == 0 {
		toggleVK, toggleMods = hotkey.Parse(hotkey.DefaultName)
	}
	slaveWidth := opts.SlaveWidth
	slaveHeight := opts.SlaveHeight
	if slaveWidth <= 0 {
		slaveWidth = 1920
	}
	if slaveHeight <= 0 {
		slaveHeight = 1080
	}
	hostSide := opts.HostSide
	switch hostSide {
	case HostSideLeft, HostSideRight, HostSideTop, HostSideBottom:
	default:
		hostSide = HostSideLeft
	}

	remoteModeActive := false
	systemCursorHidden := false
	edgeArmed := true
	hotkeyDown := false
	leftwardReturnDistance := 0
	leftwardReturnWindowStart := time.Time{}
	edgeReturnPressure := 0
	virtualSlaveX := slaveWidth / 2
	virtualSlaveY := slaveHeight / 2
	remoteAnchor := virtualCenterPoint()

	defer func() {
		if systemCursorHidden {
			restoreSystemCursors()
		}
		if remoteModeActive {
			setCursorVisible(true)
		}
	}()

	setRemoteModeActive := func(active bool, source string) {
		if remoteModeActive == active {
			return
		}
		remoteModeActive = active
		if active {
			setCursorVisible(false)
			if visible, ok := currentCursorVisible(); (!ok || visible) && !systemCursorHidden {
				systemCursorHidden = hideSystemCursors()
			}
		} else {
			if systemCursorHidden {
				restoreSystemCursors()
				systemCursorHidden = false
			}
			setCursorVisible(true)
		}
		publish(out, Event{Kind: EventRemoteMode, Active: active, Source: source})
	}

	monitorRects, _ := enumerateMonitorRects()
	refreshMonitorRects := func() {
		updated, err := enumerateMonitorRects()
		if err != nil || len(updated) == 0 {
			return
		}
		monitorRects = updated
	}
	findMonitor := func(p point) (monitorRect, bool) {
		if rect, found := findMonitorForPoint(p, monitorRects); found {
			return rect, true
		}
		refreshMonitorRects()
		return findMonitorForPoint(p, monitorRects)
	}
	canActivateFromHostEdge := func(p point) bool {
		rect, found := findMonitor(p)
		if !found {
			virtualLeft := getSystemMetric(smXVirtualScreen)
			virtualTop := getSystemMetric(smYVirtualScreen)
			virtualWidth := getSystemMetric(smCXVirtualScreen)
			virtualHeight := getSystemMetric(smCYVirtualScreen)
			if virtualWidth <= 0 {
				virtualLeft = 0
				virtualWidth = getSystemMetric(smCXScreen)
			}
			if virtualHeight <= 0 {
				virtualTop = 0
				virtualHeight = getSystemMetric(smCYScreen)
			}
			if virtualWidth <= 0 {
				virtualWidth = 1
			}
			if virtualHeight <= 0 {
				virtualHeight = 1
			}
			virtualRight := virtualLeft + virtualWidth - 1
			virtualBottom := virtualTop + virtualHeight - 1
			switch hostSide {
			case HostSideRight:
				return p.X <= virtualLeft+hostEdgeActivationThreshold
			case HostSideTop:
				return p.Y <= virtualTop+hostEdgeActivationThreshold
			case HostSideBottom:
				return p.Y >= virtualBottom-hostEdgeActivationThreshold
			default:
				return p.X >= virtualRight-hostEdgeActivationThreshold
			}
		}
		return isOuterActivationEdgePoint(p, rect, monitorRects, hostSide)
	}
	entryInsetForAxis := func(axisLength int) int {
		inset := axisLength / 12
		if inset < edgeEntryInsetMin {
			inset = edgeEntryInsetMin
		}
		if inset > edgeEntryInsetMax {
			inset = edgeEntryInsetMax
		}
		if inset >= axisLength {
			inset = axisLength / 2
		}
		if inset < 0 {
			inset = 0
		}
		return inset
	}
	setVirtualSlaveCursorForActivation := func(source string) {
		entryX := slaveWidth / 2
		entryY := slaveHeight / 2
		if source == "edge" {
			insetX := entryInsetForAxis(slaveWidth)
			insetY := entryInsetForAxis(slaveHeight)
			switch hostSide {
			case HostSideLeft:
				entryX = insetX
			case HostSideRight:
				entryX = slaveWidth - 1 - insetX
			case HostSideTop:
				entryY = insetY
			case HostSideBottom:
				entryY = slaveHeight - 1 - insetY
			}
		}
		if entryX < 0 {
			entryX = 0
		} else if entryX >= slaveWidth {
			entryX = slaveWidth - 1
		}
		if entryY < 0 {
			entryY = 0
		} else if entryY >= slaveHeight {
			entryY = slaveHeight - 1
		}
		virtualSlaveX = entryX
		virtualSlaveY = entryY
		edgeReturnPressure = 0
	}
	resetEdgeReturnPressure := func() {
		edgeReturnPressure = 0
	}
	// Dead-reckons the cursor position on the slave; pushing past the
	// host-facing edge builds pressure (decaying at 2x movement) until the
	// return threshold trips.
	updateVirtualSlaveCursorAndCheckReturn := func(dx, dy int) bool {
		nextX := virtualSlaveX + dx
		nextY := virtualSlaveY + dy
		overflow := 0
		switch hostSide {
		case HostSideLeft:
			if nextX < 0 && dx < 0 {
				overflow = -nextX
			}
		case HostSideRight:
			if nextX >= slaveWidth && dx > 0 {
				overflow = nextX - (slaveWidth - 1)
			}
		case HostSideTop:
			if nextY < 0 && dy < 0 {
				overflow = -nextY
			}
		case HostSideBottom:
			if nextY >= slaveHeight && dy > 0 {
				overflow = nextY - (slaveHeight - 1)
			}
		}
		if overflow > 0 {
			edgeReturnPressure += overflow
		} else {
			decay := absInt(dx) + absInt(dy)
			if decay < 1 {
				decay = 1
			}
			edgeReturnPressure -= decay * 2
			if edgeReturnPressure < 0 {
				edgeReturnPressure = 0
			}
		}
		if nextX < 0 {
			nextX = 0
		} else if nextX >= slaveWidth {
			nextX = slaveWidth - 1
		}
		if nextY < 0 {
			nextY = 0
		} else if nextY >= slaveHeight {
			nextY = slaveHeight - 1
		}
		virtualSlaveX = nextX
		virtualSlaveY = nextY
		return edgeReturnPressure >= edgeReturnPressureThreshold
	}
	resetLeftwardReturnDistance := func() {
		leftwardReturnDistance = 0
		leftwardReturnWindowStart = time.Time{}
	}
	updateLeftwardReturnDistance := func(dx, dy int) bool {
		if !opts.LeftwardReturn || hostSide != HostSideLeft {
			return false
		}
		if dx >= 0 {
			leftwardReturnDistance = 0
			leftwardReturnWindowStart = time.Time{}
			return false
		}
		if absInt(dx) < leftwardReturnMinStep {
			return false
		}
		if absInt(dy) > absInt(dx)*2 {
			leftwardReturnDistance = 0
			leftwardReturnWindowStart = time.Time{}
			return false
		}
		now := time.Now()
		if leftwardReturnDistance == 0 || leftwardReturnWindowStart.IsZero() {
			leftwardReturnWindowStart = now
		} else if now.Sub(leftwardReturnWindowStart) > leftwardReturnWindow {
			leftwardReturnDistance = 0
			leftwardReturnWindowStart = now
		}
		leftwardReturnDistance += -dx
		if leftwardReturnDistance < 0 {
			leftwardReturnDistance = 0
		}
		return leftwardReturnDistance >= leftwardReturnThreshold
	}
	returnToHostPointForAnchor := func(current point) point {
		if rect, found := findMonitor(remoteAnchor); found {
			targetX := clampInt32(current.X, rect.Left, rect.Right-1)
			targetY := clampInt32(current.Y, rect.Top, rect.Bottom-1)
			switch hostSide {
			case HostSideRight:
				targetX = rect.Left + 1
				if targetX >= rect.Right {
					targetX = rect.Left
				}
			case HostSideTop:
				targetY = rect.Top + 1
				if targetY >= rect.Bottom {
					targetY = rect.Top
				}
			case HostSideBottom:
				targetY = rect.Bottom - 2
				if targetY < rect.Top {
					targetY = rect.Top
				}
			default:
				targetX = rect.Right - 2
				if targetX < rect.Left {
					targetX = rect.Left
				}
			}
			return point{X: targetX, Y: targetY}
		}
		virtualLeft := getSystemMetric(smXVirtualScreen)
		virtualTop := getSystemMetric(smYVirtualScreen)
		virtualWidth := getSystemMetric(smCXVirtualScreen)
		virtualHeight := getSystemMetric(smCYVirtualScreen)
		if virtualWidth <= 0 {
			virtualLeft = 0
			virtualWidth = getSystemMetric(smCXScreen)
		}
		if virtualHeight <= 0 {
			virtualTop = 0
			virtualHeight = getSystemMetric(smCYScreen)
		}
		if virtualWidth <= 0 {
			virtualWidth = 1
		}
		if virtualHeight <= 0 {
			virtualHeight = 1
		}
		virtualRight := virtualLeft + virtualWidth - 1
		virtualBottom := virtualTop + virtualHeight - 1
		targetX := clampInt32(current.X, virtualLeft, virtualRight)
		targetY := clampInt32(current.Y, virtualTop, virtualBottom)
		switch hostSide {
		case HostSideRight:
			targetX = virtualLeft + 1
		case HostSideTop:
			targetY = virtualTop + 1
		case HostSideBottom:
			targetY = virtualBottom - 1
		default:
			targetX = virtualRight - 1
		}
		return point{X: targetX, Y: targetY}
	}
	setRemoteAnchorForPoint := func(p point) {
		if rect, found := findMonitor(p); found {
			remoteAnchor = rect.centerPoint()
			return
		}
		remoteAnchor = virtualCenterPoint()
	}
	if cursorPoint, ok := currentCursorPoint(); ok {
		setRemoteAnchorForPoint(cursorPoint)
	}
	setVirtualSlaveCursorForActivation("hotkey")

	activationAllowed := func() bool {
		if activationAllowedFn == nil {
			return true
		}
		return activationAllowedFn()
	}
	// The invariant: at the top of BOTH hook callbacks, if the link is gone,
	// warp home and exit remote mode. You can never be trapped controlling a
	// device the link cannot reach.
	disableRemoteIfDisconnected := func() {
		if !remoteModeActive {
			return
		}
		if activationAllowed() {
			return
		}
		returnPoint := returnToHostPointForAnchor(remoteAnchor)
		setCursorPosition(returnPoint.X, returnPoint.Y)
		setRemoteModeActive(false, "serial")
		edgeArmed = true
		resetLeftwardReturnDistance()
		resetEdgeReturnPressure()
	}

	mouseCallback := windows.NewCallback(func(nCode uintptr, wParam uintptr, lParam *msllHookStruct) uintptr {
		lParamAddress := uintptr(0)
		if lParam != nil {
			lParamAddress = uintptr(unsafe.Pointer(lParam))
		}
		if int32(nCode) >= 0 {
			if lParam == nil {
				next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
				return next
			}

			disableRemoteIfDisconnected()

			if !remoteModeActive && uint32(wParam) == wmMouseMove {
				if !activationAllowed() {
					edgeArmed = true
					resetLeftwardReturnDistance()
					resetEdgeReturnPressure()
				} else if opts.AutoSwitch && canActivateFromHostEdge(lParam.Pt) {
					if edgeArmed {
						setRemoteAnchorForPoint(lParam.Pt)
						setVirtualSlaveCursorForActivation("edge")
						setRemoteModeActive(true, "edge")
						edgeArmed = false
						resetLeftwardReturnDistance()
						resetEdgeReturnPressure()
						setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
						return 1
					}
				} else {
					edgeArmed = true
				}
			}

			if remoteModeActive {
				if !systemCursorHidden {
					setCursorVisible(false)
					if visible, ok := currentCursorVisible(); ok && visible {
						systemCursorHidden = hideSystemCursors()
					}
				}

				if (lParam.Flags & llmhfInjected) != 0 {
					return 1
				}

				switch uint32(wParam) {
				case wmMouseMove:
					dx := int(lParam.Pt.X - remoteAnchor.X)
					dy := int(lParam.Pt.Y - remoteAnchor.Y)
					shouldReturn := updateVirtualSlaveCursorAndCheckReturn(dx, dy)
					if !shouldReturn {
						shouldReturn = updateLeftwardReturnDistance(dx, dy)
					}
					if shouldReturn {
						returnPoint := returnToHostPointForAnchor(lParam.Pt)
						setCursorPosition(returnPoint.X, returnPoint.Y)
						setRemoteModeActive(false, "slave_edge")
						edgeArmed = false
						resetLeftwardReturnDistance()
						resetEdgeReturnPressure()
						return 1
					}
					if dx != 0 || dy != 0 {
						publish(out, Event{Kind: EventMouseDelta, DX: dx, DY: dy})
					}
					setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
				case wmLButtonDown:
					publish(out, Event{Kind: EventButtonDown, Button: protocol.ButtonLeft})
				case wmLButtonUp:
					publish(out, Event{Kind: EventButtonUp, Button: protocol.ButtonLeft})
				case wmRButtonDown:
					publish(out, Event{Kind: EventButtonDown, Button: protocol.ButtonRight})
				case wmRButtonUp:
					publish(out, Event{Kind: EventButtonUp, Button: protocol.ButtonRight})
				case wmMButtonDown:
					publish(out, Event{Kind: EventButtonDown, Button: protocol.ButtonMiddle})
				case wmMButtonUp:
					publish(out, Event{Kind: EventButtonUp, Button: protocol.ButtonMiddle})
				case wmMouseWheel:
					if amount := normalizeWheel(lParam.MouseData); amount != 0 {
						publish(out, Event{Kind: EventScroll, ScrollV: amount})
					}
				case wmMouseHWheel:
					if amount := normalizeWheel(lParam.MouseData); amount != 0 {
						publish(out, Event{Kind: EventScroll, ScrollH: amount})
					}
				}
				return 1
			}
		}
		next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
		return next
	})

	keyboardCallback := windows.NewCallback(func(nCode uintptr, wParam uintptr, lParam *kbdllHookStruct) uintptr {
		lParamAddress := uintptr(0)
		if lParam != nil {
			lParamAddress = uintptr(unsafe.Pointer(lParam))
		}
		if int32(nCode) >= 0 {
			if lParam == nil {
				next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
				return next
			}

			disableRemoteIfDisconnected()

			message := uint32(wParam)
			isKeyDown := message == wmKeyDown || message == wmSysKeyDown
			isKeyUp := message == wmKeyUp || message == wmSysKeyUp
			isInjected := (lParam.Flags & llkhfInjected) != 0

			if lParam.VkCode == toggleVK && !isInjected && currentMods() == toggleMods {
				consumeHotkey := remoteModeActive || activationAllowed()
				if isKeyDown {
					if !hotkeyDown {
						hotkeyDown = true
						if consumeHotkey {
							if !remoteModeActive {
								if cursorPoint, ok := currentCursorPoint(); ok {
									setRemoteAnchorForPoint(cursorPoint)
								}
								setVirtualSlaveCursorForActivation("hotkey")
								setRemoteModeActive(true, "hotkey")
								setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
							} else {
								returnPoint := returnToHostPointForAnchor(remoteAnchor)
								setCursorPosition(returnPoint.X, returnPoint.Y)
								setRemoteModeActive(false, "hotkey")
							}
							edgeArmed = false
							resetLeftwardReturnDistance()
							resetEdgeReturnPressure()
						}
					}
				} else if isKeyUp {
					hotkeyDown = false
				}
				if consumeHotkey {
					return 1
				}
			}

			if remoteModeActive && opts.CaptureKeyboard && !isInjected {
				if usage, mapped := keymap.VKToUsage(lParam.VkCode); mapped {
					switch message {
					case wmKeyDown, wmSysKeyDown:
						publish(out, Event{Kind: EventKeyDown, Usage: usage})
					case wmKeyUp, wmSysKeyUp:
						publish(out, Event{Kind: EventKeyUp, Usage: usage})
					}
				}
				return 1
			}
		}
		next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
		return next
	})

	mouseHookHandle, _, mouseHookErr := procSetWindowsHookExW.Call(uintptr(whMouseLL), mouseCallback, 0, 0)
	if mouseHookHandle == 0 {
		if mouseHookErr != syscall.Errno(0) {
			return fmt.Errorf("SetWindowsHookExW(mouse) failed: %w", mouseHookErr)
		}
		return errors.New("SetWindowsHookExW(mouse) returned null hook handle")
	}
	defer procUnhookWindowsHookEx.Call(mouseHookHandle)

	keyboardHookHandle, _, keyboardHookErr := procSetWindowsHookExW.Call(uintptr(whKeyboardLL), keyboardCallback, 0, 0)
	if keyboardHookHandle == 0 {
		if keyboardHookErr != syscall.Errno(0) {
			return fmt.Errorf("SetWindowsHookExW(keyboard) failed: %w", keyboardHookErr)
		}
		return errors.New("SetWindowsHookExW(keyboard) returned null hook handle")
	}
	defer procUnhookWindowsHookEx.Call(keyboardHookHandle)

	threadID := windows.GetCurrentThreadId()
	stopSignal := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			procPostThreadMessageW.Call(uintptr(threadID), uintptr(wmQuit), 0, 0)
		case <-stopSignal:
		}
	}()
	defer close(stopSignal)

	var message windowsMessage
	for {
		ret, _, messageErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		switch int32(ret) {
		case -1:
			if messageErr != syscall.Errno(0) {
				return fmt.Errorf("GetMessageW failed: %w", messageErr)
			}
			return errors.New("GetMessageW failed")
		case 0:
			return nil
		default:
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
		}
	}
}
