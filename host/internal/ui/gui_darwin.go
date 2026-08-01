//go:build darwin

// The AppKit macOS GUI, at parity with the Windows build: connection and BLE
// status, input settings, and bond clearing, plus a menu-bar item whose icon
// changes while remote mode is active.
//
// Two things it does that the Windows build does not need to. macOS withholds
// input capture behind two separate permissions, so the window surfaces them
// as a fixable state rather than an error; and it watches for Secure Event
// Input, which silently blocks keyboard capture whenever any app has a
// password field focused.
package ui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "gui_darwin.h"
*/
import "C"

import (
	"log"
	"runtime/cgo"
	"strconv"
	"unsafe"

	"esp-hid/host/internal/bridge"
	"esp-hid/host/internal/capture"
	"esp-hid/host/internal/config"
)

// System Settings pane anchors for the two permissions.
const (
	privacyAccessibility = "Privacy_Accessibility"
	privacyInputMonitor  = "Privacy_ListenEvent"
)

type darwinGUI struct {
	cfg     config.Config
	runtime *bridge.Runtime
	events  chan bridge.Event

	running        bool
	permissionsOK  bool
	autoStartDone  bool
	secureInputWas bool
}

// gui is a singleton: the C layer holds no Go pointers, so the exported
// callbacks resolve the instance through this rather than a handle.
var app *darwinGUI

// Run builds the window and enters the AppKit event loop. It must be called
// from the main goroutine, which cmd/bridge pins to the main OS thread.
func Run(cfg config.Config) error {
	app = &darwinGUI{
		cfg:    cfg,
		events: make(chan bridge.Event, 256),
	}
	app.runtime = bridge.New(app.events)

	C.ehbGuiInit()

	for _, choice := range SlaveResolutionChoices {
		cValue := C.CString(choice)
		C.ehbGuiAddResolution(cValue)
		C.free(unsafe.Pointer(cValue))
	}
	for _, side := range HostSideChoices {
		cValue := C.CString(side)
		C.ehbGuiAddHostSide(cValue)
		C.free(unsafe.Pointer(cValue))
	}

	values := FormValuesFrom(cfg)
	cHotkey := C.CString(values.ToggleHotkey)
	cResolution := C.CString(values.Resolution)
	C.ehbGuiSetForm(cHotkey, C.int(cfg.MoveRateHz), cBool(cfg.CaptureKeyboard),
		cResolution, C.int(values.HostSideIndex))
	C.free(unsafe.Pointer(cHotkey))
	C.free(unsafe.Pointer(cResolution))

	go app.consumeEvents()

	// Unlike Windows, do not auto-start blindly: without permission the
	// capture layer would fail immediately and the user would see an error
	// instead of the thing they can actually act on.
	app.refreshPermissions()

	C.ehbGuiRun()
	return nil
}

func cBool(value bool) C.int {
	if value {
		return 1
	}
	return 0
}

func setStatus(bridgeText, device, firmware, bluetooth string) {
	var cBridge, cDevice, cFirmware, cBluetooth *C.char
	if bridgeText != "" {
		cBridge = C.CString(bridgeText)
		defer C.free(unsafe.Pointer(cBridge))
	}
	if device != "" {
		cDevice = C.CString(device)
		defer C.free(unsafe.Pointer(cDevice))
	}
	if firmware != "" {
		cFirmware = C.CString(firmware)
		defer C.free(unsafe.Pointer(cFirmware))
	}
	if bluetooth != "" {
		cBluetooth = C.CString(bluetooth)
		defer C.free(unsafe.Pointer(cBluetooth))
	}
	C.ehbGuiSetStatus(cBridge, cDevice, cFirmware, cBluetooth)
}

func showAlert(title, message string, isError bool) {
	cTitle := C.CString(title)
	cMessage := C.CString(message)
	C.ehbGuiShowAlert(cTitle, cMessage, cBool(isError))
	C.free(unsafe.Pointer(cTitle))
	C.free(unsafe.Pointer(cMessage))
}

func setBanner(message string, visible, showButtons bool) {
	cMessage := C.CString(message)
	C.ehbGuiSetBanner(cMessage, cBool(visible), cBool(showButtons))
	C.free(unsafe.Pointer(cMessage))
}

// onMain marshals fn onto the main thread. AppKit may only be touched there,
// and bridge events arrive on a worker goroutine — this is the direct
// analogue of walk's Synchronize on Windows.
func onMain(fn func()) {
	handle := cgo.NewHandle(fn)
	C.ehbGuiPerformOnMain(C.uintptr_t(handle))
}

//export goGuiPerform
func goGuiPerform(token C.uintptr_t) {
	handle := cgo.Handle(uintptr(token))
	defer handle.Delete()
	if fn, ok := handle.Value().(func()); ok {
		fn()
	}
}

func (g *darwinGUI) consumeEvents() {
	for event := range g.events {
		event := event
		onMain(func() { g.applyEvent(event) })
	}
}

