//go:build windows

// The walk-based Windows GUI: connection + BLE status, input settings, and
// device maintenance (bond clearing). Adapted from the legacy
// software/gui_windows.go, with a BLE status line fed by device reports —
// the diagnostic the old system never had. Form validation and status
// wording are shared with the macOS GUI in form.go.
package ui

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"unsafe"

	"github.com/lxn/walk"
	//lint:ignore ST1001 walk's declarative DSL is designed for dot import
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"esp-hid/host/internal/bridge"
	"esp-hid/host/internal/config"
)

const (
	iconResourceIDApp        = 1
	iconResourceIDRemoteMode = 2
)

type gui struct {
	cfg     config.Config
	version string
	runtime *bridge.Runtime
	events  chan bridge.Event

	mw            *walk.MainWindow
	statusLabel   *walk.Label
	bleLabel      *walk.Label
	portLabel     *walk.Label
	fwLabel       *walk.Label
	startButton   *walk.PushButton
	stopButton    *walk.PushButton
	bondsButton   *walk.PushButton
	forgetButton  *walk.PushButton
	hotkeyEdit    *walk.LineEdit
	rateEdit      *walk.LineEdit
	keyboardCheck *walk.CheckBox
	autoRadio     *walk.RadioButton
	manualRadio   *walk.RadioButton
	resCombo      *walk.ComboBox
	deviceCombo   *walk.ComboBox
	orientCombo   *walk.ComboBox

	// The display-arrangement picture that chooses the host side. Untested
	// on real Windows so far: it only draws arranger's frame and forwards
	// mouse events, with the model itself covered by arrange_test.go.
	arrangeWidget *walk.CustomWidget
	arranger      *arranger

	// The update notice row in the status group, hidden until a newer
	// release is found; walk gives a hidden widget no space.
	updateRow        *walk.Composite
	updateLabel      *walk.Label
	updateButton     *walk.PushButton
	autoUpdateAction *walk.Action
	updater          *updater

	// The rows in deviceCombo's list, in display order.
	deviceMatches []DeviceMatch
	// True while this code is writing to the device-layout controls, so the
	// change events they raise are not mistaken for user edits and fed back.
	syncing bool

	trayIcon   *walk.NotifyIcon
	iconApp    *walk.Icon
	iconRemote *walk.Icon
	exiting    bool
}

// Run builds the window and enters the message loop. version is the build's
// release tag, which the update check compares against; "dev" disables it.
func Run(cfg config.Config, version string) error {
	app := &gui{
		cfg:     cfg,
		version: version,
		events:  make(chan bridge.Event, 256),
	}
	app.runtime = bridge.New(app.events)

	if err := app.build(); err != nil {
		return err
	}
	app.updater = newUpdater(version,
		func(fn func()) { app.mw.Synchronize(fn) },
		app.showUpdate,
		func(title, message string, isError bool) {
			style := walk.MsgBoxIconInformation
			if isError {
				style = walk.MsgBoxIconError
			}
			walk.MsgBox(app.mw, title, message, style)
		},
		func(title, message, notes string) bool {
			// A message box is the plain tool for this; the notes ride along
			// under the question, cut short if a release is very talkative.
			const maxNotes = 1500
			if len(notes) > maxNotes {
				notes = notes[:maxNotes] + "…"
			}
			text := message + "\n\nInstall it now and restart? (No keeps the offer in the window.)"
			if notes != "" {
				text += "\n\n" + notes
			}
			return walk.MsgBox(app.mw, title, text, walk.MsgBoxYesNo|walk.MsgBoxIconInformation) == walk.DlgCmdYes
		},
		func() {
			app.exiting = true
			app.mw.Close()
		})
	app.updater.setEnabled(cfg.CheckUpdates)
	app.updater.start()
	app.loadIcons()
	app.setupTray()
	defer func() {
		if app.trayIcon != nil {
			app.trayIcon.Dispose()
		}
	}()

	go app.consumeEvents()

	// Auto-start, like the legacy GUI.
	app.startBridge()

	app.mw.Show()
	app.mw.Run()
	if app.runtime.Running() {
		app.runtime.Stop()
	}
	return nil
}

