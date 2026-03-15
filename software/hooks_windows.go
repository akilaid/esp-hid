//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
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
	wmMouseWheel  = 0x020A

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

	rightEdgeActivationThreshold = 1
	leftwardReturnThreshold      = 280
)

var hideSystemCursorIDs = [...]uintptr{
	ocrNormal,
	ocrIBeam,
	ocrWait,
	ocrCross,
	ocrUp,
	ocrSizeNWSE,
	ocrSizeNESW,
	ocrSizeWE,
	ocrSizeNS,
	ocrSizeAll,
	ocrNo,
	ocrHand,
	ocrAppStarting,
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

	return point{
		X: rect.Left + width/2,
		Y: rect.Top + height/2,
	}
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
	for _, monitorRect := range monitorRects {
		if monitorRect.containsPoint(p) {
			return monitorRect, true
		}
	}

	return monitorRect{}, false
}

func pointInsideAnyMonitor(p point, monitorRects []monitorRect) bool {
	_, found := findMonitorForPoint(p, monitorRects)
	return found
}

func isOuterRightEdgePoint(p point, monitorRect monitorRect, monitorRects []monitorRect) bool {
	if !monitorRect.containsPoint(p) {
		return false
	}

	activationX := monitorRect.Right - 1 - rightEdgeActivationThreshold
	if p.X < activationX {
		return false
	}

	sampleY := p.Y
	if sampleY < monitorRect.Top {
		sampleY = monitorRect.Top
	} else if sampleY >= monitorRect.Bottom {
		sampleY = monitorRect.Bottom - 1
	}

	samplePoint := point{X: monitorRect.Right, Y: sampleY}
	return !pointInsideAnyMonitor(samplePoint, monitorRects)
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

func publishInputEvent(out chan<- inputEvent, event inputEvent) {
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

func virtualRightEdgeX() int32 {
	left := getSystemMetric(smXVirtualScreen)
	width := getSystemMetric(smCXVirtualScreen)
	if width <= 0 {
		left = 0
		width = getSystemMetric(smCXScreen)
	}
	if width <= 0 {
		return 0
	}
	return left + width - 1
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

	return point{
		X: left + width/2,
		Y: top + height/2,
	}
}

func setCursorPosition(x int32, y int32) {
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
		0,
		0,
		0,
		cursorSize,
		cursorSize,
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

func publishRemoteModeEvent(out chan<- inputEvent, active bool, source string) {
	publishInputEvent(out, inputEvent{
		kind:   inputRemoteModeChangedEvent,
		active: active,
		source: source,
	})
}

func runInputHooks(ctx context.Context, captureKeyboard bool, toggleHotkeyVK uint32, out chan<- inputEvent, remoteActivationAllowed func() bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if toggleHotkeyVK == 0 {
		defaultVK, _ := toggleHotkeyNameToVK(defaultToggleHotkeyName)
		toggleHotkeyVK = defaultVK
	}

	remoteModeActive := false
	systemCursorHidden := false
	edgeArmed := true
	hotkeyDown := false
	leftwardReturnDistance := 0
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
		publishRemoteModeEvent(out, active, source)
	}
	monitorRects, _ := enumerateMonitorRects()
	refreshMonitorRects := func() {
		updatedMonitorRects, err := enumerateMonitorRects()
		if err != nil || len(updatedMonitorRects) == 0 {
			return
		}

		monitorRects = updatedMonitorRects
	}
	findMonitor := func(p point) (monitorRect, bool) {
		if monitorRect, found := findMonitorForPoint(p, monitorRects); found {
			return monitorRect, true
		}

		refreshMonitorRects()
		return findMonitorForPoint(p, monitorRects)
	}
	canActivateFromRightEdge := func(p point) bool {
		monitorRect, found := findMonitor(p)
		if !found {
			rightEdgeX := virtualRightEdgeX()
			return p.X >= rightEdgeX-rightEdgeActivationThreshold
		}

		return isOuterRightEdgePoint(p, monitorRect, monitorRects)
	}
	resetLeftwardReturnDistance := func() {
		leftwardReturnDistance = 0
	}
	updateLeftwardReturnDistance := func(dx int, dy int) {
		if dx >= 0 {
			leftwardReturnDistance = 0
			return
		}

		if absInt(dy) > absInt(dx)*2 {
			leftwardReturnDistance = 0
			return
		}

		leftwardReturnDistance += -dx
		if leftwardReturnDistance < 0 {
			leftwardReturnDistance = 0
		}
	}
	returnToHostPointForAnchor := func(currentY int32) point {
		if monitorRect, found := findMonitor(remoteAnchor); found {
			targetY := currentY
			if targetY < monitorRect.Top {
				targetY = monitorRect.Top
			} else if targetY >= monitorRect.Bottom {
				targetY = monitorRect.Bottom - 1
			}

			targetX := monitorRect.Right - 2
			if targetX < monitorRect.Left {
				targetX = monitorRect.Left
			}

			return point{X: targetX, Y: targetY}
		}

		rightEdgeX := virtualRightEdgeX()
		return point{X: rightEdgeX - 1, Y: currentY}
	}
	setRemoteAnchorForPoint := func(p point) {
		if monitorRect, found := findMonitor(p); found {
			remoteAnchor = monitorRect.centerPoint()
			return
		}

		remoteAnchor = virtualCenterPoint()
	}
	if cursorPoint, ok := currentCursorPoint(); ok {
		setRemoteAnchorForPoint(cursorPoint)
	}
	activationAllowed := func() bool {
		if remoteActivationAllowed == nil {
			return true
		}
		return remoteActivationAllowed()
	}
	disableRemoteIfDisconnected := func() {
		if !remoteModeActive {
			return
		}
		if activationAllowed() {
			return
		}

		returnPoint := returnToHostPointForAnchor(remoteAnchor.Y)
		setCursorPosition(returnPoint.X, returnPoint.Y)
		setRemoteModeActive(false, "serial")
		edgeArmed = true
		resetLeftwardReturnDistance()
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
				} else if canActivateFromRightEdge(lParam.Pt) {
					if edgeArmed {
						setRemoteModeActive(true, "edge")
						edgeArmed = false
						resetLeftwardReturnDistance()
						setRemoteAnchorForPoint(lParam.Pt)
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
					updateLeftwardReturnDistance(dx, dy)
					if leftwardReturnDistance >= leftwardReturnThreshold {
						returnPoint := returnToHostPointForAnchor(lParam.Pt.Y)
						setCursorPosition(returnPoint.X, returnPoint.Y)
						setRemoteModeActive(false, "edge_return")
						edgeArmed = false
						resetLeftwardReturnDistance()
						return 1
					}
					if dx != 0 || dy != 0 {
						publishInputEvent(out, inputEvent{kind: inputMouseDeltaEvent, x: dx, y: dy})
					}
					setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
				case wmLButtonDown:
					publishInputEvent(out, inputEvent{kind: inputMouseLeftDownEvent})
				case wmLButtonUp:
					publishInputEvent(out, inputEvent{kind: inputMouseLeftUpEvent})
				case wmRButtonDown:
					publishInputEvent(out, inputEvent{kind: inputMouseRightDownEvent})
				case wmRButtonUp:
					publishInputEvent(out, inputEvent{kind: inputMouseRightUpEvent})
				case wmMouseWheel:
					scrollAmount := normalizeWheel(lParam.MouseData)
					if scrollAmount != 0 {
						publishInputEvent(out, inputEvent{kind: inputMouseScrollEvent, scroll: scrollAmount})
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

			if lParam.VkCode == toggleHotkeyVK && !isInjected {
				consumeHotkey := remoteModeActive || activationAllowed()

				if isKeyDown {
					if !hotkeyDown {
						hotkeyDown = true
						if consumeHotkey {
							nextRemoteModeState := !remoteModeActive
							if nextRemoteModeState {
								if cursorPoint, ok := currentCursorPoint(); ok {
									setRemoteAnchorForPoint(cursorPoint)
								}
								setRemoteModeActive(true, "hotkey")
								setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
							} else {
								returnPoint := returnToHostPointForAnchor(remoteAnchor.Y)
								setCursorPosition(returnPoint.X, returnPoint.Y)
								setRemoteModeActive(false, "hotkey")
							}
							edgeArmed = false
							resetLeftwardReturnDistance()
						}
					}
				} else if isKeyUp {
					hotkeyDown = false
				}

				if consumeHotkey {
					return 1
				}
			}

			if remoteModeActive && captureKeyboard && !isInjected {
				keyCode, mapped := vkToBleKeyCode(lParam.VkCode)
				if mapped {
					switch message {
					case wmKeyDown, wmSysKeyDown:
						publishInputEvent(out, inputEvent{kind: inputKeyboardDownEvent, keyCode: keyCode})
					case wmKeyUp, wmSysKeyUp:
						publishInputEvent(out, inputEvent{kind: inputKeyboardUpEvent, keyCode: keyCode})
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
