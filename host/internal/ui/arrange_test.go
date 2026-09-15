package ui

import (
	"math"
	"testing"

	"esp-hid/host/internal/config"
)

// A 22" 1080p monitor as the primary, and the user's second one: same
// panel, sitting to its left and a little lower.
var (
	mainDisplay = Display{Name: "Main", X: 0, Y: 0, W: 1920, H: 1080, WidthMM: 476, HeightMM: 268, Primary: true}
	sideDisplay = Display{Name: "ASUS VZ22EHE", X: -1920, Y: 200, W: 1920, H: 1080, WidthMM: 476, HeightMM: 268}
	twoDisplays = []Display{mainDisplay, sideDisplay}
)

// newTestArranger places the device on deviceSide (what the picture shows);
// the constructor itself takes the host's side, hence the flip.
func newTestArranger(deviceSide string, displays []Display) *arranger {
	a := newArranger(OppositeSide(deviceSide), "1080x2400")
	a.setCanvas(600, 200)
	a.setDisplays(displays)
	return a
}

func within(r rectF, w, h float64) bool {
	return r.X >= -0.01 && r.Y >= -0.01 && r.X+r.W <= w+0.01 && r.Y+r.H <= h+0.01
}

func TestLayoutFitsEverythingInTheCanvas(t *testing.T) {
	for _, side := range HostSideChoices {
		for _, displays := range [][]Display{{mainDisplay}, twoDisplays, nil} {
			a := newTestArranger(side, displays)
			f := a.frameNow()
			if len(f.Displays) == 0 {
				t.Fatalf("%s: no displays in frame", side)
			}
			for _, d := range f.Displays {
				if !within(d.Rect, 600, 200) {
					t.Errorf("%s: display %s %+v escapes the canvas", side, d.Name, d.Rect)
				}
			}
			if !within(f.Device.Rect, 600, 200) {
				t.Errorf("%s: device %+v escapes the canvas", side, f.Device.Rect)
			}
		}
	}
}

func TestArrangementPreservesTheDisplaysGeometry(t *testing.T) {
	a := newTestArranger(config.HostSideRight, twoDisplays)
	f := a.frameNow()
	var main, side arrangeBox
	for _, d := range f.Displays {
		if d.Primary {
			main = d
		} else {
			side = d
		}
	}
	if main.Name != "Main" || side.Name != "ASUS VZ22EHE" {
		t.Fatalf("names lost: %+v / %+v", main, side)
	}
	// Same size, side one to the left, lower by 200/1080 of a height.
	if math.Abs(main.Rect.W-side.Rect.W) > 0.01 || math.Abs(main.Rect.H-side.Rect.H) > 0.01 {
		t.Errorf("equal panels drawn unequal: %+v vs %+v", main.Rect, side.Rect)
	}
	if math.Abs((side.Rect.X+side.Rect.W)-main.Rect.X) > 0.01 {
		t.Errorf("side display should abut the main one on its left: %+v vs %+v", side.Rect, main.Rect)
	}
	wantDrop := main.Rect.H * 200 / 1080
	if math.Abs((side.Rect.Y-main.Rect.Y)-wantDrop) > 0.01 {
		t.Errorf("side display vertical offset %v, want %v", side.Rect.Y-main.Rect.Y, wantDrop)
	}
}

func TestDeviceSitsOutsideTheDesktopOnEachSide(t *testing.T) {
	a := newTestArranger(config.HostSideLeft, twoDisplays)
	for _, side := range HostSideChoices {
		a.setDeviceSide(side)
		f := a.frameNow()
		u := a.union
		d := f.Device.Rect
		dcx, dcy := d.centre()
		ucx, ucy := u.centre()
		switch side {
		case config.HostSideLeft:
			if d.X+d.W > u.X || math.Abs(dcy-ucy) > 0.01 {
				t.Errorf("left: device %+v not left of and centred on union %+v", d, u)
			}
		case config.HostSideRight:
			if d.X < u.X+u.W || math.Abs(dcy-ucy) > 0.01 {
				t.Errorf("right: device %+v not right of and centred on union %+v", d, u)
			}
		case config.HostSideTop:
			if d.Y+d.H > u.Y || math.Abs(dcx-ucx) > 0.01 {
				t.Errorf("top: device %+v not above and centred on union %+v", d, u)
			}
		case config.HostSideBottom:
			if d.Y < u.Y+u.H || math.Abs(dcx-ucx) > 0.01 {
				t.Errorf("bottom: device %+v not below and centred on union %+v", d, u)
			}
		}
		// The frame names the device's side; the config index is the host's,
		// which is the opposite one.
		if f.Side != side || a.sideIndex() != IndexOf(HostSideChoices, OppositeSide(side)) {
			t.Errorf("side bookkeeping: frame %q, index %d", f.Side, a.sideIndex())
		}
	}
}