func (app *gui) build() error {
	resValue := fmt.Sprintf("%dx%d", app.cfg.SlaveWidth, app.cfg.SlaveHeight)
	resIndex := IndexOf(SlaveResolutionChoices, resValue)
	orientIndex := OrientationIndexOf(resValue)
	if orientIndex < 0 {
		orientIndex = OrientationPortrait
	}

	window := MainWindow{
		AssignTo: &app.mw,
		Title:    "ESP HID Bridge",
		MinSize:  Size{Width: 560, Height: 440},
		Size:     Size{Width: 580, Height: 460},
		Layout:   VBox{},
		MenuItems: []MenuItem{
			Menu{
				Text: "&Help",
				Items: []MenuItem{
					Action{
						Text:        "Check for &updates…",
						OnTriggered: func() { app.updater.checkNow() },
					},
					Action{
						AssignTo:    &app.autoUpdateAction,
						Text:        "Check for updates &automatically",
						Checkable:   true,
						Checked:     app.cfg.CheckUpdates,
						OnTriggered: app.toggleAutoUpdates,
					},
				},
			},
		},
		Children: []Widget{
			GroupBox{
				Title:  "Connection && Status",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "Bridge:"},
					Label{AssignTo: &app.statusLabel, Text: "Stopped"},
					Label{Text: "Device:"},
					Label{AssignTo: &app.portLabel, Text: "-"},
					Label{Text: "Firmware:"},
					Label{AssignTo: &app.fwLabel, Text: "-"},
					Label{Text: "Bluetooth:"},
					Label{AssignTo: &app.bleLabel, Text: "-"},
					Composite{
						AssignTo:   &app.updateRow,
						Layout:     HBox{MarginsZero: true},
						ColumnSpan: 2,
						Visible:    false,
						Children: []Widget{
							Label{AssignTo: &app.updateLabel},
							PushButton{
								AssignTo:  &app.updateButton,
								Text:      "Install and restart",
								OnClicked: func() { app.updater.install() },
							},
							HSpacer{},
						},
					},
					Composite{
						Layout:     HBox{MarginsZero: true},
						ColumnSpan: 2,
						Children: []Widget{
							PushButton{AssignTo: &app.startButton, Text: "Start", OnClicked: app.startBridge},
							PushButton{AssignTo: &app.stopButton, Text: "Stop", Enabled: false, OnClicked: app.stopBridge},
							HSpacer{},
							PushButton{AssignTo: &app.forgetButton, Text: "Forget device", OnClicked: app.forgetDevice},
							PushButton{AssignTo: &app.bondsButton, Text: "Clear device bonds", OnClicked: app.clearBonds},
						},
					},
				},
			},
			GroupBox{
				Title:  "Input Settings",
				Layout: Grid{Columns: 4},
				Children: []Widget{
					Label{Text: "Toggle hotkey:"},
					LineEdit{AssignTo: &app.hotkeyEdit, Text: app.cfg.ToggleHotkey},
					Label{Text: "Send rate (Hz):"},
					LineEdit{AssignTo: &app.rateEdit, Text: strconv.Itoa(app.cfg.MoveRateHz)},
					CheckBox{
						AssignTo:   &app.keyboardCheck,
						Text:       "Forward keyboard",
						Checked:    app.cfg.CaptureKeyboard,
						ColumnSpan: 2,
					},
					RadioButtonGroup{
						Buttons: []RadioButton{
							{AssignTo: &app.autoRadio, Text: "Auto (switch at screen edge)"},
							{AssignTo: &app.manualRadio, Text: "Manual (hotkey only)"},
						},
					},
				},
			},
			GroupBox{
				Title:  "Device Layout",
				Layout: Grid{Columns: 4},
				Children: []Widget{
					Label{Text: "Device:"},
					ComboBox{
						AssignTo:              &app.deviceCombo,
						Editable:              true,
						ColumnSpan:            3,
						ToolTipText:           "Type part of a phone or tablet name; matches drop down and the best one fills in the resolution",
						OnTextChanged:         app.deviceSearchChanged,
						OnCurrentIndexChanged: app.deviceMatchSelected,
					},
					Label{Text: "Device resolution:"},
					ComboBox{
						AssignTo:              &app.resCombo,
						Editable:              true,
						Model:                 SlaveResolutionChoices,
						CurrentIndex:          resIndex,
						ToolTipText:           ResolutionHint,
						OnTextChanged:         app.resolutionEdited,
						OnCurrentIndexChanged: app.resolutionEdited,
					},
					Label{Text: "Orientation:"},
					ComboBox{
						AssignTo:              &app.orientCombo,
						Model:                 OrientationChoices,
						CurrentIndex:          orientIndex,
						OnCurrentIndexChanged: app.orientationChanged,
					},
					CustomWidget{
						AssignTo:            &app.arrangeWidget,
						ColumnSpan:          4,
						MinSize:             Size{Height: 130},
						ToolTipText:         "Drag the device to the side of your displays it sits on.",
						PaintPixels:         app.paintArrangement,
						PaintMode:           PaintBuffered,
						InvalidatesOnResize: true,
						OnSizeChanged:       app.arrangeResized,
						OnMouseDown: func(x, y int, button walk.MouseButton) {
							if button == walk.LeftButton && app.arranger.beginDrag(float64(x), float64(y)) {
								app.arrangeWidget.Invalidate()
							}
						},
						OnMouseMove: func(x, y int, button walk.MouseButton) {
							if button&walk.LeftButton != 0 {
								app.arranger.drag(float64(x), float64(y))
								app.arrangeWidget.Invalidate()
							}
						},
						OnMouseUp: func(x, y int, button walk.MouseButton) {
							if button == walk.LeftButton {
								app.arranger.endDrag(float64(x), float64(y))
								app.arrangeWidget.Invalidate()
							}
						},
					},
					Label{
						Text:       ResolutionHint,
						ColumnSpan: 4,
						TextColor:  walk.RGB(0x6e, 0x6e, 0x6e),
					},
				},
			},
			VSpacer{},
			// Footer: the running version, and the update check where it is
			// easy to find without unbalancing the status group's buttons.
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{Text: VersionText(app.version), TextColor: walk.RGB(0x6e, 0x6e, 0x6e)},
					HSpacer{},
					PushButton{Text: "Check for updates…", OnClicked: func() { app.updater.checkNow() }},
				},
			},
		},
	}
	if err := window.Create(); err != nil {
		return err
	}
	if resIndex < 0 {
		app.resCombo.SetText(resValue)
	}
	app.arranger = newArranger(app.cfg.HostSide, resValue)
	app.arranger.setDisplays(hostDisplays())
	app.arrangeResized()
	if app.cfg.AutoSwitch {
		app.autoRadio.SetChecked(true)
	} else {
		app.manualRadio.SetChecked(true)
	}
	app.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if !app.exiting {
			*canceled = true
			app.mw.Hide()
		}
	})
	return nil
}