func (g *darwinGUI) applyEvent(event bridge.Event) {
	switch event.Kind {
	case bridge.EventStarting:
		setStatus("Starting — looking for device…", "", "", "")
	case bridge.EventSerialConnected:
		setStatus("Running", event.Port, "", "")
	case bridge.EventSerialDown:
		setStatus("Waiting for device (USB)…", "-", "", "-")
	case bridge.EventHello:
		setStatus("", "", FirmwareText(event.Hello), "")
	case bridge.EventBleState:
		setStatus("", "", "", BleStateText(event.BleState))
	case bridge.EventDeviceError:
		log.Printf("device error: %s", event.Detail)
	case bridge.EventRemoteMode:
		C.ehbGuiSetRemoteActive(cBool(event.Active))
	case bridge.EventPermissionRequired:
		// Recoverable, and the banner already explains it — an alert here
		// would just be noise on top.
		g.setRunning(false)
		setStatus("Permission required", "", "", "")
		g.refreshPermissions()
	case bridge.EventCaptureError:
		setStatus("Capture error", "", "", "")
		showAlert("Capture error", event.Detail, true)
		g.setRunning(false)
	case bridge.EventStopped:
		setStatus("Stopped", "-", "", "-")
		g.setRunning(false)
	case bridge.EventLog:
		log.Printf("device: %s", event.Detail)
	}
}

func (g *darwinGUI) setRunning(running bool) {
	g.running = running
	C.ehbGuiSetRunning(cBool(running))
}

func (g *darwinGUI) readConfigFromForm() error {
	form := C.ehbGuiReadForm()
	values := FormValues{
		ToggleHotkey:    C.GoString(&form.hotkey[0]),
		MoveRateHz:      strconv.Itoa(int(form.rateHz)),
		Resolution:      C.GoString(&form.resolution[0]),
		HostSideIndex:   int(form.hostSideIndex),
		CaptureKeyboard: form.captureKeyboard != 0,
		// Hotkey-only on macOS. Edge switching is not offered here, so this
		// is pinned off rather than read from a control that does not exist
		// — otherwise a value persisted on Windows, or from an older build,
		// would quietly switch it back on.
		AutoSwitch: false,
	}
	return values.Apply(&g.cfg)
}

func (g *darwinGUI) startBridge() {
	if g.runtime.Running() {
		return
	}
	if err := g.readConfigFromForm(); err != nil {
		showAlert("Invalid settings", err.Error(), false)
		return
	}
	if !g.permissionsOK {
		g.refreshPermissions()
		return
	}
	if err := config.Save(g.cfg); err != nil {
		log.Printf("settings save failed: %v", err)
	}
	if err := g.runtime.Start(g.cfg); err != nil {
		showAlert("Start failed", err.Error(), true)
		return
	}
	g.setRunning(true)
}

// refreshPermissions drives the banner and gates Start. It is called at
// launch and once a second afterwards, so granting a permission in System
// Settings takes effect without the user hunting for a refresh button.
func (g *darwinGUI) refreshPermissions() {
	permissions := capture.CheckPermissions()
	ok := permissions.OK(g.cfg.CaptureKeyboard)
	g.permissionsOK = ok

	if !ok {
		setBanner(permissions.PermissionHint(g.cfg.CaptureKeyboard)+
			" — grant it, then reopen this app if nothing happens.", true, true)
		if !g.running {
			setStatus("Waiting for permission", "", "", "")
		}
		return
	}

	// Secure Event Input is not a permission and cannot be fixed here, but
	// it makes the keyboard silently stop working, so say so plainly.
	secureInput := capture.SecureInputEnabled()
	if secureInput != g.secureInputWas {
		g.secureInputWas = secureInput
	}
	if secureInput && g.cfg.CaptureKeyboard {
		setBanner("Keyboard blocked: another app has Secure Input enabled "+
			"(close any password field or sudo prompt).", true, false)
	} else {
		setBanner("", false, false)
	}

	// Permissions are in place: start once, the way the Windows build does.
	if !g.autoStartDone {
		g.autoStartDone = true
		g.startBridge()
	}
}

//export goGuiStartClicked
func goGuiStartClicked() { app.startBridge() }

//export goGuiStopClicked
func goGuiStopClicked() {
	if !app.runtime.Running() {
		return
	}
	// Stop blocks until the pipeline unwinds; doing that on the main thread
	// would freeze the UI.
	go app.runtime.Stop()
}

//export goGuiClearBondsClicked
func goGuiClearBondsClicked() {
	if !app.runtime.Running() {
		showAlert("Not running", "Start the bridge first so the device is connected.", false)
		return
	}
	app.runtime.ClearBonds()
	showAlert("Bonds cleared",
		"The device forgot all paired phones.\n\nOn the phone: forget \"ESP-HID-ME\" "+
			"in Bluetooth settings, then pair again.", false)
}

//export goGuiGrantClicked
func goGuiGrantClicked() {
	capture.RequestPermissions()
	app.refreshPermissions()
}

//export goGuiOpenSettingsClicked
func goGuiOpenSettingsClicked() {
	permissions := capture.CheckPermissions()
	anchor := privacyAccessibility
	if permissions.Accessibility {
		anchor = privacyInputMonitor
	}
	cAnchor := C.CString(anchor)
	C.ehbGuiOpenPrivacySettings(cAnchor)
	C.free(unsafe.Pointer(cAnchor))
}

//export goGuiTick
func goGuiTick() {
	if app == nil {
		return
	}
	app.refreshPermissions()
}

//export goGuiWillTerminate
func goGuiWillTerminate() {
	if app == nil || app.runtime == nil {
		return
	}
	// Synchronous on purpose. Remote mode leaves the pointer hidden and
	// decoupled from the mouse, and only the capture layer's teardown puts
	// that back; quitting without waiting would strand the user.
	if app.runtime.Running() {
		app.runtime.Stop()
	}
}
