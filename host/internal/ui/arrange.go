package ui

import (
	"fmt"
	"math"

	"esp-hid/host/internal/config"
)

// Display is one monitor as the OS reports it: its place in the desktop's
// global coordinate space (points on macOS, pixels on Windows, y down in
// both — the same space the capture layer works in) and its physical size,
// which is what lets the device be drawn to scale next to it.
type Display struct {
	Name              string
	X, Y, W, H        float64
	WidthMM, HeightMM float64 // 0 when the OS does not know
	Primary           bool
}

// rectF is a rectangle in canvas pixels, y down.
type rectF struct{ X, Y, W, H float64 }

func (r rectF) contains(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r rectF) centre() (float64, float64) { return r.X + r.W/2, r.Y + r.H/2 }

// arrangeBox is one thing to draw: a display or the device.
type arrangeBox struct {
	Rect    rectF
	Name    string
	Primary bool
}

// arrangeFrame is everything a GUI needs to paint the picture. The platform
// code draws it and forwards mouse events; it decides nothing itself.
type arrangeFrame struct {
	Displays []arrangeBox
	Device   arrangeBox
	Side     string
	Dragging bool
}

// Layout constants, in canvas pixels unless noted.
const (
	arrangePadding = 12.0
	// The device never shrinks below this on screen, whatever the scale, so
	// it stays visible and grabbable beside a wall of monitors.
	arrangeMinDevicePx = 10.0
	// Gap between the device and the desktop, as a fraction of the desktop's
	// larger dimension (world units).
	arrangeGapFraction = 0.02
	// mm per unit when the OS reports no physical size: 96 dpi.
	arrangeFallbackMMPerUnit = 25.4 / 96
)

// arranger is the display-arrangement widget's model: the desktop's
// monitors, the device's size, which side it sits on, and a drag in
// progress. It is untagged and tested; gui_darwin.m and gui_windows.go only
// render frame() and report mouse events.
//
// Sides: the model and the picture speak of the *device's* side, because
// that is what the user drags. The saved HostSide is the host's side
// relative to the device — the other end of the same relationship — so the
// two are opposites; newArranger and sideIndex convert at the boundary and
// nothing else needs to know.
//
// Scale: displays keep their OS geometry, so the arrangement is exactly what
// System Settings shows. The device is sized in that same space from its
// physical dimensions (pixels ÷ density) through the primary display's
// millimetres-per-unit, so a phone comes out the size it would be lying
// beside the monitor. Everything is then fitted into the canvas.
type arranger struct {
	displays         []Display
	devW, devH       int
	devDPI           int
	side             string // the device's side of the desktop
	canvasW, canvasH float64

	dragging           bool
	grabDX, grabDY     float64 // pointer minus device origin at grab
	dragDevX, dragDevY float64 // device origin while dragging

	frame arrangeFrame
	// The desktop's union in canvas pixels, for the drop decision.
	union rectF
}

// newArranger takes the saved host side and shows the device on the
// opposite one.
func newArranger(hostSide string, resolution string) *arranger {
	a := &arranger{side: OppositeSide(normalizeSide(hostSide)), devW: 1080, devH: 2400, devDPI: DefaultDeviceDPI}
	a.setDevice(resolution)
	return a
}

func normalizeSide(side string) string {
	if IndexOf(HostSideChoices, side) < 0 {
		return config.HostSideLeft
	}
	return side
}

func (a *arranger) setDisplays(displays []Display) {
	a.displays = append([]Display(nil), displays...)
	a.layout()
}

// setDevice takes the resolution field's text; anything that does not parse
// keeps the previous size, so half-typed input does not make the picture
// jump.
func (a *arranger) setDevice(resolution string) {
	width, height, err := config.ParseResolution(resolution)
	if err != nil {
		return
	}
	a.devW, a.devH = width, height
	a.devDPI = DeviceDensity(resolution)
	a.layout()
}

// setDeviceSide moves the device to a side of the desktop (the device's
// side, as drawn).
func (a *arranger) setDeviceSide(side string) {
	a.side = normalizeSide(side)
	a.layout()
}

func (a *arranger) setCanvas(w, h float64) {
	a.canvasW, a.canvasH = w, h
	a.layout()
}

// sideIndex is what FormValues.HostSideIndex wants: the host's side, which
// is the opposite of where the device is drawn.
func (a *arranger) sideIndex() int { return IndexOf(HostSideChoices, OppositeSide(a.side)) }

func (a *arranger) frameNow() arrangeFrame { return a.frame }

// beginDrag starts a drag if the pointer is on the device.
func (a *arranger) beginDrag(x, y float64) bool {
	if !a.frame.Device.Rect.contains(x, y) {
		return false
	}
	a.dragging = true
	a.grabDX, a.grabDY = x-a.frame.Device.Rect.X, y-a.frame.Device.Rect.Y
	a.dragDevX, a.dragDevY = a.frame.Device.Rect.X, a.frame.Device.Rect.Y
	a.frame.Dragging = true
	return true
}

