//go:build darwin

package main

import (
	"fmt"
	"image/color"
	"log"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// ── color palette ────────────────────────────────────────────────────────────

var (
	colorConnected = color.RGBA{R: 46, G: 185, B: 89, A: 255}
	colorWaiting   = color.RGBA{R: 209, G: 154, B: 30, A: 255}
	colorFailed    = color.RGBA{R: 214, G: 69, B: 65, A: 255}

	colorHostFill  = color.RGBA{R: 23, G: 122, B: 205, A: 255}
	colorSlaveFill = color.RGBA{R: 200, G: 215, B: 235, A: 255}
	colorFrame     = color.RGBA{R: 188, G: 196, B: 208, A: 255}
)

// ── guiApp ───────────────────────────────────────────────────────────────────

type guiApp struct {
	baseCfg config
	runtime *bridgeRuntime

	fyneApp fyne.App
	window  fyne.Window

	statusCh  chan bridgeEvent
	eventStop chan struct{}

	// widgets
	portSelect      *widget.Select
	statusLabel     *canvas.Text
	activePortLabel *widget.Label
	startBtn        *widget.Button
	stopBtn         *widget.Button

	hotkeyLabel     *widget.Label
	hotkeyRecordBtn *widget.Button
	recordingHotkey bool
	rateEntry       *widget.Entry
	keyboardCheck   *widget.Check
	autoSwitchRadio *widget.RadioGroup
	slaveResSelect  *widget.Select
	hostSideWidget  *hostSideCanvas

	selectedHostSide string
}

// runGUI is the main entry point for the GUI.
func runGUI(initial config) error {
	a := app.NewWithID("com.esp-hid-bridge.macos")

	w := a.NewWindow("ESP HID Bridge")
	w.Resize(fyne.NewSize(660, 380))
	w.SetFixedSize(false)

	guiInst := &guiApp{
		baseCfg:   initial,
		fyneApp:   a,
		window:    w,
		statusCh:  make(chan bridgeEvent, 256),
		eventStop: make(chan struct{}),
	}

	if normalizedHostSide, ok := normalizeHostSide(initial.hostSide); ok {
		guiInst.selectedHostSide = normalizedHostSide
	} else {
		guiInst.selectedHostSide = defaultHostSide
	}
	guiInst.baseCfg.hostSide = guiInst.selectedHostSide

	content := guiInst.buildContent()
	w.SetContent(content)

	// System tray (menu bar icon) — only available on desktop platforms
	if desk, ok := a.(desktop.App); ok {
		desk.SetSystemTrayMenu(fyne.NewMenu("ESP HID Bridge",
			fyne.NewMenuItem("Open", func() { w.Show(); w.RequestFocus() }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				guiInst.stopRuntimeAndWait()
				close(guiInst.eventStop)
				a.Quit()
			}),
		))
	}

	w.SetCloseIntercept(func() {
		w.Hide() // minimize to tray instead of quitting
	})

	// Populate ports
	guiInst.refreshPorts()

	// Initialize hotkey label
	toggleName := guiInst.baseCfg.toggleHotkeyName
	if normalized, ok := normalizeToggleHotkeyName(toggleName); ok {
		guiInst.hotkeyLabel.SetText(normalized)
	} else {
		guiInst.hotkeyLabel.SetText(defaultToggleHotkeyName)
	}

	// Initialize slave resolution
	resText := formatSlaveResolution(guiInst.baseCfg.slaveWidth, guiInst.baseCfg.slaveHeight)
	guiInst.slaveResSelect.SetSelected(resText)

	guiInst.setRunning(false)
	guiInst.setStatusText("Stopped", colorWaiting)

	go guiInst.consumeEvents()
	go guiInst.backgroundRefresh()

	if err := guiInst.startBridge(false); err != nil {
		guiInst.setStatusText("Start failed", colorFailed)
	}

	w.ShowAndRun()

	close(guiInst.eventStop)
	guiInst.stopRuntimeAndWait()

	return nil
}

