//go:build windows

package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	appIcon          *walk.Icon
	remoteModeIcon   *walk.Icon
	trayFallbackIcon *walk.Icon
	remoteModeActive bool
	portCombo        *walk.ComboBox
	toggleCombo      *walk.ComboBox
	keyboardCheck    *walk.CheckBox
	slaveResCombo    *walk.ComboBox
	hostSideWidget   *walk.CustomWidget
	selectedHostSide string
	draggingSlave    bool
	dragOffsetX      int
	dragOffsetY      int
	dragSlaveRect    walk.Rectangle
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

const (
	iconResourceIDApp        = 1
	iconResourceIDRemoteMode = 2
)

var (
	connectionDotColorConnected = walk.RGB(46, 185, 89)
	connectionDotColorWaiting   = walk.RGB(209, 154, 30)
	connectionDotColorFailed    = walk.RGB(214, 69, 65)

	hostSideBgColor        = walk.RGB(245, 247, 250)
	hostSideFrameColor     = walk.RGB(188, 196, 208)
	hostSideHostFillColor  = walk.RGB(23, 122, 205)
	hostSideHostLineColor  = walk.RGB(16, 87, 147)
	hostSideHostTextColor  = walk.RGB(255, 255, 255)
	hostSideSlaveFillColor = walk.RGB(231, 236, 244)
	hostSideSlaveLineColor = walk.RGB(120, 130, 146)
	hostSideSlaveTextColor = walk.RGB(66, 74, 90)
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

	app.applyWindowIcon()

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
	if app.slaveResCombo != nil {
		resolutionText := formatSlaveResolution(app.baseCfg.slaveWidth, app.baseCfg.slaveHeight)
		app.slaveResCombo.SetText(resolutionText)
	}
	if normalizedHostSide, ok := normalizeHostSide(app.baseCfg.hostSide); ok {
		app.selectedHostSide = normalizedHostSide
	} else {
		app.selectedHostSide = defaultHostSide
	}
	app.baseCfg.hostSide = app.selectedHostSide
	app.attachHostSideWidgetEvents()
	app.setRunning(false)
	app.setStatusText("Stopped")
	app.setConnectionIndicator(connectionIndicatorWaiting)

	go app.consumeEvents()
	go app.backgroundRefresh()

	if err := app.startBridge(false); err != nil {
		app.setStatusText("Start failed")
		app.setConnectionIndicator(connectionIndicatorFailed)
	}

	app.mw.Run()
	close(app.eventStop)
	app.stopRuntimeAndWait()

	return nil
}

func (app *guiApp) applyWindowIcon() {
	if app.mw == nil {
		return
	}

	if icon := app.loadIconFromResourceOrFile(iconResourceIDApp, "app.ico"); icon != nil {
		if err := app.mw.SetIcon(icon); err != nil {
			icon.Dispose()
		} else {
			app.appIcon = icon
		}
	}

	app.remoteModeIcon = app.loadIconFromResourceOrFile(iconResourceIDRemoteMode, "on.ico")
}

func (app *guiApp) loadIconFromResourceOrFile(resourceID int, iconName string) *walk.Icon {
	if icon, err := walk.NewIconFromResourceId(resourceID); err == nil {
		return icon
	}

	return app.loadIconFromCandidates(iconName)
}

func (app *guiApp) loadIconFromCandidates(iconName string) *walk.Icon {
	iconCandidates := []string{
		iconName,
		filepath.Join("software", iconName),
	}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		iconCandidates = append(iconCandidates,
			filepath.Join(exeDir, iconName),
			filepath.Join(exeDir, "software", iconName),
		)
	}

	for _, iconPath := range iconCandidates {
		icon, err := walk.NewIconFromFile(iconPath)
		if err != nil {
			continue
		}
		return icon
	}

	return nil
}

