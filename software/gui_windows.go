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
	exiting   bool

	mw               *walk.MainWindow
	trayIcon         *walk.NotifyIcon
	trayFallbackIcon *walk.Icon
	portCombo        *walk.ComboBox
	toggleCombo      *walk.ComboBox
	keyboardCheck    *walk.CheckBox
	rateEdit         *walk.LineEdit
	statusWidget     *walk.CustomWidget
	statusText       string
	statusColor      walk.Color
	activePortLabel  *walk.Label
	startButton      *walk.PushButton
	stopButton       *walk.PushButton
}

type connectionIndicatorState int

const (
	connectionIndicatorWaiting connectionIndicatorState = iota
	connectionIndicatorConnected
	connectionIndicatorFailed
)

var (
	connectionDotColorConnected = walk.RGB(46, 185, 89)
	connectionDotColorWaiting   = walk.RGB(209, 154, 30)
	connectionDotColorFailed    = walk.RGB(214, 69, 65)
)

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

	if err := app.setupTray(); err != nil {
		if app.mw != nil {
			app.mw.Dispose()
		}
		return err
	}
	defer app.disposeTray()

	app.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if app.exiting {
			return
		}

		*canceled = true
		app.hideWindowToTray()
	})

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
	app.setStatusText("Stopped")
	app.setConnectionIndicator(connectionIndicatorWaiting)

	go app.consumeEvents()
	go app.backgroundRefresh()

	app.mw.Run()
	close(app.eventStop)
	app.stopRuntimeAndWait()

	return nil
}

func (app *guiApp) setConnectionIndicator(state connectionIndicatorState) {
	switch state {
	case connectionIndicatorConnected:
		app.statusColor = connectionDotColorConnected
	case connectionIndicatorFailed:
		app.statusColor = connectionDotColorFailed
	default:
		app.statusColor = connectionDotColorWaiting
	}

	if app.statusWidget != nil {
		app.statusWidget.Invalidate()
	}
}

func (app *guiApp) setStatusText(text string) {
	app.statusText = text

	if app.statusWidget != nil {
		app.statusWidget.Invalidate()
	}
}

func (app *guiApp) paintStatusText(canvas *walk.Canvas, bounds walk.Rectangle) error {
	if app.statusText == "" {
		return nil
	}

	font := app.mw.Font()
	if app.statusWidget != nil && app.statusWidget.Font() != nil {
		font = app.statusWidget.Font()
	}
	if font == nil {
		return nil
	}

	format := walk.TextLeft | walk.TextVCenter | walk.TextSingleLine | walk.TextEndEllipsis
	return canvas.DrawTextPixels(app.statusText, font, app.statusColor, bounds, format)
}

func (app *guiApp) setupTray() error {
	if app.mw == nil {
		return fmt.Errorf("main window is not initialized")
	}

	tray, err := walk.NewNotifyIcon(app.mw)
	if err != nil {
		return fmt.Errorf("create tray icon: %w", err)
	}

	fail := func(inner error) error {
		_ = tray.Dispose()
		return inner
	}

	app.trayIcon = tray

	if icon := app.mw.Icon(); icon != nil {
		_ = tray.SetIcon(icon)
	} else {
		fallbackIcon, iconErr := walk.NewIconFromSysDLL("shell32", 3)
		if iconErr == nil {
			app.trayFallbackIcon = fallbackIcon
			_ = tray.SetIcon(fallbackIcon)
		}
	}

	if err := tray.SetToolTip("ESP HID Bridge"); err != nil {
		app.trayIcon = nil
		return fail(fmt.Errorf("set tray tooltip: %w", err))
	}

	tray.MouseDown().Attach(func(_ int, _ int, button walk.MouseButton) {
		if button == walk.LeftButton {
			app.showWindowFromTray()
		}
	})

	openAction := walk.NewAction()
	if err := openAction.SetText("Open"); err != nil {
		app.trayIcon = nil
		return fail(fmt.Errorf("create tray open action: %w", err))
	}
	openAction.Triggered().Attach(app.showWindowFromTray)

	exitAction := walk.NewAction()
	if err := exitAction.SetText("Exit"); err != nil {
		app.trayIcon = nil
		return fail(fmt.Errorf("create tray exit action: %w", err))
	}
	exitAction.Triggered().Attach(app.requestExit)

	if err := tray.ContextMenu().Actions().Add(openAction); err != nil {
		app.trayIcon = nil
		return fail(fmt.Errorf("add tray open action: %w", err))
	}
	if err := tray.ContextMenu().Actions().Add(walk.NewSeparatorAction()); err != nil {
		app.trayIcon = nil
		return fail(fmt.Errorf("add tray separator action: %w", err))
	}
	if err := tray.ContextMenu().Actions().Add(exitAction); err != nil {
		app.trayIcon = nil
		return fail(fmt.Errorf("add tray exit action: %w", err))
	}

	if err := tray.SetVisible(true); err != nil {
		app.trayIcon = nil
		return fail(fmt.Errorf("show tray icon: %w", err))
	}

	return nil
}

func (app *guiApp) disposeTray() {
	if app.trayIcon != nil {
		_ = app.trayIcon.Dispose()
		app.trayIcon = nil
	}

	if app.trayFallbackIcon != nil {
		app.trayFallbackIcon.Dispose()
		app.trayFallbackIcon = nil
	}
}

func (app *guiApp) showWindowFromTray() {
	if app.mw == nil {
		return
	}

	app.mw.Show()
	_ = app.mw.BringToTop()
	_ = app.mw.Activate()
	_ = app.mw.SetFocus()
}

func (app *guiApp) hideWindowToTray() {
	if app.mw == nil {
		return
	}

	app.mw.Hide()
}

func (app *guiApp) requestExit() {
	if app.mw == nil {
		return
	}

	app.exiting = true
	_ = app.mw.Close()
}

func (app *guiApp) buildWindow() error {
	return MainWindow{
		AssignTo: &app.mw,
		Title:    "ESP HID Bridge",
		MinSize:  Size{Width: 520, Height: 140},
		Size:     Size{Width: 560, Height: 160},
		Visible:  false,
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
					CustomWidget{
						AssignTo:    &app.statusWidget,
						PaintPixels: app.paintStatusText,
						MinSize:     Size{Width: 98, Height: 18},
						MaxSize:     Size{Width: 98, Height: 18},
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
	app.setStatusText("Starting")
	app.setConnectionIndicator(connectionIndicatorWaiting)
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
		app.setStatusText("Starting")
		app.setConnectionIndicator(connectionIndicatorWaiting)
	case bridgeEventStopping:
		app.setStatusText("Stopping")
		app.setConnectionIndicator(connectionIndicatorWaiting)
	case bridgeEventStopped:
		app.setStatusText("Stopped")
		app.activePortLabel.SetText("-")
		app.setConnectionIndicator(connectionIndicatorWaiting)
		app.setRunning(false)
	case bridgeEventSerialConnected:
		app.setStatusText("Connected")
		if event.Port != "" {
			app.activePortLabel.SetText(event.Port)
		}
		app.setConnectionIndicator(connectionIndicatorConnected)
		app.setRunning(true)
	case bridgeEventSerialOpenFailed:
		app.setStatusText("Waiting for device")
		app.setConnectionIndicator(connectionIndicatorFailed)
	case bridgeEventSerialWriteError:
		app.setStatusText("Connection issue")
		app.setConnectionIndicator(connectionIndicatorFailed)
	case bridgeEventCaptureError:
		app.setStatusText("Capture error")
		app.setConnectionIndicator(connectionIndicatorFailed)
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