// ── UI construction ───────────────────────────────────────────────────────────

func (app *guiApp) buildContent() fyne.CanvasObject {
	// ── Connection & Status ──────────────────────────────────────────────────
	app.portSelect = widget.NewSelect([]string{"auto"}, nil)
	app.portSelect.Selected = "auto"

	refreshBtn := widget.NewButton("Refresh", func() { app.refreshPorts() })

	app.statusLabel = canvas.NewText("Stopped", colorWaiting)
	app.statusLabel.TextStyle = fyne.TextStyle{Bold: true}

	app.activePortLabel = widget.NewLabel("-")

	app.startBtn = widget.NewButton("Start", app.onStart)
	app.stopBtn = widget.NewButton("Stop", app.onStop)

	connectionRow := container.NewHBox(
		widget.NewLabel("Serial Port:"),
		app.portSelect,
		refreshBtn,
		layout.NewSpacer(),
		widget.NewLabel("Status:"),
		app.statusLabel,
		widget.NewLabel("  Port:"),
		app.activePortLabel,
		layout.NewSpacer(),
		app.startBtn,
		app.stopBtn,
	)
	connectionGroup := widget.NewCard("Connection & Status", "", connectionRow)

	// ── Input Settings ────────────────────────────────────────────────────────
	app.hotkeyLabel = widget.NewLabel(app.baseCfg.toggleHotkeyName)
	app.hotkeyLabel.TextStyle = fyne.TextStyle{Bold: true}

	app.hotkeyRecordBtn = widget.NewButton("Record", func() {
		app.startHotkeyRecording()
	})

	hotkeyRow := container.NewHBox(
		widget.NewLabel("Toggle Hotkey:"),
		app.hotkeyLabel,
		app.hotkeyRecordBtn,
	)

	app.rateEntry = widget.NewEntry()
	app.rateEntry.SetText(strconv.Itoa(app.baseCfg.moveRateHz))
	app.rateEntry.Validator = func(s string) error {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || v <= 0 {
			return fmt.Errorf("must be a positive integer")
		}
		return nil
	}

	rateRow := container.NewHBox(
		widget.NewLabel("Send Rate (Hz):"),
		app.rateEntry,
	)

	app.keyboardCheck = widget.NewCheck("Forward Keyboard", nil)
	app.keyboardCheck.SetChecked(app.baseCfg.captureKeyboard)

	inputGroup := widget.NewCard("Input Settings", "",
		container.NewVBox(hotkeyRow, rateRow, app.keyboardCheck),
	)

	// ── Switching Mode ────────────────────────────────────────────────────────
	app.autoSwitchRadio = widget.NewRadioGroup(
		[]string{"Auto (Switch at edge of screens)", "Manual (Hotkey toggle only)"},
		func(selected string) {
			app.baseCfg.autoSwitch = strings.HasPrefix(selected, "Auto")
			app.updateModeDependentWidgets()
		},
	)
	if app.baseCfg.autoSwitch {
		app.autoSwitchRadio.SetSelected("Auto (Switch at edge of screens)")
	} else {
		app.autoSwitchRadio.SetSelected("Manual (Hotkey toggle only)")
	}

	modeGroup := widget.NewCard("Switching Mode", "", app.autoSwitchRadio)

	middleRow := container.NewGridWithColumns(2, inputGroup, modeGroup)

	// ── Device Layout ─────────────────────────────────────────────────────────
	app.slaveResSelect = widget.NewSelect(slaveResolutionChoices, func(res string) {
		w, h, ok := parseSlaveResolution(res)
		if ok {
			app.baseCfg.slaveWidth = w
			app.baseCfg.slaveHeight = h
		}
		if app.hostSideWidget != nil {
			app.hostSideWidget.Refresh()
		}
	})

	// Find best match in choices list
	resText := formatSlaveResolution(app.baseCfg.slaveWidth, app.baseCfg.slaveHeight)
	app.slaveResSelect.SetSelected(resText)

	app.hostSideWidget = newHostSideCanvas(app)

	hostSideButtons := container.NewHBox(
		widget.NewButton("Left", func() { app.setSelectedHostSide(hostSideLeft) }),
		widget.NewButton("Right", func() { app.setSelectedHostSide(hostSideRight) }),
		widget.NewButton("Top", func() { app.setSelectedHostSide(hostSideTop) }),
		widget.NewButton("Bottom", func() { app.setSelectedHostSide(hostSideBottom) }),
	)

	layoutGroup := widget.NewCard("Device Layout & Resolution", "",
		container.NewHBox(
			container.NewVBox(
				widget.NewLabel("Slave Resolution:"),
				app.slaveResSelect,
			),
			container.NewVBox(
				widget.NewLabel("Placement:"),
				app.hostSideWidget,
				hostSideButtons,
			),
		),
	)

	return container.NewVBox(
		connectionGroup,
		middleRow,
		layoutGroup,
	)
}