func (app *gui) loadIcons() {
	if icon, err := walk.NewIconFromResourceId(iconResourceIDApp); err == nil {
		app.iconApp = icon
		app.mw.SetIcon(icon)
	}
	if icon, err := walk.NewIconFromResourceId(iconResourceIDRemoteMode); err == nil {
		app.iconRemote = icon
	}
}

func (app *gui) setupTray() {
	trayIcon, err := walk.NewNotifyIcon(app.mw)
	if err != nil {
		log.Printf("tray icon unavailable: %v", err)
		return
	}
	app.trayIcon = trayIcon
	_ = trayIcon.SetToolTip("ESP HID Bridge")
	if app.iconApp != nil {
		_ = trayIcon.SetIcon(app.iconApp)
	}
	trayIcon.MouseDown().Attach(func(_, _ int, button walk.MouseButton) {
		if button == walk.LeftButton {
			app.mw.Show()
		}
	})
	openAction := walk.NewAction()
	_ = openAction.SetText("Open")
	openAction.Triggered().Attach(func() { app.mw.Show() })
	_ = trayIcon.ContextMenu().Actions().Add(openAction)
	exitAction := walk.NewAction()
	_ = exitAction.SetText("Exit")
	exitAction.Triggered().Attach(func() {
		app.exiting = true
		app.mw.Close()
	})
	_ = trayIcon.ContextMenu().Actions().Add(exitAction)
	_ = trayIcon.SetVisible(true)
}

