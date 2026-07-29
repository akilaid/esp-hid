//go:build windows

// Package ui is the walk-based Windows GUI: connection + BLE status, input
// settings, and device maintenance (bond clearing). Adapted from the legacy
// software/gui_windows.go, with a BLE status line fed by device reports —
// the diagnostic the old system never had.
package ui

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/lxn/walk"
	//lint:ignore ST1001 walk's declarative DSL is designed for dot import
	. "github.com/lxn/walk/declarative"

	"esp-hid/host/internal/bridge"
	"esp-hid/host/internal/config"
	"esp-hid/host/internal/hotkey"
	"esp-hid/host/internal/protocol"
)

const (
	iconResourceIDApp        = 1
	iconResourceIDRemoteMode = 2
)

var slaveResolutionChoices = []string{
	"1280x720", "1366x768", "1600x900", "1920x1080", "2560x1440", "3840x2160",
	"720x1280", "768x1366", "900x1600", "1080x1920", "1440x2560", "2160x3840",
}

var hostSideChoices = []string{"left", "right", "top", "bottom"}

type gui struct {
	cfg     config.Config
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
	hotkeyEdit    *walk.LineEdit
	rateEdit      *walk.LineEdit
	keyboardCheck *walk.CheckBox
	autoRadio     *walk.RadioButton
	manualRadio   *walk.RadioButton
	resCombo      *walk.ComboBox
	sideCombo     *walk.ComboBox

	trayIcon   *walk.NotifyIcon
	iconApp    *walk.Icon
	iconRemote *walk.Icon
	exiting    bool
}

// Run builds the window and enters the message loop.
func Run(cfg config.Config) error {
	app := &gui{
		cfg:    cfg,
		events: make(chan bridge.Event, 256),
	}
	app.runtime = bridge.New(app.events)

	if err := app.build(); err != nil {
		return err
	}
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
	sideIndex := indexOf(hostSideChoices, app.cfg.HostSide)
	resValue := fmt.Sprintf("%dx%d", app.cfg.SlaveWidth, app.cfg.SlaveHeight)
	resIndex := indexOf(slaveResolutionChoices, resValue)

	window := MainWindow{
		AssignTo: &app.mw,
		Title:    "ESP HID Bridge",
		MinSize:  Size{Width: 560, Height: 380},
		Size:     Size{Width: 580, Height: 400},
		Layout:   VBox{},
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
						Layout:     HBox{MarginsZero: true},
						ColumnSpan: 2,
						Children: []Widget{
							PushButton{AssignTo: &app.startButton, Text: "Start", OnClicked: app.startBridge},
							PushButton{AssignTo: &app.stopButton, Text: "Stop", Enabled: false, OnClicked: app.stopBridge},
							HSpacer{},
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
					Label{Text: "Device resolution:"},
					ComboBox{
						AssignTo:     &app.resCombo,
						Editable:     true,
						Model:        slaveResolutionChoices,
						CurrentIndex: resIndex,
					},
					Label{Text: "This PC sits:"},
					ComboBox{
						AssignTo:     &app.sideCombo,
						Model:        hostSideChoices,
						CurrentIndex: sideIndex,
					},
				},
			},
			VSpacer{},
		},
	}
	if err := window.Create(); err != nil {
		return err
	}
	if resIndex < 0 {
		app.resCombo.SetText(resValue)
	}
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

func (app *gui) readConfigFromForm() error {
	combo, ok := hotkey.Normalize(strings.TrimSpace(app.hotkeyEdit.Text()))
	if !ok {
		return fmt.Errorf("invalid hotkey %q (examples: F9, Ctrl+Alt+F7)", app.hotkeyEdit.Text())
	}
	app.cfg.ToggleHotkey = combo

	rate, err := strconv.Atoi(strings.TrimSpace(app.rateEdit.Text()))
	if err != nil || rate <= 0 || rate > 500 {
		return fmt.Errorf("send rate must be a number between 1 and 500")
	}
	app.cfg.MoveRateHz = rate

	width, height, err := config.ParseResolution(app.resCombo.Text())
	if err != nil {
		return err
	}
	app.cfg.SlaveWidth = width
	app.cfg.SlaveHeight = height

	if index := app.sideCombo.CurrentIndex(); index >= 0 && index < len(hostSideChoices) {
		app.cfg.HostSide = hostSideChoices[index]
	}
	app.cfg.CaptureKeyboard = app.keyboardCheck.Checked()
	app.cfg.AutoSwitch = app.autoRadio.Checked()
	return app.cfg.Validate()
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

func (app *gui) setRunning(running bool) {
	app.startButton.SetEnabled(!running)
	app.stopButton.SetEnabled(running)
	app.hotkeyEdit.SetEnabled(!running)
	app.rateEdit.SetEnabled(!running)
	app.keyboardCheck.SetEnabled(!running)
	app.autoRadio.SetEnabled(!running)
	app.manualRadio.SetEnabled(!running)
	app.resCombo.SetEnabled(!running)
	app.sideCombo.SetEnabled(!running)
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
		app.portLabel.SetText(event.Port)
	case bridge.EventSerialDown:
		app.statusLabel.SetText("Waiting for device (USB)…")
		app.portLabel.SetText("-")
		app.bleLabel.SetText("-")
	case bridge.EventHello:
		hello := event.Hello
		app.fwLabel.SetText(fmt.Sprintf("%d.%d.%d (protocol v%d)",
			hello.FwMajor, hello.FwMinor, hello.FwPatch, hello.ProtoVersion))
	case bridge.EventBleState:
		app.bleLabel.SetText(bleStateText(event.BleState))
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

func bleStateText(state protocol.BleState) string {
	switch state.State {
	case protocol.BleConnected:
		return fmt.Sprintf("Connected (%d paired)", state.BondCount)
	case protocol.BleAdvertising:
		if state.BondCount == 0 {
			return "Advertising — pair the phone with \"ESP-HID-ME\""
		}
		return fmt.Sprintf("Advertising — waiting for phone (%d paired)", state.BondCount)
	case protocol.BleIdle:
		return "Bluetooth starting…"
	default:
		return "Unknown"
	}
}

func indexOf(values []string, value string) int {
	for i, v := range values {
		if strings.EqualFold(v, value) {
			return i
		}
	}
	return -1
}