// ── Host side preview widget ─────────────────────────────────────────────────

type hostSideCanvas struct {
	widget.BaseWidget
	guiApp *guiApp
}

func newHostSideCanvas(g *guiApp) *hostSideCanvas {
	h := &hostSideCanvas{guiApp: g}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hostSideCanvas) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.RGBA{R: 245, G: 247, B: 250, A: 255})
	frame := canvas.NewRectangle(color.Transparent)
	frame.StrokeColor = colorFrame
	frame.StrokeWidth = 1

	hostRect := canvas.NewRectangle(colorHostFill)
	hostLabel := canvas.NewText("1", color.White)
	hostLabel.TextStyle = fyne.TextStyle{Bold: true}

	slaveRect := canvas.NewRectangle(colorSlaveFill)
	slaveRect.StrokeColor = colorFrame
	slaveRect.StrokeWidth = 1
	slaveLabel := canvas.NewText("2", color.RGBA{R: 66, G: 74, B: 90, A: 255})
	slaveLabel.TextStyle = fyne.TextStyle{Bold: true}

	return &hostSideRenderer{
		h:         h,
		bg:        bg,
		frame:     frame,
		hostRect:  hostRect,
		hostLabel: hostLabel,
		slaveRect: slaveRect,
		slaveLabel: slaveLabel,
		objects:   []fyne.CanvasObject{bg, frame, hostRect, hostLabel, slaveRect, slaveLabel},
	}
}

type hostSideRenderer struct {
	h          *hostSideCanvas
	bg         *canvas.Rectangle
	frame      *canvas.Rectangle
	hostRect   *canvas.Rectangle
	hostLabel  *canvas.Text
	slaveRect  *canvas.Rectangle
	slaveLabel *canvas.Text
	objects    []fyne.CanvasObject
}