func TestDeviceIsDrawnAtRealLifeScale(t *testing.T) {
	// 1080x2400 at 420 dpi is 65.3 x 145.1 mm; the 22" panel is 268 mm tall.
	a := newArranger(config.HostSideLeft, "1080x2400")
	a.devDPI = 420 // pin it: DeviceDensity may know a better value for this size
	a.setCanvas(800, 300)
	a.setDisplays([]Display{mainDisplay})
	f := a.frameNow()
	want := (2400.0 / 420 * 25.4) / 268
	got := f.Device.Rect.H / f.Displays[0].Rect.H
	if math.Abs(got-want) > 0.01 {
		t.Errorf("device/monitor height ratio %.3f, want %.3f", got, want)
	}
	aspect := f.Device.Rect.W / f.Device.Rect.H
	if math.Abs(aspect-1080.0/2400) > 0.01 {
		t.Errorf("device aspect %.3f, want %.3f", aspect, 1080.0/2400)
	}
}

func TestScaleFallsBackTo96DPIWithoutPhysicalSize(t *testing.T) {
	a := newArranger(config.HostSideRight, "1080x2400")
	a.devDPI = 420
	a.setCanvas(800, 300)
	a.setDisplays([]Display{{Name: "Unknown", W: 1920, H: 1080, Primary: true}})
	f := a.frameNow()
	// Monitor assumed 1080 * 25.4/96 = 285.75 mm tall.
	want := (2400.0 / 420 * 25.4) / (1080 * 25.4 / 96)
	got := f.Device.Rect.H / f.Displays[0].Rect.H
	if math.Abs(got-want) > 0.01 {
		t.Errorf("ratio %.3f, want %.3f", got, want)
	}
}

func TestLandscapeResolutionDrawsLandscapeDevice(t *testing.T) {
	a := newTestArranger(config.HostSideRight, []Display{mainDisplay})
	a.setDevice("2400x1080")
	f := a.frameNow()
	if f.Device.Rect.W <= f.Device.Rect.H {
		t.Errorf("landscape resolution drew a portrait device: %+v", f.Device.Rect)
	}
	if f.Device.Name != "2400x1080" {
		t.Errorf("label %q", f.Device.Name)
	}
}

func TestSetDeviceIgnoresUnparsableInput(t *testing.T) {
	a := newTestArranger(config.HostSideRight, []Display{mainDisplay})
	before := a.frameNow().Device.Rect
	a.setDevice("1080x")
	if a.frameNow().Device.Rect != before {
		t.Error("half-typed resolution moved the device")
	}
}

func TestDropOnEachQuadrantPicksThatSide(t *testing.T) {
	cases := []struct {
		toX, toY float64 // fractions of the canvas
		want     string
	}{
		{0.02, 0.5, config.HostSideLeft},
		{0.98, 0.5, config.HostSideRight},
		{0.5, 0.02, config.HostSideTop},
		{0.5, 0.98, config.HostSideBottom},
	}
	for _, tc := range cases {
		start := config.HostSideLeft
		if tc.want == config.HostSideLeft {
			start = config.HostSideRight
		}
		a := newTestArranger(start, twoDisplays)
		cx, cy := a.frameNow().Device.Rect.centre()
		if !a.beginDrag(cx, cy) {
			t.Fatalf("%s: grab at the device centre failed", tc.want)
		}
		a.drag(tc.toX*600, tc.toY*200)
		if !a.frameNow().Dragging {
			t.Errorf("%s: frame not marked dragging mid-drag", tc.want)
		}
		changed := a.endDrag(tc.toX*600, tc.toY*200)
		if !changed || a.side != tc.want {
			t.Errorf("drop at (%.2f,%.2f): side %q changed=%v, want %q", tc.toX, tc.toY, a.side, changed, tc.want)
		}
		if a.frameNow().Dragging {
			t.Errorf("%s: still dragging after drop", tc.want)
		}
	}
}