// The device-layout controls feed one another: a picked device or a flipped
// orientation writes the resolution field, and an edited resolution moves the
// orientation picker. Every write from here goes through setResolution or is
// wrapped in syncing so it does not come back round as a user edit.

// deviceSearchChanged runs on every keystroke in the device box: the list is
// refilled with the matches and dropped open under the text. Replacing the
// list (CB_RESETCONTENT) also wipes the edit box, so the typed text and the
// caret are put back before anything is shown. No row is selected here —
// CB_SETCURSEL would copy the row's text over what is being typed — the
// user reaches the rows with Down or the mouse.
func (app *gui) deviceSearchChanged() {
	if app.syncing {
		return
	}
	text := app.deviceCombo.Text()
	start, end := app.deviceCombo.TextSelection()
	app.deviceMatches = DeviceMatches(text)
	labels := make([]string, len(app.deviceMatches))
	for i, m := range app.deviceMatches {
		labels[i] = m.Label
	}
	app.syncing = true
	_ = app.deviceCombo.SetModel(labels)
	_ = app.deviceCombo.SetText(text)
	app.deviceCombo.SetTextSelection(start, end)
	show := uintptr(0)
	if len(labels) > 0 {
		show = 1
	}
	app.deviceCombo.SendMessage(win.CB_SHOWDROPDOWN, show, 0)
	app.syncing = false
	// The best match fills the field as the user types, so the picker never
	// shows a device whose size is not the one about to be saved.
	if len(app.deviceMatches) > 0 {
		app.setResolution(app.deviceMatches[0].Resolution)
	}
}

func (app *gui) deviceMatchSelected() {
	if app.syncing {
		return
	}
	i := app.deviceCombo.CurrentIndex()
	if i < 0 || i >= len(app.deviceMatches) {
		return
	}
	app.setResolution(app.deviceMatches[i].Resolution)
}

func (app *gui) orientationChanged() {
	if app.syncing {
		return
	}
	if flipped, ok := OrientResolution(app.resolutionText(), app.orientCombo.CurrentIndex()); ok {
		app.setResolution(flipped)
	}
}

func (app *gui) resolutionEdited() {
	if app.syncing {
		return
	}
	text := app.resolutionText()
	if index := OrientationIndexOf(text); index >= 0 {
		app.syncing = true
		_ = app.orientCombo.SetCurrentIndex(index)
		app.syncing = false
	}
	app.arranger.setDevice(text)
	app.arrangeWidget.Invalidate()
}

// resolutionText is the field's value as of the current event. When a preset
// was just picked from the list the edit box may not have been updated yet,
// so the list item is authoritative; typed text has no list index.
func (app *gui) resolutionText() string {
	if i := app.resCombo.CurrentIndex(); i >= 0 && i < len(SlaveResolutionChoices) {
		return SlaveResolutionChoices[i]
	}
	return app.resCombo.Text()
}

// setResolution writes the field and moves the orientation picker to match.
func (app *gui) setResolution(value string) {
	app.syncing = true
	_ = app.resCombo.SetText(value)
	if index := OrientationIndexOf(value); index >= 0 {
		_ = app.orientCombo.SetCurrentIndex(index)
	}
	app.syncing = false
	app.arranger.setDevice(value)
	app.arrangeWidget.Invalidate()
}

// arrangeResized gives the model the widget's pixel size (mouse events and
// PaintPixels are both in native pixels) and re-reads the displays, which
// is also how a monitor plugged in mid-session shows up.
func (app *gui) arrangeResized() {
	if app.arranger == nil || app.arrangeWidget == nil {
		return
	}
	bounds := app.arrangeWidget.ClientBoundsPixels()
	app.arranger.setDisplays(hostDisplays())
	app.arranger.setCanvas(float64(bounds.Width), float64(bounds.Height))
	app.arrangeWidget.Invalidate()
}