func (r *hostSideRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.frame.Resize(size)
	r.frame.Move(fyne.NewPos(0, 0))

	padding := float32(6)
	areaX := padding
	areaY := padding
	areaW := size.Width - 2*padding
	areaH := size.Height - 2*padding

	hostW := areaW * 0.30
	hostH := areaH * 0.55
	if hostW < 40 {
		hostW = 40
	}
	if hostH < 30 {
		hostH = 30
	}

	hostX := areaX + (areaW-hostW)/2
	hostY := areaY + (areaH-hostH)/2

	// Slave sizing: scale slave resolution to fit
	slaveAspect := float32(1.0)
	g := r.h.guiApp
	if g != nil {
		sw, sh := g.slavePreviewResolution()
		if sh > 0 {
			slaveAspect = float32(sw) / float32(sh)
		}
	}
	slaveH := hostH * 0.8
	slaveW := slaveH * slaveAspect
	if slaveW > hostW*0.9 {
		slaveW = hostW * 0.9
		slaveH = slaveW / slaveAspect
	}

	gap := float32(12)
	var slaveX, slaveY float32

	hostSide := hostSideLeft
	if g != nil {
		hostSide = g.selectedHostSideOrDefault()
	}

	switch hostSide {
	case hostSideRight:
		slaveX = hostX - gap - slaveW
		slaveY = hostY + (hostH-slaveH)/2
	case hostSideTop:
		slaveX = hostX + (hostW-slaveW)/2
		slaveY = hostY + hostH + gap
	case hostSideBottom:
		slaveX = hostX + (hostW-slaveW)/2
		slaveY = hostY - gap - slaveH
	default: // left
		slaveX = hostX + hostW + gap
		slaveY = hostY + (hostH-slaveH)/2
	}

	// Clamp within area
	if slaveX < areaX {
		slaveX = areaX
	}
	if slaveY < areaY {
		slaveY = areaY
	}
	if slaveX+slaveW > areaX+areaW {
		slaveX = areaX + areaW - slaveW
	}
	if slaveY+slaveH > areaY+areaH {
		slaveY = areaY + areaH - slaveH
	}

	r.hostRect.Resize(fyne.NewSize(hostW, hostH))
	r.hostRect.Move(fyne.NewPos(hostX, hostY))

	r.hostLabel.Move(fyne.NewPos(hostX+hostW/2-5, hostY+hostH/2-8))

	r.slaveRect.Resize(fyne.NewSize(slaveW, slaveH))
	r.slaveRect.Move(fyne.NewPos(slaveX, slaveY))

	r.slaveLabel.Move(fyne.NewPos(slaveX+slaveW/2-5, slaveY+slaveH/2-8))
}

func (r *hostSideRenderer) MinSize() fyne.Size { return fyne.NewSize(200, 90) }
func (r *hostSideRenderer) Refresh() {
	r.Layout(r.h.Size())
	for _, o := range r.objects {
		canvas.Refresh(o)
	}
}
func (r *hostSideRenderer) Destroy()           {}
func (r *hostSideRenderer) Objects() []fyne.CanvasObject { return r.objects }

// ── guiApp helper methods ─────────────────────────────────────────────────────

func (app *guiApp) slavePreviewResolution() (int, int) {
	if app.slaveResSelect != nil {
		if w, h, ok := parseSlaveResolution(app.slaveResSelect.Selected); ok {
			return w, h
		}
	}
	if app.baseCfg.slaveWidth > 0 && app.baseCfg.slaveHeight > 0 {
		return app.baseCfg.slaveWidth, app.baseCfg.slaveHeight
	}
	return defaultSlaveWidth, defaultSlaveHeight
}

func (app *guiApp) selectedHostSideOrDefault() string {
	if ns, ok := normalizeHostSide(app.selectedHostSide); ok {
		return ns
	}
	return defaultHostSide
}

func (app *guiApp) setSelectedHostSide(side string) {
	ns, ok := normalizeHostSide(side)
	if !ok {
		ns = defaultHostSide
	}
	if app.selectedHostSide == ns {
		return
	}
	app.selectedHostSide = ns
	app.baseCfg.hostSide = ns
	if app.hostSideWidget != nil {
		app.hostSideWidget.Refresh()
	}
}

func (app *guiApp) setStatusText(text string, col color.RGBA) {
	if app.statusLabel == nil {
		return
	}
	app.statusLabel.Text = text
	app.statusLabel.Color = col
	app.statusLabel.Refresh()
}

func (app *guiApp) setConnectionIndicator(state connectionIndicatorState) {
	switch state {
	case connectionIndicatorConnected:
		app.setStatusText(app.statusLabel.Text, colorConnected)
	case connectionIndicatorFailed:
		app.setStatusText(app.statusLabel.Text, colorFailed)
	default:
		app.setStatusText(app.statusLabel.Text, colorWaiting)
	}
}

type connectionIndicatorState int

const (
	connectionIndicatorWaiting connectionIndicatorState = iota
	connectionIndicatorConnected
	connectionIndicatorFailed
)

