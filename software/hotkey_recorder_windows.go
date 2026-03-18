//go:build windows

package main

import (
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// recordHookActive is 1 while a hotkey-capture hook is running.
var recordHookActive int32

type keyCallback func(vk uint32) (swallow bool)

var globalKeyCallback keyCallback

// startHotkeyRecording puts the GUI into hotkey-recording mode.
// The next non-modifier, non-escape key the user presses is captured (together
// with any currently-held modifiers) and written back into hotkeyLabel / baseCfg.
func (app *guiApp) startHotkeyRecording() {
	if app.recordingHotkey {
		return
	}
	app.recordingHotkey = true
	if app.hotkeyLabel != nil {
		app.hotkeyLabel.SetText("Press a key…")
	}
	if app.hotkeyRecordBtn != nil {
		app.hotkeyRecordBtn.SetEnabled(false)
	}

	go app.runHotkeyCapture()
}

func (app *guiApp) runHotkeyCapture() {
	atomic.StoreInt32(&recordHookActive, 1)

	var hookHandle uintptr

	globalKeyCallback = func(vk uint32) bool {
		// Skip pure modifier keys — wait for the trigger key.
		if IsModifierVK(vk) {
			return false
		}

		_, ok := vkToHotkeyName(vk)
		if !ok {
			return false // ignore keys we can't map
		}

		// Sample modifier state at the moment of the keypress.
		mods := currentMods()
		comboName := FormatHotkeyCombo(vk, mods)

		// Uninstall hook first, then update UI.
		atomic.StoreInt32(&recordHookActive, 0)
		unhookWindowsHookExRecorder(hookHandle)

		app.mw.Synchronize(func() {
			app.recordingHotkey = false
			toggleVK, toggleMods := toggleHotkeyNameToVKMods(comboName)
			app.baseCfg.toggleHotkeyName = comboName
			app.baseCfg.toggleHotkeyVK = toggleVK
			app.baseCfg.toggleHotkeyMods = toggleMods
			if app.hotkeyLabel != nil {
				app.hotkeyLabel.SetText(comboName)
			}
			if app.hotkeyRecordBtn != nil {
				app.hotkeyRecordBtn.SetEnabled(true)
			}
		})
		return true // swallow the key
	}

	hookHandle, _, _ = procSetWindowsHookExW.Call(
		whKeyboardLL,
		windows.NewCallback(recordLLKeyboardProc),
		0,
		0,
	)

	// Pump messages so the hook callback is delivered.
	type msg struct {
		Hwnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      [2]int32
	}
	var m msg
	for atomic.LoadInt32(&recordHookActive) == 1 {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func recordLLKeyboardProc(nCode int, wParam uintptr, ks *kbdllHookStruct) uintptr {
	if nCode >= 0 && (wParam == wmKeyDown || wParam == wmSysKeyDown) && ks != nil {
		if atomic.LoadInt32(&recordHookActive) == 1 && globalKeyCallback != nil {
			if swallow := globalKeyCallback(ks.VkCode); swallow {
				return 1
			}
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, uintptr(unsafe.Pointer(ks)))
	return ret
}

func unhookWindowsHookExRecorder(hook uintptr) {
	procUnhookWindowsHookEx.Call(hook)
}