func (a *arranger) drag(x, y float64) {
	if !a.dragging {
		return
	}
	a.dragDevX, a.dragDevY = x-a.grabDX, y-a.grabDY
	a.frame.Device.Rect.X, a.frame.Device.Rect.Y = a.dragDevX, a.dragDevY
}

// endDrag drops the device: whichever side of the desktop's centre it was
// let go on, judged by the larger normalised offset, is the new side. The
// picture snaps back to a tidy placement. Reports whether the side changed.
func (a *arranger) endDrag(x, y float64) bool {
	if !a.dragging {
		return false
	}
	a.drag(x, y)
	a.dragging = false
	a.frame.Dragging = false

	cx, cy := a.frame.Device.Rect.centre()
	ucx, ucy := a.union.centre()
	dx := (cx - ucx) / math.Max(a.union.W, 1)
	dy := (cy - ucy) / math.Max(a.union.H, 1)
	side := a.side
	if math.Abs(dx) >= math.Abs(dy) {
		if dx < 0 {
			side = config.HostSideLeft
		} else {
			side = config.HostSideRight
		}
	} else {
		if dy < 0 {
			side = config.HostSideTop
		} else {
			side = config.HostSideBottom
		}
	}
	changed := side != a.side
	a.side = side
	a.layout()
	return changed
}

// layout recomputes the frame from the model. World units are the displays'
// own; canvas units are pixels.
func (a *arranger) layout() {
	displays := a.displays
	if len(displays) == 0 {
		// Nothing enumerated yet (or a headless test): a stand-in so the
		// picture still shows the device on its side.
		displays = []Display{{Name: "Display", W: 1920, H: 1080, Primary: true}}
	}

	// Desktop union in world units.
	ux0, uy0 := math.Inf(1), math.Inf(1)
	ux1, uy1 := math.Inf(-1), math.Inf(-1)
	for _, d := range displays {
		ux0, uy0 = math.Min(ux0, d.X), math.Min(uy0, d.Y)
		ux1, uy1 = math.Max(ux1, d.X+d.W), math.Max(uy1, d.Y+d.H)
	}
	uw, uh := ux1-ux0, uy1-uy0

	// Device size in world units, via the primary display's physical scale.
	mmPerUnit := arrangeFallbackMMPerUnit
	for _, d := range displays {
		if d.Primary && d.WidthMM > 0 && d.W > 0 {
			mmPerUnit = d.WidthMM / d.W
			break
		}
	}
	dpi := float64(a.devDPI)
	if dpi <= 0 {
		dpi = DefaultDeviceDPI
	}
	devW := float64(a.devW) / dpi * 25.4 / mmPerUnit
	devH := float64(a.devH) / dpi * 25.4 / mmPerUnit

	// Place it just outside the union, centred along that edge.
	gap := arrangeGapFraction * math.Max(uw, uh)
	var dx, dy float64
	switch a.side {
	case config.HostSideRight:
		dx, dy = ux1+gap, uy0+(uh-devH)/2
	case config.HostSideTop:
		dx, dy = ux0+(uw-devW)/2, uy0-gap-devH
	case config.HostSideBottom:
		dx, dy = ux0+(uw-devW)/2, uy1+gap
	default:
		dx, dy = ux0-gap-devW, uy0+(uh-devH)/2
	}

	// Fit union ∪ device into the canvas.
	tx0, ty0 := math.Min(ux0, dx), math.Min(uy0, dy)
	tx1, ty1 := math.Max(ux1, dx+devW), math.Max(uy1, dy+devH)
	tw, th := tx1-tx0, ty1-ty0
	if a.canvasW <= 2*arrangePadding || a.canvasH <= 2*arrangePadding || tw <= 0 || th <= 0 {
		a.frame = arrangeFrame{Side: a.side}
		a.union = rectF{}
		return
	}
	scale := math.Min((a.canvasW-2*arrangePadding)/tw, (a.canvasH-2*arrangePadding)/th)
	offX := (a.canvasW - tw*scale) / 2
	offY := (a.canvasH - th*scale) / 2
	toCanvas := func(x, y, w, h float64) rectF {
		return rectF{X: (x-tx0)*scale + offX, Y: (y-ty0)*scale + offY, W: w * scale, H: h * scale}
	}

	frame := arrangeFrame{Side: a.side, Dragging: a.dragging}
	for _, d := range displays {
		frame.Displays = append(frame.Displays, arrangeBox{Rect: toCanvas(d.X, d.Y, d.W, d.H), Name: d.Name, Primary: d.Primary})
	}
	device := toCanvas(dx, dy, devW, devH)
	// Keep the device grabbable, growing about its centre if the scale made
	// it tiny.
	if m := math.Min(device.W, device.H); m < arrangeMinDevicePx {
		k := arrangeMinDevicePx / m
		cx, cy := device.centre()
		device.W, device.H = device.W*k, device.H*k
		device.X, device.Y = cx-device.W/2, cy-device.H/2
	}
	if a.dragging {
		device.X, device.Y = a.dragDevX, a.dragDevY
	}
	frame.Device = arrangeBox{Rect: device, Name: fmt.Sprintf("%dx%d", a.devW, a.devH)}
	a.frame = frame
	a.union = toCanvas(ux0, uy0, uw, uh)
}