func (app *guiApp) setRunning(running bool) {
	if app.startBtn != nil {
		if running {
			app.startBtn.Disable()
		} else {
			app.startBtn.Enable()
		}
	}
	if app.stopBtn != nil {
		if running {
			app.stopBtn.Enable()
		} else {
			app.stopBtn.Disable()
		}
	}
	if app.portSelect != nil {
		if running {
			app.portSelect.Disable()
		} else {
			app.portSelect.Enable()
		}
	}
	if app.hotkeyRecordBtn != nil {
		if running {
			app.hotkeyRecordBtn.Disable()
		} else {
			app.hotkeyRecordBtn.Enable()
		}
	}
	if app.keyboardCheck != nil {
		if running {
			app.keyboardCheck.Disable()
		} else {
			app.keyboardCheck.Enable()
		}
	}
	if app.slaveResSelect != nil {
		if running {
			app.slaveResSelect.Disable()
		} else {
			app.slaveResSelect.Enable()
		}
	}
	if app.rateEntry != nil {
		if running {
			app.rateEntry.Disable()
		} else {
			app.rateEntry.Enable()
		}
	}
	if app.autoSwitchRadio != nil {
		if running {
			app.autoSwitchRadio.Disable()
		} else {
			app.autoSwitchRadio.Enable()
		}
	}
	app.updateModeDependentWidgets()
}

func (app *guiApp) updateModeDependentWidgets() {
	running := app.startBtn != nil && !app.startBtn.Disabled()
	autoMode := app.baseCfg.autoSwitch
	enabled := !running && autoMode

	if app.slaveResSelect != nil {
		if enabled {
			app.slaveResSelect.Enable()
		} else {
			app.slaveResSelect.Disable()
		}
	}
}

func (app *guiApp) refreshPorts() {
	ports, err := listSerialPorts()
	items := []string{"auto"}
	if err == nil {
		items = append(items, ports...)
	}

	current := ""
	if app.portSelect != nil {
		current = app.portSelect.Selected
	}

	if app.portSelect != nil {
		app.portSelect.Options = items
		app.portSelect.Refresh()
	}

	// Restore selection
	found := false
	if current != "" {
		for _, item := range items {
			if strings.EqualFold(item, current) {
				app.portSelect.SetSelected(item)
				found = true
				break
			}
		}
	}
	if !found && app.portSelect != nil {
		if app.baseCfg.autoPort {
			app.portSelect.SetSelected("auto")
		} else if app.baseCfg.portName != "" {
			app.portSelect.SetSelected(app.baseCfg.portName)
		} else {
			app.portSelect.SetSelected(items[0])
		}
	}
}

func (app *guiApp) onStart() {
	_ = app.startBridge(true)
}

func (app *guiApp) startBridge(showError bool) error {
	cfg, err := app.readConfigFromForm()
	if err != nil {
		if showError {
			d := widget.NewLabel("Invalid settings: " + err.Error())
			w := app.fyneApp.NewWindow("Error")
			w.SetContent(d)
			w.Show()
		}
		return err
	}

	if app.runtime != nil && app.runtime.Running() {
		return nil
	}

	if err := saveSettingsConfig(cfg); err != nil {
		log.Printf("settings save failed: %v", err)
	}

	app.runtime = newBridgeRuntime(cfg, app.pushEvent)
	if err := app.runtime.Start(); err != nil {
		if showError {
			d := widget.NewLabel("Start failed: " + err.Error())
			w := app.fyneApp.NewWindow("Error")
			w.SetContent(d)
			w.Show()
		}
		return err
	}

	app.setRunning(true)
	app.setStatusText("Starting", colorWaiting)
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
			app.applyEventOnMain(event)
		}
	}
}

