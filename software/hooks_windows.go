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
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
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

	rightEdgeActivationThreshold = 1
)

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

func publishRemoteModeEvent(out chan<- inputEvent, active bool, source string) {
	publishInputEvent(out, inputEvent{
		kind:   inputRemoteModeChangedEvent,
		active: active,
		source: source,
	})
}

func runInputHooks(ctx context.Context, captureKeyboard bool, toggleHotkeyVK uint32, out chan<- inputEvent) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if toggleHotkeyVK == 0 {
		defaultVK, _ := toggleHotkeyNameToVK(defaultToggleHotkeyName)
		toggleHotkeyVK = defaultVK
	}

	remoteModeActive := false
	edgeArmed := true
	hotkeyDown := false
	rightEdgeX := virtualRightEdgeX()
	remoteAnchor := virtualCenterPoint()

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

			if !remoteModeActive && uint32(wParam) == wmMouseMove {
				if lParam.Pt.X >= rightEdgeX-rightEdgeActivationThreshold {
					if edgeArmed {
						remoteModeActive = true
						edgeArmed = false
						publishRemoteModeEvent(out, true, "edge")
						setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
						return 1
					}
				} else {
					edgeArmed = true
				}
			}

			if remoteModeActive {
				if (lParam.Flags & llmhfInjected) != 0 {
					return 1
				}

				switch uint32(wParam) {
				case wmMouseMove:
					dx := int(lParam.Pt.X - remoteAnchor.X)
					dy := int(lParam.Pt.Y - remoteAnchor.Y)
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

			message := uint32(wParam)
			isKeyDown := message == wmKeyDown || message == wmSysKeyDown
			isKeyUp := message == wmKeyUp || message == wmSysKeyUp
			isInjected := (lParam.Flags & llkhfInjected) != 0

			if lParam.VkCode == toggleHotkeyVK && !isInjected {
				if isKeyDown {
					if !hotkeyDown {
						hotkeyDown = true
						remoteModeActive = !remoteModeActive
						edgeArmed = false
						publishRemoteModeEvent(out, remoteModeActive, "hotkey")
						if remoteModeActive {
							setCursorPosition(remoteAnchor.X, remoteAnchor.Y)
						}
					}
				} else if isKeyUp {
					hotkeyDown = false
				}

				return 1
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
