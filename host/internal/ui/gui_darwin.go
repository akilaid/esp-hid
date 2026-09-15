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

	running       bool
	permissionsOK bool
	autoStartDone bool
	secureInput   bool

	// What the update strip should say when no permission problem outranks
	// it, and whether the install button goes with it.
	updateText        string
	updateInstallable bool
	updater           *updater

	// The rows of the device suggestions list, in display order.
	deviceMatches []DeviceMatch
	// True while this code is writing to the device-layout controls. AppKit
	// does not echo programmatic writes back as edits, so this is belt and
	// braces — but it keeps the two GUIs' logic identical.
	syncing bool

	// The display-arrangement picture that chooses the host side.
	arranger *arranger
}

// gui is a singleton: the C layer holds no Go pointers, so the exported
// callbacks resolve the instance through this rather than a handle.
var app *darwinGUI

// Run builds the window and enters the AppKit event loop. It must be called
// from the main goroutine, which cmd/bridge pins to the main OS thread.
// version is the build's release tag, which the update check compares
// against; "dev" disables it.
func Run(cfg config.Config, version string) error {
	app = &darwinGUI{
		cfg:    cfg,
		events: make(chan bridge.Event, 256),
	}
	app.runtime = bridge.New(app.events)
	app.updater = newUpdater(version, onMain,
		func(text string, installable bool) {
			app.updateText, app.updateInstallable = text, installable
			app.refreshBanner()
		},
		showAlert,
		askUpdate,
		func() { C.ehbGuiTerminate() })
	app.updater.setEnabled(cfg.CheckUpdates)

	C.ehbGuiInit()
	C.ehbGuiSetAutoUpdateChecked(cBool(cfg.CheckUpdates))
	cVersion := C.CString(VersionText(version))
	C.ehbGuiSetVersion(cVersion)
	C.free(unsafe.Pointer(cVersion))

	for _, choice := range SlaveResolutionChoices {
		cValue := C.CString(choice)
		C.ehbGuiAddResolution(cValue)
		C.free(unsafe.Pointer(cValue))
	}
	for _, orientation := range OrientationChoices {
		cValue := C.CString(orientation)
		C.ehbGuiAddOrientation(cValue)
		C.free(unsafe.Pointer(cValue))
	}
	cHint := C.CString(ResolutionHint)
	C.ehbGuiSetResolutionHint(cHint)
	C.free(unsafe.Pointer(cHint))

	values := FormValuesFrom(cfg)
	cHotkey := C.CString(values.ToggleHotkey)
	cResolution := C.CString(values.Resolution)
	C.ehbGuiSetForm(cHotkey, C.int(cfg.MoveRateHz), cBool(cfg.CaptureKeyboard),
		cBool(cfg.AutoSwitch), cResolution)
	C.free(unsafe.Pointer(cHotkey))
	C.free(unsafe.Pointer(cResolution))
	if index := OrientationIndexOf(values.Resolution); index >= 0 {
		C.ehbGuiSetOrientation(C.int(index))
	}

	app.arranger = newArranger(cfg.HostSide, values.Resolution)
	var width, height C.double
	C.ehbGuiArrangeSize(&width, &height)
	app.arranger.setCanvas(float64(width), float64(height))
	app.refreshDisplays()

	go app.consumeEvents()

	// Unlike Windows, do not auto-start blindly: without permission the
	// capture layer would fail immediately and the user would see an error
	// instead of the thing they can actually act on.
	app.refreshPermissions()
	app.updater.start()

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

func askUpdate(title, message, notes string) bool {
	cTitle := C.CString(title)
	cMessage := C.CString(message)
	cNotes := C.CString(notes)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cMessage))
	defer C.free(unsafe.Pointer(cNotes))
	return C.ehbGuiAskUpdate(cTitle, cMessage, cNotes) != 0
}

