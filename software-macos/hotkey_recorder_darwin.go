//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation -framework ApplicationServices

#include <ApplicationServices/ApplicationServices.h>
#include <stdlib.h>

extern CGEventRef goRecorderEventCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *userInfo);

// Local copies of helpers (CGo preambles are per-file; each file needs its own declarations).
static int64_t recGetEventKeyCode(CGEventRef e) {
    return CGEventGetIntegerValueField(e, kCGKeyboardEventKeycode);
}
static uint64_t recGetEventFlags(CGEventRef e) {
    return (uint64_t)CGEventGetFlags(e);
}

static CFRunLoopRef gRecorderRunLoop = NULL;

static CFMachPortRef installRecorderEventTap(void) {
    CGEventMask mask = CGEventMaskBit(kCGEventKeyDown) | CGEventMaskBit(kCGEventFlagsChanged);
    return CGEventTapCreate(
        kCGSessionEventTap,
        kCGHeadInsertEventTap,
        kCGEventTapOptionDefault,
        mask,
        goRecorderEventCallback,
        NULL);
}

static void saveRecorderRunLoop(void) {
    gRecorderRunLoop = CFRunLoopGetCurrent();
    CFRetain(gRecorderRunLoop);
}

static void stopRecorderRunLoop(void) {
    if (gRecorderRunLoop) {
        CFRunLoopStop(gRecorderRunLoop);
        CFRelease(gRecorderRunLoop);
        gRecorderRunLoop = NULL;
    }
}

static void runRecorderRunLoop(void) {
    CFRunLoopRun();
}

// Null helpers for recorder file.
static CGEventRef recNullCGEvent(void)           { return NULL; }
static int recMachPortIsNull(CFMachPortRef p)     { return p == NULL; }
static int recRLSourceIsNull(CFRunLoopSourceRef s){ return s == NULL; }
*/
import "C"

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

var recorderActive int32 // atomic: 1 while recording

type recorderKeyCallback func(keyCode uint32, mods uint32) bool

var gRecorderCallback recorderKeyCallback

// startHotkeyRecording launches the hotkey capture process.
// Called from the GUI when the user clicks "Record".
func (app *guiApp) startHotkeyRecording() {
	if app.recordingHotkey {
		return
	}
	app.recordingHotkey = true
	app.setHotkeyRecordingUI(true)
	go app.runHotkeyCapture()
}

func (app *guiApp) runHotkeyCapture() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	atomic.StoreInt32(&recorderActive, 1)

	gRecorderCallback = func(keyCode uint32, mods uint32) bool {
		if IsModifierVK(keyCode) {
			return false
		}
		_, ok := vkToHotkeyName(keyCode)
		if !ok {
			return false
		}

		comboName := FormatHotkeyCombo(keyCode, mods)

		atomic.StoreInt32(&recorderActive, 0)
		C.stopRecorderRunLoop()

		app.applyRecordedHotkey(comboName)
		return true
	}

	tap := C.installRecorderEventTap()
	if C.recMachPortIsNull(tap) != 0 {
		atomic.StoreInt32(&recorderActive, 0)
		app.applyRecordedHotkey("")
		return
	}
	defer C.CFRelease(C.CFTypeRef(unsafe.Pointer(tap)))

	source := C.CFMachPortCreateRunLoopSource(C.kCFAllocatorDefault, tap, 0)
	if C.recRLSourceIsNull(source) != 0 {
		atomic.StoreInt32(&recorderActive, 0)
		app.applyRecordedHotkey("")
		return
	}
	defer C.CFRelease(C.CFTypeRef(unsafe.Pointer(source)))

	C.CFRunLoopAddSource(C.CFRunLoopGetCurrent(), source, C.kCFRunLoopCommonModes)
	defer C.CFRunLoopRemoveSource(C.CFRunLoopGetCurrent(), source, C.kCFRunLoopCommonModes)

	C.CGEventTapEnable(tap, C.bool(true))
	defer C.CGEventTapEnable(tap, C.bool(false))

	C.saveRecorderRunLoop()
	C.runRecorderRunLoop()
}

//export goRecorderEventCallback
func goRecorderEventCallback(proxy C.CGEventTapProxy, eventType C.CGEventType, event C.CGEventRef, userInfo unsafe.Pointer) C.CGEventRef {
	if atomic.LoadInt32(&recorderActive) != 1 {
		return event
	}

	if uint32(eventType) != cgEventKeyDown {
		return event
	}

	keyCode := uint32(C.recGetEventKeyCode(event))
	flags := uint64(C.recGetEventFlags(event))
	mods := flagsToMods(flags)

	if cb := gRecorderCallback; cb != nil {
		if cb(keyCode, mods) {
			return C.recNullCGEvent() // swallow
		}
	}

	return event
}