// paintArrangement draws arranger's frame: a well, the displays in blue with
// their names (the primary with a menu-bar stripe), the device in orange.
// GDI objects are made and disposed per paint; it runs on user action, not
// per frame.
func (app *gui) paintArrangement(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := app.arrangeWidget.ClientBoundsPixels()
	frame := app.arranger.frameNow()
	enabled := app.arrangeWidget.Enabled()

	fill := func(color walk.Color, r rectF, rounded bool) {
		brush, err := walk.NewSolidColorBrush(color)
		if err != nil {
			return
		}
		defer brush.Dispose()
		rect := walk.Rectangle{X: int(r.X), Y: int(r.Y), Width: int(r.W + 0.5), Height: int(r.H + 0.5)}
		if rounded {
			_ = canvas.FillRoundedRectanglePixels(brush, rect, walk.Size{Width: 6, Height: 6})
		} else {
			_ = canvas.FillRectanglePixels(brush, rect)
		}
	}
	outline := func(color walk.Color, r rectF) {
		pen, err := walk.NewCosmeticPen(walk.PenSolid, color)
		if err != nil {
			return
		}
		defer pen.Dispose()
		rect := walk.Rectangle{X: int(r.X), Y: int(r.Y), Width: int(r.W + 0.5), Height: int(r.H + 0.5)}
		_ = canvas.DrawRoundedRectanglePixels(pen, rect, walk.Size{Width: 6, Height: 6})
	}
	label := func(text string, color walk.Color, r rectF) {
		if r.W < 44 || r.H < 16 {
			return
		}
		rect := walk.Rectangle{X: int(r.X) + 2, Y: int(r.Y), Width: int(r.W) - 4, Height: int(r.H)}
		_ = canvas.DrawTextPixels(text, app.arrangeWidget.Font(), color, rect,
			walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
	}
	// Muted when disabled (the bridge is running and the settings are locked).
	mix := func(c walk.Color) walk.Color {
		if enabled {
			return c
		}
		r, g, b := byte(c), byte(c>>8), byte(c>>16)
		return walk.RGB((r+0xf0)/2, (g+0xf0)/2, (b+0xf0)/2)
	}

	fill(walk.RGB(0xf0, 0xf0, 0xf0), rectF{X: 0, Y: 0, W: float64(bounds.Width), H: float64(bounds.Height)}, false)
	for _, d := range frame.Displays {
		fill(mix(walk.RGB(0x4a, 0x90, 0xd9)), d.Rect, true)
		outline(mix(walk.RGB(0x2b, 0x6c, 0xb0)), d.Rect)
		if d.Primary && d.Rect.H > 12 {
			fill(mix(walk.RGB(0xff, 0xff, 0xff)), rectF{X: d.Rect.X + 1, Y: d.Rect.Y + 1, W: d.Rect.W - 2, H: 3}, false)
		}
		label(d.Name, mix(walk.RGB(0xff, 0xff, 0xff)), d.Rect)
	}
	device := frame.Device.Rect
	fill(mix(walk.RGB(0xf0, 0x8a, 0x24)), device, true)
	outline(mix(walk.RGB(0xc2, 0x66, 0x0e)), device)
	label(frame.Device.Name, mix(walk.RGB(0xff, 0xff, 0xff)), device)
	return nil
}

// monitorInfoEx is MONITORINFOEXW: lxn/win's MONITORINFO plus the device
// name, which GetMonitorInfoW fills when cbSize says there is room for it.
type monitorInfoEx struct {
	win.MONITORINFO
	Device [win.CCHDEVICENAME]uint16
}

var (
	guiUser32              = windows.NewLazySystemDLL("user32.dll")
	procGuiEnumDisplayMons = guiUser32.NewProc("EnumDisplayMonitors")
	procGuiGetMonitorInfoW = guiUser32.NewProc("GetMonitorInfoW")
)

// hostDisplays enumerates the monitors in virtual-screen pixels, y down —
// the space the capture layer's EnumDisplayMonitors rects live in — with
// the primary flag, the device name (\\.\DISPLAY1 without the prefix), and
// the physical size GetDeviceCaps reports for the device, which is 0 when
// the driver does not know.
func hostDisplays() []Display {
	var displays []Display
	callback := windows.NewCallback(func(hMonitor uintptr, _ uintptr, _ *win.RECT, _ uintptr) uintptr {
		var info monitorInfoEx
		info.CbSize = uint32(unsafe.Sizeof(info))
		if ok, _, _ := procGuiGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
			return 1
		}
		rc := info.RcMonitor
		if rc.Right <= rc.Left || rc.Bottom <= rc.Top {
			return 1
		}
		device := windows.UTF16ToString(info.Device[:])
		d := Display{
			Name:    strings.TrimPrefix(device, `\\.\`),
			X:       float64(rc.Left),
			Y:       float64(rc.Top),
			W:       float64(rc.Right - rc.Left),
			H:       float64(rc.Bottom - rc.Top),
			Primary: info.DwFlags&win.MONITORINFOF_PRIMARY != 0,
		}
		if name, err := windows.UTF16PtrFromString(device); err == nil {
			if hdc := win.CreateDC(name, name, nil, nil); hdc != 0 {
				d.WidthMM = float64(win.GetDeviceCaps(hdc, win.HORZSIZE))
				d.HeightMM = float64(win.GetDeviceCaps(hdc, win.VERTSIZE))
				win.DeleteDC(hdc)
			}
		}
		displays = append(displays, d)
		return 1
	})
	procGuiEnumDisplayMons.Call(0, 0, callback, 0)
	return displays
}

// showUpdate is the updater's notice callback: text in the status group
// with the install button beside it, or nothing at all.
func (app *gui) showUpdate(text string, installable bool) {
	app.updateLabel.SetText(text)
	app.updateButton.SetVisible(installable)
	app.updateRow.SetVisible(text != "")
}

func (app *gui) toggleAutoUpdates() {
	app.cfg.CheckUpdates = app.autoUpdateAction.Checked()
	app.updater.setEnabled(app.cfg.CheckUpdates)
	if err := config.Save(app.cfg); err != nil {
		log.Printf("settings save failed: %v", err)
	}
}

func (app *gui) readConfigFromForm() error {
	values := FormValues{
		ToggleHotkey:    app.hotkeyEdit.Text(),
		MoveRateHz:      app.rateEdit.Text(),
		Resolution:      app.resCombo.Text(),
		HostSideIndex:   app.arranger.sideIndex(),
		CaptureKeyboard: app.keyboardCheck.Checked(),
		AutoSwitch:      app.autoRadio.Checked(),
	}
	return values.Apply(&app.cfg)
}

func (app *gui) startBridge() {
	if app.runtime.Running() {
		return
	}
	if err := app.readConfigFromForm(); err != nil {
		walk.MsgBox(app.mw, "Invalid settings", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	if err := config.Save(app.cfg); err != nil {
		log.Printf("settings save failed: %v", err)
	}
	if err := app.runtime.Start(app.cfg); err != nil {
		walk.MsgBox(app.mw, "Start failed", err.Error(), walk.MsgBoxIconError)
		return
	}
	app.setRunning(true)
}

func (app *gui) stopBridge() {
	if !app.runtime.Running() {
		return
	}
	go func() {
		app.runtime.Stop()
	}()
}

func (app *gui) clearBonds() {
	if !app.runtime.Running() {
		walk.MsgBox(app.mw, "Not running",
			"Start the bridge first so the device is connected.", walk.MsgBoxIconInformation)
		return
	}
	app.runtime.ClearBonds()
	walk.MsgBox(app.mw, "Bonds cleared",
		"The device forgot all paired phones.\n\nOn the phone: forget \"ESP-HID-ME\" in Bluetooth settings, then pair again.",
		walk.MsgBoxIconInformation)
}

// forgetDevice drops the learned binding so the next Start re-identifies the
// bridge by handshake. This is the way out of "never substitute": swap in a
// replacement board, forget, start, and it binds to the new one.
func (app *gui) forgetDevice() {
	if app.runtime.Running() {
		walk.MsgBox(app.mw, "Stop first",
			"Stop the bridge before forgetting the device.", walk.MsgBoxIconInformation)
		return
	}
	app.cfg.DeviceSerial = ""
	if err := config.Save(app.cfg); err != nil {
		log.Printf("settings save failed: %v", err)
	}
	app.portLabel.SetText("-")
	app.statusLabel.SetText("Device forgotten — press Start to detect again")
}

func (app *gui) setRunning(running bool) {
	app.startButton.SetEnabled(!running)
	app.stopButton.SetEnabled(running)
	app.forgetButton.SetEnabled(!running)
	app.hotkeyEdit.SetEnabled(!running)
	app.rateEdit.SetEnabled(!running)
	app.keyboardCheck.SetEnabled(!running)
	app.autoRadio.SetEnabled(!running)
	app.manualRadio.SetEnabled(!running)
	app.resCombo.SetEnabled(!running)
	app.arrangeWidget.SetEnabled(!running)
	app.arrangeWidget.Invalidate()
	app.deviceCombo.SetEnabled(!running)
	app.orientCombo.SetEnabled(!running)
}

func (app *gui) consumeEvents() {
	for event := range app.events {
		event := event
		app.mw.Synchronize(func() {
			app.applyEvent(event)
		})
	}
}

func (app *gui) applyEvent(event bridge.Event) {
	switch event.Kind {
	case bridge.EventStarting:
		app.statusLabel.SetText("Starting — looking for device…")
	case bridge.EventSerialConnected:
		app.statusLabel.SetText("Running")
		app.portLabel.SetText(DeviceText(event.Serial, event.Port))
	case bridge.EventSerialDown:
		app.statusLabel.SetText("Waiting for device (USB)…")
		app.portLabel.SetText("-")
		app.bleLabel.SetText("-")
	case bridge.EventDiscovering:
		app.statusLabel.SetText("Identifying device…")
		log.Printf("device discovery: %s", event.Detail)
	case bridge.EventDeviceLearned:
		// Remember which board this is. Without this the next launch would
		// rediscover it, and re-probe the user's other ESP32s to do so.
		app.cfg.DeviceSerial = event.Serial
		if err := config.SaveDeviceSerial(event.Serial); err != nil {
			log.Printf("could not save device binding: %v", err)
		}
		app.portLabel.SetText(DeviceText(event.Serial, event.Port))
	case bridge.EventDeviceAbsent:
		app.statusLabel.SetText("Bridge not connected")
		app.portLabel.SetText(DeviceText(event.Serial, "") + " (absent)")
		app.bleLabel.SetText("-")
		log.Printf("bridge absent: %s", event.Detail)
	case bridge.EventDeviceAmbiguous:
		app.statusLabel.SetText("Several bridges found — leave one attached")
		app.portLabel.SetText("-")
		log.Printf("ambiguous bridge: %s", event.Detail)
	case bridge.EventHello:
		app.fwLabel.SetText(FirmwareText(event.Hello))
	case bridge.EventBleState:
		app.bleLabel.SetText(BleStateText(event.BleState))
	case bridge.EventDeviceError:
		log.Printf("device error: %s", event.Detail)
	case bridge.EventRemoteMode:
		if app.trayIcon != nil {
			if event.Active && app.iconRemote != nil {
				_ = app.trayIcon.SetIcon(app.iconRemote)
			} else if app.iconApp != nil {
				_ = app.trayIcon.SetIcon(app.iconApp)
			}
		}
	case bridge.EventCaptureError:
		app.statusLabel.SetText("Capture error")
		walk.MsgBox(app.mw, "Capture error", event.Detail, walk.MsgBoxIconError)
		app.setRunning(false)
	case bridge.EventStopped:
		app.statusLabel.SetText("Stopped")
		app.portLabel.SetText("-")
		app.bleLabel.SetText("-")
		app.setRunning(false)
	case bridge.EventLog:
		log.Printf("device: %s", event.Detail)
	}
}