func setBanner(message string, visible bool, buttons C.int, isError bool) {
	cMessage := C.CString(message)
	C.ehbGuiSetBanner(cMessage, cBool(visible), buttons, cBool(isError))
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
		setStatus("Running", DeviceText(event.Serial, event.Port), "", "")
	case bridge.EventSerialDown:
		setStatus("Waiting for device (USB)…", "-", "", "-")
	case bridge.EventDiscovering:
		setStatus("Identifying device…", "", "", "")
		log.Printf("device discovery: %s", event.Detail)
	case bridge.EventDeviceLearned:
		// Remember which board this is. Without this the next launch would
		// rediscover it, and re-probe the user's other ESP32s to do so.
		g.cfg.DeviceSerial = event.Serial
		if err := config.SaveDeviceSerial(event.Serial); err != nil {
			log.Printf("could not save device binding: %v", err)
		}
		setStatus("", DeviceText(event.Serial, event.Port), "", "")
	case bridge.EventDeviceAbsent:
		setStatus("Bridge not connected", DeviceText(event.Serial, "")+" (absent)", "-", "-")
		log.Printf("bridge absent: %s", event.Detail)
	case bridge.EventDeviceAmbiguous:
		setStatus("Several bridges found — leave one attached", "-", "-", "-")
		log.Printf("ambiguous bridge: %s", event.Detail)
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

// The device-layout controls feed one another: a picked device or a flipped
// orientation writes the resolution field, and an edited resolution moves the
// orientation popup. Same logic as the Windows build, behind the C setters.

func (g *darwinGUI) deviceSearchChanged(query string) {
	if g.syncing {
		return
	}
	g.deviceMatches = DeviceMatches(query)
	g.syncing = true
	C.ehbGuiClearDeviceMatches()
	for _, m := range g.deviceMatches {
		cLabel := C.CString(m.Label)
		cName := C.CString(m.Name)
		C.ehbGuiAddDeviceMatch(cLabel, cName)
		C.free(unsafe.Pointer(cLabel))
		C.free(unsafe.Pointer(cName))
	}
	// Show lays the list out (or hides it when empty); the highlight then
	// sits on the best match, which also fills the field below.
	C.ehbGuiShowDeviceMatches()
	if len(g.deviceMatches) > 0 {
		C.ehbGuiSelectDeviceMatch(0)
	}
	g.syncing = false
	if len(g.deviceMatches) > 0 {
		g.setResolution(g.deviceMatches[0].Resolution)
	}
}

func (g *darwinGUI) deviceMatchSelected(index int) {
	if g.syncing || index < 0 || index >= len(g.deviceMatches) {
		return
	}
	g.setResolution(g.deviceMatches[index].Resolution)
}

func (g *darwinGUI) orientationChanged(index int) {
	if g.syncing {
		return
	}
	form := C.ehbGuiReadForm()
	if flipped, ok := OrientResolution(C.GoString(&form.resolution[0]), index); ok {
		g.setResolution(flipped)
	}
}

func (g *darwinGUI) resolutionEdited(text string) {
	if g.syncing {
		return
	}
	if index := OrientationIndexOf(text); index >= 0 {
		g.syncing = true
		C.ehbGuiSetOrientation(C.int(index))
		g.syncing = false
	}
	g.arranger.setDevice(text)
	g.pushArrangement()
}

// setResolution writes the field and moves the orientation popup to match.
func (g *darwinGUI) setResolution(value string) {
	g.syncing = true
	cValue := C.CString(value)
	C.ehbGuiSetResolution(cValue)
	C.free(unsafe.Pointer(cValue))
	if index := OrientationIndexOf(value); index >= 0 {
		C.ehbGuiSetOrientation(C.int(index))
	}
	g.syncing = false
	g.arranger.setDevice(value)
	g.pushArrangement()
}

// refreshDisplays re-reads the monitors and redraws the picture. Called at
// startup and whenever macOS reports the screen layout changed.
func (g *darwinGUI) refreshDisplays() {
	const maxDisplays = 32
	buffer := make([]C.EhbDisplay, maxDisplays)
	count := int(C.ehbGuiDisplays(&buffer[0], maxDisplays))
	displays := make([]Display, 0, count)
	for i := 0; i < count; i++ {
		d := buffer[i]
		displays = append(displays, Display{
			Name:     C.GoString(&d.name[0]),
			X:        float64(d.x),
			Y:        float64(d.y),
			W:        float64(d.w),
			H:        float64(d.h),
			WidthMM:  float64(d.widthMM),
			HeightMM: float64(d.heightMM),
			Primary:  d.primary != 0,
		})
	}
	g.arranger.setDisplays(displays)
	g.pushArrangement()
}

// pushArrangement hands the current frame to the view.
func (g *darwinGUI) pushArrangement() {
	frame := g.arranger.frameNow()
	C.ehbGuiArrangeBegin(cBool(frame.Dragging))
	for _, d := range frame.Displays {
		cName := C.CString(d.Name)
		C.ehbGuiArrangeAddDisplay(cRect(d.Rect), cName, cBool(d.Primary))
		C.free(unsafe.Pointer(cName))
	}
	cLabel := C.CString(frame.Device.Name)
	C.ehbGuiArrangeSetDevice(cRect(frame.Device.Rect), cLabel)
	C.free(unsafe.Pointer(cLabel))
	C.ehbGuiArrangeEnd()
}

func cRect(r rectF) C.EhbRect {
	return C.EhbRect{x: C.double(r.X), y: C.double(r.Y), w: C.double(r.W), h: C.double(r.H)}
}

func (g *darwinGUI) readConfigFromForm() error {
	form := C.ehbGuiReadForm()
	values := FormValues{
		ToggleHotkey:    C.GoString(&form.hotkey[0]),
		MoveRateHz:      strconv.Itoa(int(form.rateHz)),
		Resolution:      C.GoString(&form.resolution[0]),
		HostSideIndex:   g.arranger.sideIndex(),
		CaptureKeyboard: form.captureKeyboard != 0,
		AutoSwitch:      form.autoSwitch != 0,
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
	g.permissionsOK = permissions.OK(g.cfg.CaptureKeyboard)
	// Secure Event Input is not a permission and cannot be fixed here, but
	// it makes the keyboard silently stop working, so say so plainly.
	g.secureInput = capture.SecureInputEnabled()
	g.refreshBanner()

	if !g.permissionsOK {
		if !g.running {
			setStatus("Waiting for permission", "", "", "")
		}
		return
	}
	// Permissions are in place: start once, the way the Windows build does.
	if !g.autoStartDone {
		g.autoStartDone = true
		g.startBridge()
	}
}

// refreshBanner picks what the strip shows. One thing at a time, most
// pressing first: a missing permission blocks everything, Secure Input
// blocks the keyboard, an update can wait.
func (g *darwinGUI) refreshBanner() {
	switch {
	case !g.permissionsOK:
		setBanner(capture.CheckPermissions().PermissionHint(g.cfg.CaptureKeyboard)+
			" — grant it, then reopen this app if nothing happens.",
			true, C.EHB_BANNER_PERMISSION, true)
	case g.secureInput && g.cfg.CaptureKeyboard:
		setBanner("Keyboard blocked: another app has Secure Input enabled "+
			"(close any password field or sudo prompt).", true, C.EHB_BANNER_NONE, true)
	case g.updateText != "":
		buttons := C.int(C.EHB_BANNER_NONE)
		if g.updateInstallable {
			buttons = C.EHB_BANNER_UPDATE
		}
		setBanner(g.updateText, true, buttons, false)
	default:
		setBanner("", false, C.EHB_BANNER_NONE, false)
	}
}

//export goGuiStartClicked
func goGuiStartClicked() { app.startBridge() }

//export goGuiUpdateClicked
func goGuiUpdateClicked() { app.updater.install() }

//export goGuiCheckUpdatesClicked
func goGuiCheckUpdatesClicked() { app.updater.checkNow() }

//export goGuiToggleAutoUpdatesClicked
func goGuiToggleAutoUpdatesClicked() {
	app.cfg.CheckUpdates = !app.cfg.CheckUpdates
	app.updater.setEnabled(app.cfg.CheckUpdates)
	C.ehbGuiSetAutoUpdateChecked(cBool(app.cfg.CheckUpdates))
	if err := config.Save(app.cfg); err != nil {
		log.Printf("settings save failed: %v", err)
	}
}

//export goGuiDeviceSearchChanged
func goGuiDeviceSearchChanged(text *C.char) { app.deviceSearchChanged(C.GoString(text)) }

//export goGuiDeviceMatchSelected
func goGuiDeviceMatchSelected(index C.int) { app.deviceMatchSelected(int(index)) }

//export goGuiOrientationChanged
func goGuiOrientationChanged(index C.int) { app.orientationChanged(int(index)) }

//export goGuiResolutionEdited
func goGuiResolutionEdited(text *C.char) { app.resolutionEdited(C.GoString(text)) }

//export goGuiArrangeMouse
func goGuiArrangeMouse(phase C.int, x, y C.double) {
	px, py := float64(x), float64(y)
	switch phase {
	case 0:
		if !app.arranger.beginDrag(px, py) {
			return
		}
	case 1:
		app.arranger.drag(px, py)
	default:
		app.arranger.endDrag(px, py)
	}
	app.pushArrangement()
}

//export goGuiDisplaysChanged
func goGuiDisplaysChanged() { app.refreshDisplays() }

//export goGuiStopClicked
func goGuiStopClicked() {
	if !app.runtime.Running() {
		return
	}
	// Stop blocks until the pipeline unwinds; doing that on the main thread
	// would freeze the UI.
	go app.runtime.Stop()
}

//export goGuiForgetDeviceClicked
func goGuiForgetDeviceClicked() {
	if app.runtime.Running() {
		// The button is disabled while running; this is belt and braces.
		return
	}
	// Drops the learned binding so the next Start re-identifies the bridge by
	// handshake. This is the way out of "never substitute": swap in a
	// replacement board, forget, start, and it binds to the new one.
	app.cfg.DeviceSerial = ""
	if err := config.Save(app.cfg); err != nil {
		log.Printf("settings save failed: %v", err)
	}
	setStatus("Device forgotten — press Start to detect again", "-", "-", "-")
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
