//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type guiApp struct {
	baseCfg config
	runtime *bridgeRuntime

	statusCh  chan bridgeEvent
	eventStop chan struct{}

	mw              *walk.MainWindow
	portCombo       *walk.ComboBox
	toggleCombo     *walk.ComboBox
	keyboardCheck   *walk.CheckBox
	rateEdit        *walk.LineEdit
	statusLabel     *walk.Label
	activePortLabel *walk.Label
	startButton     *walk.PushButton
	stopButton      *walk.PushButton
}

func runGUI(initial config) error {
	deactivateActCtx, err := activateCommonControlsV6()
	if err != nil {
		return fmt.Errorf("windows ui activation failed: %w", err)
	}
	defer deactivateActCtx()

	app := &guiApp{
		baseCfg:   initial,
		statusCh:  make(chan bridgeEvent, 256),
		eventStop: make(chan struct{}),
	}

	if err := app.buildWindow(); err != nil {
		return err
	}

	app.refreshPorts()
	if app.toggleCombo != nil {
		toggleName := app.baseCfg.toggleHotkeyName
		if normalized, ok := normalizeToggleHotkeyName(toggleName); ok {
			app.toggleCombo.SetText(normalized)
		} else {
			app.toggleCombo.SetText(defaultToggleHotkeyName)
		}
	}
	app.setRunning(false)

	go app.consumeEvents()
	go app.backgroundRefresh()

	app.mw.Run()
	close(app.eventStop)
	app.stopRuntimeAndWait()

	return nil
}

func (app *guiApp) buildWindow() error {
	return MainWindow{
		AssignTo: &app.mw,
		Title:    "ESP HID Bridge",
		MinSize:  Size{Width: 520, Height: 140},
		Size:     Size{Width: 560, Height: 160},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Serial Port:"},
					ComboBox{
						AssignTo:     &app.portCombo,
						Model:        []string{"auto"},
						Editable:     false,
						MaxSize:      Size{Width: 150},
						MinSize:      Size{Width: 130},
						CurrentIndex: 0,
					},
					PushButton{
						Text: "Refresh Devices",
						OnClicked: func() {
							app.refreshPorts()
						},
					},
					HSpacer{},
					PushButton{
						AssignTo:  &app.startButton,
						Text:      "Start",
						OnClicked: app.onStart,
					},
					PushButton{
						AssignTo:  &app.stopButton,
						Text:      "Stop",
						OnClicked: app.onStop,
					},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Toggle:"},
					ComboBox{
						AssignTo:     &app.toggleCombo,
						Model:        toggleHotkeyChoices,
						Editable:     false,
						CurrentIndex: 8,
						MinSize:      Size{Width: 68},
						MaxSize:      Size{Width: 68},
					},
					CheckBox{
						AssignTo: &app.keyboardCheck,
						Text:     "Forward keyboard",
						Checked:  app.baseCfg.captureKeyboard,
					},
					Label{Text: "Rate:"},
					LineEdit{
						AssignTo: &app.rateEdit,
						Text:     strconv.Itoa(app.baseCfg.moveRateHz),
						MaxSize:  Size{Width: 80},
					},
					HSpacer{},
					Label{Text: "Bridge:"},
					Label{
						AssignTo: &app.statusLabel,
						Text:     "Stopped",
					},
					Label{Text: "Connected Port:"},
					Label{
						AssignTo: &app.activePortLabel,
						Text:     "-",
					},
				},
			},
		},
	}.Create()
}

func (app *guiApp) onStart() {
	cfg, err := app.readConfigFromForm()
	if err != nil {
		walk.MsgBox(app.mw, "Invalid Settings", err.Error(), walk.MsgBoxIconError)
		return
	}

	if app.runtime != nil && app.runtime.Running() {
		return
	}

	app.runtime = newBridgeRuntime(cfg, app.pushEvent)
	if err := app.runtime.Start(); err != nil {
		walk.MsgBox(app.mw, "Start Failed", err.Error(), walk.MsgBoxIconError)
		return
	}

	app.setRunning(true)
	app.statusLabel.SetText("Starting")
}

func (app *guiApp) onStop() {
	go app.stopRuntimeAndWait()
}

