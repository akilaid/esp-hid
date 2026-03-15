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
	wmRButtonDown = 0x0204
	wmMouseWheel  = 0x020A

	llkhfInjected = 0x10
	wheelDelta    = 120
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

func runInputHooks(ctx context.Context, captureKeyboard bool, out chan<- inputEvent) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

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

			switch uint32(wParam) {
			case wmMouseMove:
				publishInputEvent(out, inputEvent{
					kind: inputMouseMoveEvent,
					x:    int(lParam.Pt.X),
					y:    int(lParam.Pt.Y),
				})
			case wmLButtonDown:
				publishInputEvent(out, inputEvent{kind: inputMouseLeftClickEvent})
			case wmRButtonDown:
				publishInputEvent(out, inputEvent{kind: inputMouseRightClickEvent})
			case wmMouseWheel:
				scrollAmount := normalizeWheel(lParam.MouseData)
				if scrollAmount != 0 {
					publishInputEvent(out, inputEvent{kind: inputMouseScrollEvent, scroll: scrollAmount})
				}
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

		if captureKeyboard && int32(nCode) >= 0 {
			if lParam == nil {
				next, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParamAddress)
				return next
			}

			if (lParam.Flags & llkhfInjected) == 0 {
				keyCode, mapped := vkToBleKeyCode(lParam.VkCode)
				if mapped {
					switch uint32(wParam) {
					case wmKeyDown, wmSysKeyDown:
						publishInputEvent(out, inputEvent{kind: inputKeyboardDownEvent, keyCode: keyCode})
					case wmKeyUp, wmSysKeyUp:
						publishInputEvent(out, inputEvent{kind: inputKeyboardUpEvent, keyCode: keyCode})
					}
				}
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

	if captureKeyboard {
		keyboardHookHandle, _, keyboardHookErr := procSetWindowsHookExW.Call(uintptr(whKeyboardLL), keyboardCallback, 0, 0)
		if keyboardHookHandle == 0 {
			if keyboardHookErr != syscall.Errno(0) {
				return fmt.Errorf("SetWindowsHookExW(keyboard) failed: %w", keyboardHookErr)
			}
			return errors.New("SetWindowsHookExW(keyboard) returned null hook handle")
		}
		defer procUnhookWindowsHookEx.Call(keyboardHookHandle)
	}

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