func (app *guiApp) applyEventOnMain(event bridgeEvent) {
	switch event.Type {
	case bridgeEventStarting:
		app.setStatusText("Starting", colorWaiting)
	case bridgeEventStopping:
		app.setStatusText("Stopping", colorWaiting)
	case bridgeEventStopped:
		app.setStatusText("Stopped", colorWaiting)
		if app.activePortLabel != nil {
			app.activePortLabel.SetText("-")
		}
		app.setRunning(false)
	case bridgeEventSerialConnected:
		app.setStatusText("Connected", colorConnected)
		if event.Port != "" && app.activePortLabel != nil {
			app.activePortLabel.SetText(event.Port)
		}
		app.setRunning(true)
	case bridgeEventSerialOpenFailed:
		app.setStatusText("Waiting for device", colorFailed)
	case bridgeEventSerialWriteError:
		app.setStatusText("Connection issue", colorFailed)
	case bridgeEventCaptureError:
		app.setStatusText("Capture error", colorFailed)
	}
}

func (app *guiApp) backgroundRefresh() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.eventStop:
			return
		case <-ticker.C:
			if app.runtime == nil || !app.runtime.Running() {
				app.refreshPorts()
			}
		}
	}
}

func (app *guiApp) readConfigFromForm() (config, error) {
	cfg := app.baseCfg
	cfg.guiMode = true

	if app.keyboardCheck != nil {
		cfg.captureKeyboard = app.keyboardCheck.Checked
	}
	if app.autoSwitchRadio != nil {
		cfg.autoSwitch = strings.HasPrefix(app.autoSwitchRadio.Selected, "Auto")
	}

	toggleName := app.baseCfg.toggleHotkeyName
	if n, ok := normalizeToggleHotkeyName(toggleName); ok {
		toggleName = n
	} else {
		toggleName = defaultToggleHotkeyName
	}
	toggleVK, toggleMods := toggleHotkeyNameToVKMods(toggleName)
	cfg.toggleHotkeyName = toggleName
	cfg.toggleHotkeyVK = toggleVK
	cfg.toggleHotkeyMods = toggleMods

	if app.portSelect != nil {
		port := strings.TrimSpace(app.portSelect.Selected)
		if port == "" || strings.EqualFold(port, "auto") {
			cfg.portName = "auto"
			cfg.autoPort = true
		} else {
			cfg.portName = port
			cfg.autoPort = false
		}
	}

	if app.rateEntry != nil {
		rateText := strings.TrimSpace(app.rateEntry.Text)
		rate, err := strconv.Atoi(rateText)
		if err != nil || rate <= 0 {
			return cfg, fmt.Errorf("invalid rate: %q", rateText)
		}
		cfg.moveRateHz = rate
	}

	if app.slaveResSelect != nil {
		resText := strings.TrimSpace(app.slaveResSelect.Selected)
		sw, sh, ok := parseSlaveResolution(resText)
		if !ok {
			return cfg, fmt.Errorf("invalid slave resolution: %q", resText)
		}
		cfg.slaveWidth = sw
		cfg.slaveHeight = sh
	}

	if hs, ok := normalizeHostSide(app.selectedHostSideOrDefault()); ok {
		cfg.hostSide = hs
		app.baseCfg.hostSide = hs
	}

	return cfg, nil
}

// applyRecordedHotkey is called from the hotkey recorder goroutine when a key
// is captured (or when recording is cancelled with an empty name).
func (app *guiApp) applyRecordedHotkey(comboName string) {
	app.recordingHotkey = false
	app.setHotkeyRecordingUI(false)

	if comboName == "" {
		return
	}

	vk, mods := toggleHotkeyNameToVKMods(comboName)
	app.baseCfg.toggleHotkeyName = comboName
	app.baseCfg.toggleHotkeyVK = vk
	app.baseCfg.toggleHotkeyMods = mods
	if app.hotkeyLabel != nil {
		app.hotkeyLabel.SetText(comboName)
	}
}

func (app *guiApp) setHotkeyRecordingUI(recording bool) {
	if recording {
		if app.hotkeyLabel != nil {
			app.hotkeyLabel.SetText("Press a key…")
		}
		if app.hotkeyRecordBtn != nil {
			app.hotkeyRecordBtn.Disable()
		}
	} else {
		if app.hotkeyRecordBtn != nil {
			app.hotkeyRecordBtn.Enable()
		}
	}
}