func (app *guiApp) setTrayIconForRemoteMode(active bool) {
	app.remoteModeActive = active

	if app.trayIcon == nil {
		return
	}

	if active && app.remoteModeIcon != nil {
		_ = app.trayIcon.SetIcon(app.remoteModeIcon)
		return
	}

	if icon := app.mw.Icon(); icon != nil {
		_ = app.trayIcon.SetIcon(icon)
		return
	}

	if app.trayFallbackIcon != nil {
		_ = app.trayIcon.SetIcon(app.trayFallbackIcon)
	}
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

func clampInt(value int, minValue int, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func rectContainsPoint(rect walk.Rectangle, x int, y int) bool {
	return x >= rect.X && x < rect.X+rect.Width && y >= rect.Y && y < rect.Y+rect.Height
}

func fitResolutionToBox(width int, height int, maxWidth int, maxHeight int) (int, int) {
	if maxWidth <= 0 || maxHeight <= 0 {
		return 1, 1
	}
	if width <= 0 || height <= 0 {
		return maxWidth, maxHeight
	}

	scale := math.Min(float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
	if scale <= 0 {
		return maxWidth, maxHeight
	}

	fittedWidth := int(math.Round(float64(width) * scale))
	fittedHeight := int(math.Round(float64(height) * scale))

	if fittedWidth < 28 {
		fittedWidth = 28
	}
	if fittedHeight < 28 {
		fittedHeight = 28
	}

	fittedWidth = clampInt(fittedWidth, 1, maxWidth)
	fittedHeight = clampInt(fittedHeight, 1, maxHeight)

	return fittedWidth, fittedHeight
}

func (app *guiApp) selectedHostSideOrDefault() string {
	if normalizedHostSide, ok := normalizeHostSide(app.selectedHostSide); ok {
		return normalizedHostSide
	}
	if normalizedHostSide, ok := normalizeHostSide(app.baseCfg.hostSide); ok {
		return normalizedHostSide
	}
	return defaultHostSide
}

func (app *guiApp) setSelectedHostSide(side string) {
	normalizedHostSide, ok := normalizeHostSide(side)
	if !ok {
		normalizedHostSide = defaultHostSide
	}

	if app.selectedHostSide == normalizedHostSide {
		return
	}

	app.selectedHostSide = normalizedHostSide
	app.baseCfg.hostSide = normalizedHostSide

	if app.hostSideWidget != nil {
		app.hostSideWidget.Invalidate()
	}
}

func (app *guiApp) slavePreviewResolution() (int, int) {
	if app.slaveResCombo != nil {
		if width, height, ok := parseSlaveResolution(app.slaveResCombo.Text()); ok {
			return width, height
		}
	}

	if app.baseCfg.slaveWidth > 0 && app.baseCfg.slaveHeight > 0 {
		return app.baseCfg.slaveWidth, app.baseCfg.slaveHeight
	}

	return defaultSlaveWidth, defaultSlaveHeight
}

func (app *guiApp) hostSideLayoutRects(bounds walk.Rectangle) (walk.Rectangle, walk.Rectangle, walk.Rectangle) {
	padding := 6
	layoutArea := walk.Rectangle{
		X:      bounds.X + padding,
		Y:      bounds.Y + padding,
		Width:  bounds.Width - 2*padding,
		Height: bounds.Height - 2*padding,
	}

	if layoutArea.Width < 80 {
		layoutArea.Width = 80
	}
	if layoutArea.Height < 64 {
		layoutArea.Height = 64
	}

	hostWidth := clampInt(layoutArea.Width/3, 58, 86)
	hostHeight := clampInt(layoutArea.Height/2, 40, 60)
	hostRect := walk.Rectangle{
		X:      layoutArea.X + (layoutArea.Width-hostWidth)/2,
		Y:      layoutArea.Y + (layoutArea.Height-hostHeight)/2,
		Width:  hostWidth,
		Height: hostHeight,
	}

	slaveWidth, slaveHeight := app.slavePreviewResolution()
	previewSlaveWidth, previewSlaveHeight := fitResolutionToBox(slaveWidth, slaveHeight, hostWidth, hostHeight+8)
	slaveRect := walk.Rectangle{Width: previewSlaveWidth, Height: previewSlaveHeight}

	gap := clampInt(layoutArea.Width/12, 10, 20)
	switch app.selectedHostSideOrDefault() {
	case hostSideRight:
		slaveRect.X = hostRect.X - gap - slaveRect.Width
		slaveRect.Y = hostRect.Y + (hostRect.Height-slaveRect.Height)/2
	case hostSideTop:
		slaveRect.X = hostRect.X + (hostRect.Width-slaveRect.Width)/2
		slaveRect.Y = hostRect.Y + hostRect.Height + gap
	case hostSideBottom:
		slaveRect.X = hostRect.X + (hostRect.Width-slaveRect.Width)/2
		slaveRect.Y = hostRect.Y - gap - slaveRect.Height
	default:
		slaveRect.X = hostRect.X + hostRect.Width + gap
		slaveRect.Y = hostRect.Y + (hostRect.Height-slaveRect.Height)/2
	}

	slaveRect.X = clampInt(slaveRect.X, layoutArea.X, layoutArea.X+layoutArea.Width-slaveRect.Width)
	slaveRect.Y = clampInt(slaveRect.Y, layoutArea.Y, layoutArea.Y+layoutArea.Height-slaveRect.Height)

	return layoutArea, hostRect, slaveRect
}

func (app *guiApp) resolveHostSideFromDelta(dx int, dy int) string {
	if absInt(dx) >= absInt(dy) {
		if dx >= 0 {
			return hostSideLeft
		}
		return hostSideRight
	}

	if dy >= 0 {
		return hostSideTop
	}

	return hostSideBottom
}

func (app *guiApp) resolveHostSideFromRect(slaveRect walk.Rectangle, hostRect walk.Rectangle) string {
	slaveCenterX := slaveRect.X + slaveRect.Width/2
	slaveCenterY := slaveRect.Y + slaveRect.Height/2
	hostCenterX := hostRect.X + hostRect.Width/2
	hostCenterY := hostRect.Y + hostRect.Height/2

	return app.resolveHostSideFromDelta(slaveCenterX-hostCenterX, slaveCenterY-hostCenterY)
}

func (app *guiApp) paintHostSideLayout(canvas *walk.Canvas, bounds walk.Rectangle) error {
	layoutArea, hostRect, slaveRect := app.hostSideLayoutRects(bounds)
	if app.draggingSlave {
		slaveRect = app.dragSlaveRect
	}

	backgroundBrush, _ := walk.NewSolidColorBrush(hostSideBgColor)
	if backgroundBrush != nil {
		defer backgroundBrush.Dispose()
		_ = canvas.FillRectanglePixels(backgroundBrush, layoutArea)
	}

	framePen, _ := walk.NewCosmeticPen(walk.PenSolid, hostSideFrameColor)
	if framePen != nil {
		defer framePen.Dispose()
		_ = canvas.DrawRectanglePixels(framePen, layoutArea)
	}

	hostBrush, _ := walk.NewSolidColorBrush(hostSideHostFillColor)
	hostPen, _ := walk.NewCosmeticPen(walk.PenSolid, hostSideHostLineColor)
	if hostBrush != nil {
		defer hostBrush.Dispose()
		_ = canvas.FillRectanglePixels(hostBrush, hostRect)
	}
	if hostPen != nil {
		defer hostPen.Dispose()
		_ = canvas.DrawRectanglePixels(hostPen, hostRect)
	}

	slaveBrush, _ := walk.NewSolidColorBrush(hostSideSlaveFillColor)
	slavePen, _ := walk.NewCosmeticPen(walk.PenSolid, hostSideSlaveLineColor)
	if slaveBrush != nil {
		defer slaveBrush.Dispose()
		_ = canvas.FillRectanglePixels(slaveBrush, slaveRect)
	}
	if slavePen != nil {
		defer slavePen.Dispose()
		_ = canvas.DrawRectanglePixels(slavePen, slaveRect)
	}

	font := app.mw.Font()
	if app.hostSideWidget != nil && app.hostSideWidget.Font() != nil {
		font = app.hostSideWidget.Font()
	}

	if font != nil {
		centerFormat := walk.TextCenter | walk.TextVCenter | walk.TextSingleLine
		_ = canvas.DrawTextPixels("1", font, hostSideHostTextColor, hostRect, centerFormat)
		_ = canvas.DrawTextPixels("2", font, hostSideSlaveTextColor, slaveRect, centerFormat)
	}

	return nil
}

func (app *guiApp) attachHostSideWidgetEvents() {
	if app.hostSideWidget == nil {
		return
	}

	if app.slaveResCombo != nil {
		app.slaveResCombo.TextChanged().Attach(func() {
			if app.hostSideWidget != nil {
				app.hostSideWidget.Invalidate()
			}
		})
		app.slaveResCombo.CurrentIndexChanged().Attach(func() {
			if app.hostSideWidget != nil {
				app.hostSideWidget.Invalidate()
			}
		})
	}

	_ = app.hostSideWidget.SetToolTipText("Drag display 2 around display 1 to set device placement")

	app.hostSideWidget.MouseDown().Attach(func(x int, y int, button walk.MouseButton) {
		if button != walk.LeftButton || app.hostSideWidget == nil || !app.hostSideWidget.Enabled() {
			return
		}

		layoutArea, hostRect, slaveRect := app.hostSideLayoutRects(app.hostSideWidget.ClientBoundsPixels())
		if rectContainsPoint(slaveRect, x, y) {
			app.draggingSlave = true
			app.dragOffsetX = x - slaveRect.X
			app.dragOffsetY = y - slaveRect.Y
			app.dragSlaveRect = slaveRect
			return
		}

		hostCenterX := hostRect.X + hostRect.Width/2
		hostCenterY := hostRect.Y + hostRect.Height/2
		if !rectContainsPoint(layoutArea, x, y) {
			return
		}
		app.setSelectedHostSide(app.resolveHostSideFromDelta(x-hostCenterX, y-hostCenterY))
	})

	app.hostSideWidget.MouseMove().Attach(func(x int, y int, _ walk.MouseButton) {
		if !app.draggingSlave || app.hostSideWidget == nil || !app.hostSideWidget.Enabled() {
			return
		}

		layoutArea, hostRect, _ := app.hostSideLayoutRects(app.hostSideWidget.ClientBoundsPixels())
		dragRect := app.dragSlaveRect
		dragRect.X = x - app.dragOffsetX
		dragRect.Y = y - app.dragOffsetY
		dragRect.X = clampInt(dragRect.X, layoutArea.X, layoutArea.X+layoutArea.Width-dragRect.Width)
		dragRect.Y = clampInt(dragRect.Y, layoutArea.Y, layoutArea.Y+layoutArea.Height-dragRect.Height)
		app.dragSlaveRect = dragRect

		app.setSelectedHostSide(app.resolveHostSideFromRect(dragRect, hostRect))
		app.hostSideWidget.Invalidate()
	})

	app.hostSideWidget.MouseUp().Attach(func(_ int, _ int, button walk.MouseButton) {
		if button != walk.LeftButton || !app.draggingSlave {
			return
		}

		app.draggingSlave = false
		app.dragSlaveRect = walk.Rectangle{}
		if app.hostSideWidget != nil {
			app.hostSideWidget.Invalidate()
		}
	})
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

	app.setTrayIconForRemoteMode(app.remoteModeActive)

	return nil
}

func (app *guiApp) disposeTray() {
	if app.trayIcon != nil {
		_ = app.trayIcon.Dispose()
		app.trayIcon = nil
	}

	if app.appIcon != nil {
		app.appIcon.Dispose()
		app.appIcon = nil
	}

	if app.remoteModeIcon != nil {
		app.remoteModeIcon.Dispose()
		app.remoteModeIcon = nil
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
		MinSize:  Size{Width: 680, Height: 185},
		Size:     Size{Width: 700, Height: 205},
		Visible:  false,
		Layout: VBox{
			Spacing: 6,
			Margins: Margins{Left: 8, Top: 8, Right: 8, Bottom: 8},
		},
		Children: []Widget{
			Composite{
				Layout: HBox{Spacing: 6, MarginsZero: true},
				Children: []Widget{
					Label{Text: "Serial Port:"},
					ComboBox{
						AssignTo:     &app.portCombo,
						Model:        []string{"auto"},
						Editable:     false,
						MaxSize:      Size{Width: 140},
						MinSize:      Size{Width: 120},
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
				Layout: HBox{Spacing: 6, MarginsZero: true},
				Children: []Widget{
					Label{Text: "Toggle:"},
					ComboBox{
						AssignTo:     &app.toggleCombo,
						Model:        toggleHotkeyChoices,
						Editable:     false,
						CurrentIndex: 8,
						MinSize:      Size{Width: 60},
						MaxSize:      Size{Width: 60},
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
						MaxSize:  Size{Width: 64},
						MinSize:  Size{Width: 64},
					},
				},
			},
			Composite{
				Layout: HBox{Spacing: 6, MarginsZero: true},
				Children: []Widget{
					Label{Text: "Slave Res:"},
					ComboBox{
						AssignTo:     &app.slaveResCombo,
						Model:        slaveResolutionChoices,
						Editable:     true,
						CurrentIndex: 3,
						MinSize:      Size{Width: 104},
						MaxSize:      Size{Width: 112},
					},
					Label{Text: "Layout:"},
					CustomWidget{
						AssignTo:    &app.hostSideWidget,
						PaintPixels: app.paintHostSideLayout,
						MinSize:     Size{Width: 185, Height: 82},
						MaxSize:     Size{Width: 185, Height: 82},
					},
					HSpacer{},
					Label{Text: "Bridge:"},
					CustomWidget{
						AssignTo:    &app.statusWidget,
						PaintPixels: app.paintStatusText,
						MinSize:     Size{Width: 88, Height: 18},
						MaxSize:     Size{Width: 88, Height: 18},
					},
					Label{Text: "Port:"},
					Label{
						AssignTo: &app.activePortLabel,
						Text:     "-",
						MinSize:  Size{Width: 52},
					},
				},
			},
		},
	}.Create()
}

func (app *guiApp) onStart() {
	_ = app.startBridge(true)
}

func (app *guiApp) startBridge(showErrorDialog bool) error {
	cfg, err := app.readConfigFromForm()
	if err != nil {
		if showErrorDialog {
			walk.MsgBox(app.mw, "Invalid Settings", err.Error(), walk.MsgBoxIconError)
		}
		return err
	}

	if app.runtime != nil && app.runtime.Running() {
		return nil
	}

	app.runtime = newBridgeRuntime(cfg, app.pushEvent)
	if err := app.runtime.Start(); err != nil {
		if showErrorDialog {
			walk.MsgBox(app.mw, "Start Failed", err.Error(), walk.MsgBoxIconError)
		}
		return err
	}

	app.setRunning(true)
	app.setStatusText("Starting")
	app.setConnectionIndicator(connectionIndicatorWaiting)

	return nil
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
		app.setTrayIconForRemoteMode(false)
		app.setStatusText("Starting")
		app.setConnectionIndicator(connectionIndicatorWaiting)
	case bridgeEventStopping:
		app.setTrayIconForRemoteMode(false)
		app.setStatusText("Stopping")
		app.setConnectionIndicator(connectionIndicatorWaiting)
	case bridgeEventStopped:
		app.setTrayIconForRemoteMode(false)
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
	case bridgeEventRemoteModeOn:
		app.setTrayIconForRemoteMode(true)
	case bridgeEventRemoteModeOff:
		app.setTrayIconForRemoteMode(false)
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

	resolutionText := strings.TrimSpace(app.slaveResCombo.Text())
	slaveWidth, slaveHeight, ok := parseSlaveResolution(resolutionText)
	if !ok {
		return cfg, fmt.Errorf("invalid slave resolution: %q", resolutionText)
	}
	cfg.slaveWidth = slaveWidth
	cfg.slaveHeight = slaveHeight

	hostSide, ok := normalizeHostSide(app.selectedHostSideOrDefault())
	if !ok {
		return cfg, fmt.Errorf("invalid host side: %q", app.selectedHostSide)
	}
	cfg.hostSide = hostSide
	app.baseCfg.hostSide = hostSide

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
	if app.slaveResCombo != nil {
		app.slaveResCombo.SetEnabled(!running)
	}
	if app.hostSideWidget != nil {
		app.hostSideWidget.SetEnabled(!running)
	}
	if app.rateEdit != nil {
		app.rateEdit.SetEnabled(!running)
	}
}
