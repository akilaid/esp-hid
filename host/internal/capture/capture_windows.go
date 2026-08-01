//go:build windows

// Windows input capture: two low-level hooks (WH_MOUSE_LL / WH_KEYBOARD_LL)
// driving the shared remote-mode state machine that lives in geometry.go.
// Ported nearly verbatim from the legacy software/hooks_windows.go — the
// edge-activation probe, debounce, virtual slave cursor, return-pressure
// model, and the serial-drop force-exit invariant are preserved exactly.
// New over legacy: middle mouse button and horizontal wheel capture.
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

	wmQuit        = 0x0012
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208
	wmMouseWheel  = 0x020A
	wmMouseHWheel = 0x020E

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
)

var hideSystemCursorIDs = [...]uintptr{
	ocrNormal, ocrIBeam, ocrWait, ocrCross, ocrUp, ocrSizeNWSE, ocrSizeNESW,
	ocrSizeWE, ocrSizeNS, ocrSizeAll, ocrNo, ocrHand, ocrAppStarting,
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

func currentCursorPoint() (point, bool) {
	var cursorPoint point
	fetched, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursorPoint)))
	if fetched == 0 {
		return point{}, false
	}
	return cursorPoint, true
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

// virtualDesktopRect is the bounding box of every monitor, used as the
// single-monitor fallback whenever EnumDisplayMonitors comes up empty.
func virtualDesktopRect() monitorRect {
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
	return monitorRect{Left: left, Top: top, Right: left + width, Bottom: top + height}
}

func virtualCenterPoint() point {
	return virtualDesktopRect().centerPoint()
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
	hostSide := normalizeHostSide(opts.HostSide)
	slaveCursor := newVirtualCursor(opts.SlaveWidth, opts.SlaveHeight, hostSide)
	leftwardReturn := &leftwardReturnTracker{enabled: opts.LeftwardReturn, hostSide: hostSide}

	remoteModeActive := false
	systemCursorHidden := false
	edgeArmed := true
	hotkeyDown := false
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
			// No known monitor contains the cursor: fall back to the whole
			// virtual desktop. With no other rects to probe against, only its
			// true outer border activates.
			return isOuterActivationEdgePoint(p, virtualDesktopRect(), nil, hostSide)
		}
		return isOuterActivationEdgePoint(p, rect, monitorRects, hostSide)
	}
	returnToHostPointForAnchor := func(current point) point {
		if rect, found := findMonitor(remoteAnchor); found {
			return returnPointInRect(current, rect, hostSide)
		}
		return returnPointInRect(current, virtualDesktopRect(), hostSide)
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
	slaveCursor.resetForActivation("hotkey")

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
		leftwardReturn.reset()
		slaveCursor.resetPressure()
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
					leftwardReturn.reset()
					slaveCursor.resetPressure()
				} else if opts.AutoSwitch && canActivateFromHostEdge(lParam.Pt) {
					if edgeArmed {
						setRemoteAnchorForPoint(lParam.Pt)
						slaveCursor.resetForActivation("edge")
						setRemoteModeActive(true, "edge")
						edgeArmed = false
						leftwardReturn.reset()
						slaveCursor.resetPressure()
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
					shouldReturn := slaveCursor.move(dx, dy)
					if !shouldReturn {
						shouldReturn = leftwardReturn.update(dx, dy, time.Now())
					}
					if shouldReturn {
						returnPoint := returnToHostPointForAnchor(lParam.Pt)
						setCursorPosition(returnPoint.X, returnPoint.Y)
						setRemoteModeActive(false, "slave_edge")
						edgeArmed = false
						leftwardReturn.reset()
						slaveCursor.resetPressure()
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
								slaveCursor.resetForActivation("hotkey")
								setRemoteModeActive(true, "hotkey")
								setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
							} else {
								returnPoint := returnToHostPointForAnchor(remoteAnchor)
								setCursorPosition(returnPoint.X, returnPoint.Y)
								setRemoteModeActive(false, "hotkey")
							}
							edgeArmed = false
							leftwardReturn.reset()
							slaveCursor.resetPressure()
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