func TestDropBackOnTheSameSideChangesNothing(t *testing.T) {
	a := newTestArranger(config.HostSideRight, twoDisplays)
	before := a.frameNow().Device.Rect
	cx, cy := before.centre()
	a.beginDrag(cx, cy)
	a.drag(cx+3, cy-2)
	if a.endDrag(cx+3, cy-2) {
		t.Error("a nudge reported a side change")
	}
	if a.frameNow().Device.Rect != before {
		t.Error("device did not snap back after a nudge")
	}
}

func TestDragIgnoredUnlessGrabbedOnTheDevice(t *testing.T) {
	a := newTestArranger(config.HostSideRight, twoDisplays)
	before := a.frameNow().Device.Rect
	if a.beginDrag(1, 1) {
		t.Fatal("grab on empty canvas succeeded")
	}
	a.drag(300, 100)
	if a.endDrag(300, 100) || a.frameNow().Device.Rect != before {
		t.Error("drag without a grab moved the device or changed the side")
	}
}

func TestDeviceNeverVanishesBesideAHugeDesktop(t *testing.T) {
	wall := []Display{{Name: "A", W: 7680, H: 4320, WidthMM: 1900, HeightMM: 1070, Primary: true}}
	a := newTestArranger(config.HostSideRight, wall)
	d := a.frameNow().Device.Rect
	if math.Min(d.W, d.H) < arrangeMinDevicePx-0.01 {
		t.Errorf("device shrank to %+v", d)
	}
}

func TestZeroCanvasProducesEmptyFrame(t *testing.T) {
	a := newArranger(config.HostSideLeft, "1080x2400") // host left ⇒ device drawn right
	a.setDisplays(twoDisplays)
	if f := a.frameNow(); len(f.Displays) != 0 || f.Side != config.HostSideRight {
		t.Errorf("frame before a canvas is known: %+v", f)
	}
}

func TestNewArrangerNormalizesSide(t *testing.T) {
	// An unknown host side falls back to "left" — host left of the device —
	// so the device is drawn on the right.
	if a := newArranger("sideways", "1080x2400"); a.side != config.HostSideRight {
		t.Errorf("unknown side drew the device on %q", a.side)
	}
}

// The regression that shipped as "my phone is on the left but it switches
// on the right": hostSide is where the *host* sits, so a phone drawn on the
// left must save "right", which the capture layer reads as "cross on the
// host's left border".
func TestPhoneOnTheLeftSavesHostOnTheRight(t *testing.T) {
	a := newArranger(config.HostSideRight, "720x1560") // saved: host right of phone
	a.setCanvas(600, 200)
	a.setDisplays(twoDisplays)
	f := a.frameNow()
	if f.Side != config.HostSideLeft || f.Device.Rect.X+f.Device.Rect.W > a.union.X {
		t.Fatalf("host on the right should draw the phone on the left; frame side %q, device %+v, union %+v", f.Side, f.Device.Rect, a.union)
	}
	if got := HostSideChoices[a.sideIndex()]; got != config.HostSideRight {
		t.Fatalf("saved side %q, want %q", got, config.HostSideRight)
	}
	// Drag it to the right of the desktop: the host is now on the left.
	cx, cy := f.Device.Rect.centre()
	a.beginDrag(cx, cy)
	a.endDrag(590, 100)
	if got := HostSideChoices[a.sideIndex()]; got != config.HostSideLeft {
		t.Fatalf("after dragging the phone to the right, saved side %q, want %q", got, config.HostSideLeft)
	}
}

func TestOppositeSide(t *testing.T) {
	pairs := map[string]string{
		config.HostSideLeft: config.HostSideRight, config.HostSideRight: config.HostSideLeft,
		config.HostSideTop: config.HostSideBottom, config.HostSideBottom: config.HostSideTop,
	}
	for side, want := range pairs {
		if got := OppositeSide(side); got != want {
			t.Errorf("OppositeSide(%q) = %q, want %q", side, got, want)
		}
		if OppositeSide(OppositeSide(side)) != side {
			t.Errorf("OppositeSide is not an involution for %q", side)
		}
	}
}