func (app *guiApp) stopRuntimeAndWait() {
	if app.runtime == nil {
		return
	}

	app.runtime.Stop()
	app.runtime.Wait()
}

func (app *guiApp) pushEvent(event bridgeEvent) {
	select {
	case app.statusCh <- event:
	default:
	}
}

func (app *guiApp) consumeEvents() {
	for {
		select {
		case <-app.eventStop:
			return
		case event := <-app.statusCh:
			if app.mw == nil {
				continue
			}

			app.mw.Synchronize(func() {
				app.applyEvent(event)
			})
		}
	}
}

func (app *guiApp) applyEvent(event bridgeEvent) {
	switch event.Type {
	case bridgeEventStarting:
		app.statusLabel.SetText("Starting")
	case bridgeEventStopping:
		app.statusLabel.SetText("Stopping")
	case bridgeEventStopped:
		app.statusLabel.SetText("Stopped")
		app.activePortLabel.SetText("-")
		app.setRunning(false)
	case bridgeEventSerialConnected:
		app.statusLabel.SetText("Connected")
		if event.Port != "" {
			app.activePortLabel.SetText(event.Port)
		}
		app.setRunning(true)
	case bridgeEventSerialOpenFailed:
		app.statusLabel.SetText("Waiting for device")
	case bridgeEventSerialWriteError:
		app.statusLabel.SetText("Connection issue")
	case bridgeEventCaptureError:
		app.statusLabel.SetText("Capture error")
	}
}

func (app *guiApp) refreshPorts() {
	ports, err := listSerialPorts()
	items := []string{"auto"}
	if err == nil {
		items = append(items, ports...)
	}

	current := strings.TrimSpace(app.portCombo.Text())
	_ = app.portCombo.SetModel(items)

	if current == "" {
		if app.baseCfg.autoPort {
			app.portCombo.SetText("auto")
		} else {
			app.portCombo.SetText(app.baseCfg.portName)
		}
		return
	}

	for _, item := range items {
		if strings.EqualFold(item, current) {
			app.portCombo.SetText(item)
			return
		}
	}

	app.portCombo.SetText(items[0])
}

func (app *guiApp) backgroundRefresh() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.eventStop:
			return
		case <-ticker.C:
			if app.mw == nil {
				continue
			}

			app.mw.Synchronize(func() {
				if app.runtime == nil || !app.runtime.Running() {
					app.refreshPorts()
				}
			})
		}
	}
}

func (app *guiApp) readConfigFromForm() (config, error) {
	cfg := app.baseCfg
	cfg.guiMode = true
	cfg.captureKeyboard = app.keyboardCheck.Checked()

	toggleName, ok := normalizeToggleHotkeyName(app.toggleCombo.Text())
	if !ok {
		return cfg, fmt.Errorf("invalid toggle hotkey: %q", app.toggleCombo.Text())
	}
	toggleVK, _ := toggleHotkeyNameToVK(toggleName)
	cfg.toggleHotkeyName = toggleName
	cfg.toggleHotkeyVK = toggleVK

	port := strings.TrimSpace(app.portCombo.Text())
	if port == "" || strings.EqualFold(port, "auto") {
		cfg.portName = "auto"
		cfg.autoPort = true
	} else {
		cfg.portName = port
		cfg.autoPort = false
	}

	rateText := strings.TrimSpace(app.rateEdit.Text())
	rate, err := strconv.Atoi(rateText)
	if err != nil || rate <= 0 {
		return cfg, fmt.Errorf("invalid rate: %q", rateText)
	}
	cfg.moveRateHz = rate

	return cfg, nil
}

func (app *guiApp) setRunning(running bool) {
	if app.startButton != nil {
		app.startButton.SetEnabled(!running)
	}
	if app.stopButton != nil {
		app.stopButton.SetEnabled(running)
	}
	if app.portCombo != nil {
		app.portCombo.SetEnabled(!running)
	}
	if app.toggleCombo != nil {
		app.toggleCombo.SetEnabled(!running)
	}
	if app.keyboardCheck != nil {
		app.keyboardCheck.SetEnabled(!running)
	}
	if app.rateEdit != nil {
		app.rateEdit.SetEnabled(!running)
	}
}
