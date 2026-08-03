package capture

import (
	"testing"
	"time"
)

// Two 1920x1080 monitors side by side. The seam at x=1920 is the case the
// edge probe exists to reject: crossing between physical displays must never
// activate remote mode, only leaving the desktop entirely may.
var (
	monitorA = monitorRect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	monitorB = monitorRect{Left: 1920, Top: 0, Right: 3840, Bottom: 1080}
	desktop  = []monitorRect{monitorA, monitorB}
)

func TestOuterActivationEdgeIgnoresMonitorSeam(t *testing.T) {
	// Host on the left => activation edge is the right border. The right
	// border of monitor A is the seam with monitor B, so it must not activate.
	if isOuterActivationEdgePoint(point{X: 1919, Y: 540}, monitorA, desktop, HostSideLeft) {
		t.Error("seam between monitors activated remote mode; it must not")
	}
	// The right border of monitor B is the true outer edge of the desktop.
	if !isOuterActivationEdgePoint(point{X: 3839, Y: 540}, monitorB, desktop, HostSideLeft) {
		t.Error("outer desktop edge did not activate remote mode")
	}
}

func TestOuterActivationEdgeRequiresProximity(t *testing.T) {
	if isOuterActivationEdgePoint(point{X: 3000, Y: 540}, monitorB, desktop, HostSideLeft) {
		t.Error("a point far from the edge activated remote mode")
	}
}

func TestOuterActivationEdgePerSide(t *testing.T) {
	single := []monitorRect{monitorA}
	cases := []struct {
		side string
		p    point
		want bool
	}{
		{HostSideLeft, point{X: 1919, Y: 500}, true},
		{HostSideLeft, point{X: 0, Y: 500}, false},
		{HostSideRight, point{X: 0, Y: 500}, true},
		{HostSideRight, point{X: 1919, Y: 500}, false},
		{HostSideTop, point{X: 500, Y: 0}, true},
		{HostSideTop, point{X: 500, Y: 1079}, false},
		{HostSideBottom, point{X: 500, Y: 1079}, true},
		{HostSideBottom, point{X: 500, Y: 0}, false},
	}
	for _, tc := range cases {
		got := isOuterActivationEdgePoint(tc.p, monitorA, single, tc.side)
		if got != tc.want {
			t.Errorf("side %s point %v: got %v, want %v", tc.side, tc.p, got, tc.want)
		}
	}
}

func TestOuterActivationEdgeRejectsPointOutsideRect(t *testing.T) {
	if isOuterActivationEdgePoint(point{X: 5000, Y: 500}, monitorA, desktop, HostSideLeft) {
		t.Error("a point outside the rect must never activate")
	}
}

func TestReturnPointLandsInsideHostBorder(t *testing.T) {
	cases := []struct {
		side string
		want point
	}{
		{HostSideLeft, point{X: 1918, Y: 600}},
		{HostSideRight, point{X: 1, Y: 600}},
		{HostSideTop, point{X: 500, Y: 1}},
		{HostSideBottom, point{X: 500, Y: 1078}},
	}
	for _, tc := range cases {
		got := returnPointInRect(point{X: 500, Y: 600}, monitorA, tc.side)
		if got != tc.want {
			t.Errorf("side %s: got %v, want %v", tc.side, got, tc.want)
		}
	}
}

func TestReturnPointStaysInsideTinyRect(t *testing.T) {
	// A degenerate 1px rect must not produce a coordinate outside it.
	tiny := monitorRect{Left: 10, Top: 10, Right: 11, Bottom: 11}
	for _, side := range []string{HostSideLeft, HostSideRight, HostSideTop, HostSideBottom} {
		got := returnPointInRect(point{X: 10, Y: 10}, tiny, side)
		if !tiny.containsPoint(got) {
			t.Errorf("side %s: return point %v escaped rect %v", side, got, tiny)
		}
	}
}

func TestEntryInsetClamped(t *testing.T) {
	if got := entryInsetForAxis(1920); got != 160 { // 1920/12 = 160, at the cap
		t.Errorf("entryInsetForAxis(1920) = %d, want 160", got)
	}
	if got := entryInsetForAxis(4320); got != edgeEntryInsetMax {
		t.Errorf("large axis should clamp to %d, got %d", edgeEntryInsetMax, got)
	}
	if got := entryInsetForAxis(120); got != edgeEntryInsetMin {
		t.Errorf("small axis should clamp to %d, got %d", edgeEntryInsetMin, got)
	}
	// Never inset past the axis itself.
	if got := entryInsetForAxis(20); got >= 20 {
		t.Errorf("inset %d must be inside a 20px axis", got)
	}
}

func TestVirtualCursorEdgeEntryIsInsetFromEntryEdge(t *testing.T) {
	v := newVirtualCursor(1920, 1080, HostSideLeft)
	v.resetForActivation("edge")
	// Host on the left means the user crossed the slave's left border, so the
	// slave cursor must start inset from x=0 — landing on 0 would immediately
	// start building return pressure and bounce straight back.
	if v.x != 160 {
		t.Errorf("edge entry x = %d, want 160", v.x)
	}
	v.resetForActivation("hotkey")
	if v.x != 960 || v.y != 540 {
		t.Errorf("hotkey entry = (%d,%d), want (960,540)", v.x, v.y)
	}
}

func TestVirtualCursorBuildsAndDecaysReturnPressure(t *testing.T) {
	v := newVirtualCursor(1920, 1080, HostSideLeft)
	v.resetForActivation("edge") // x=160

	if v.move(-200, 0) {
		t.Fatal("40px of overflow should not reach the return threshold")
	}
	if v.pressure != 40 {
		t.Fatalf("pressure = %d, want 40", v.pressure)
	}
	if v.x != 0 {
		t.Fatalf("cursor should clamp to 0, got %d", v.x)
	}
	// Moving back inward decays pressure at twice the movement magnitude.
	if v.move(10, 0) {
		t.Fatal("inward movement must not trigger a return")
	}
	if v.pressure != 20 {
		t.Fatalf("pressure after decay = %d, want 20", v.pressure)
	}
	// A sustained shove past the edge crosses the threshold.
	if !v.move(-40, 0) {
		t.Fatalf("pressure %d should have reached threshold %d", v.pressure, edgeReturnPressureThreshold)
	}
}

func TestVirtualCursorOnlyBuildsPressureOnHostSide(t *testing.T) {
	// Host on the left: pushing right (away from the host) must never return.
	v := newVirtualCursor(1920, 1080, HostSideLeft)
	v.resetForActivation("hotkey")
	for i := 0; i < 50; i++ {
		if v.move(200, 0) {
			t.Fatal("pushing away from the host side must not trigger a return")
		}
	}
	if v.x != 1919 {
		t.Errorf("cursor should clamp to the far edge, got %d", v.x)
	}
}

func TestVirtualCursorRespectsHostSideRight(t *testing.T) {
	v := newVirtualCursor(1920, 1080, HostSideRight)
	v.resetForActivation("edge")
	if v.x != 1920-1-160 {
		t.Fatalf("edge entry x = %d, want %d", v.x, 1920-1-160)
	}
	returned := false
	for i := 0; i < 20; i++ {
		if v.move(60, 0) {
			returned = true
			break
		}
	}
	if !returned {
		t.Error("pushing right with the host on the right should return home")
	}
}

func TestVirtualCursorDefaultsInvalidSize(t *testing.T) {
	v := newVirtualCursor(0, -5, "nonsense")
	if v.width != 1920 || v.height != 1080 {
		t.Errorf("bad size should fall back to 1920x1080, got %dx%d", v.width, v.height)
	}
	if v.hostSide != HostSideLeft {
		t.Errorf("bad host side should fall back to left, got %q", v.hostSide)
	}
}

func TestLeftwardReturnNeedsSustainedSwipe(t *testing.T) {
	l := &leftwardReturnTracker{enabled: true, hostSide: HostSideLeft}
	now := time.Now()
	triggered := false
	for i := 0; i < leftwardReturnThreshold/100+1; i++ {
		if l.update(-100, 0, now.Add(time.Duration(i)*10*time.Millisecond)) {
			triggered = true
			break
		}
	}
	if !triggered {
		t.Error("a sustained left swipe should trigger the return gesture")
	}
}

func TestLeftwardReturnRejectsDiagonalAndSlowDrift(t *testing.T) {
	now := time.Now()

	l := &leftwardReturnTracker{enabled: true, hostSide: HostSideLeft}
	for i := 0; i < 50; i++ {
		if l.update(-100, 300, now) { // dy far exceeds 2*|dx|
			t.Fatal("a diagonal drag must not trigger the return gesture")
		}
	}

	// Steps below the minimum are ignored entirely.
	l2 := &leftwardReturnTracker{enabled: true, hostSide: HostSideLeft}
	for i := 0; i < 500; i++ {
		if l2.update(-1, 0, now) {
			t.Fatal("sub-threshold steps must not accumulate into a return")
		}
	}
	if l2.distance != 0 {
		t.Errorf("distance = %d, want 0", l2.distance)
	}
}

func TestLeftwardReturnWindowExpires(t *testing.T) {
	l := &leftwardReturnTracker{enabled: true, hostSide: HostSideLeft}
	start := time.Now()
	l.update(-500, 0, start)
	// The next step lands outside the rolling window, so the accumulated
	// distance restarts rather than combining into a false positive.
	if l.update(-500, 0, start.Add(leftwardReturnWindow+time.Millisecond)) {
		t.Error("steps separated by more than the window must not combine")
	}
	if l.distance != 500 {
		t.Errorf("distance = %d, want 500 (window restarted)", l.distance)
	}
}

func TestLeftwardReturnDisabledOrWrongSide(t *testing.T) {
	now := time.Now()
	off := &leftwardReturnTracker{enabled: false, hostSide: HostSideLeft}
	wrongSide := &leftwardReturnTracker{enabled: true, hostSide: HostSideRight}
	for i := 0; i < 50; i++ {
		if off.update(-100, 0, now) {
			t.Fatal("disabled tracker must never trigger")
		}
		if wrongSide.update(-100, 0, now) {
			t.Fatal("the gesture only applies when the host is on the left")
		}
	}
}

func TestNormalizeHostSide(t *testing.T) {
	for _, side := range []string{HostSideLeft, HostSideRight, HostSideTop, HostSideBottom} {
		if got := normalizeHostSide(side); got != side {
			t.Errorf("normalizeHostSide(%q) = %q", side, got)
		}
	}
	if got := normalizeHostSide("diagonal"); got != HostSideLeft {
		t.Errorf("unknown side should fall back to left, got %q", got)
	}
}

func TestMonitorRectHelpers(t *testing.T) {
	if !monitorA.containsPoint(point{X: 0, Y: 0}) {
		t.Error("top-left corner should be inside")
	}
	if monitorA.containsPoint(point{X: 1920, Y: 0}) {
		t.Error("right edge is exclusive and must be outside")
	}
	if got := monitorA.centerPoint(); got != (point{X: 960, Y: 540}) {
		t.Errorf("centerPoint = %v", got)
	}
}

// pushUntil feeds the tracker `steps` events of (dx, dy) and reports whether
// the crossing armed at any point.
func pushUntil(p *edgeEntryPressure, dx, dy, steps int, hostSide string, start time.Time) bool {
	for i := 0; i < steps; i++ {
		if p.push(dx, dy, hostSide, start.Add(time.Duration(i)*10*time.Millisecond)) {
			return true
		}
	}
	return false
}

func TestEdgeEntryPressureNeedsASustainedPush(t *testing.T) {
	start := time.Now()

	// Arriving at the edge and stopping: a couple of events' worth of
	// deceleration is what reaching for a close button looks like.
	var nudge edgeEntryPressure
	if pushUntil(&nudge, 40, 0, 2, HostSideLeft, start) {
		t.Error("a brief nudge into the edge must not arm the crossing")
	}

	// Leaning on it: the pointer is stuck, so every event is deliberate.
	var shove edgeEntryPressure
	if !pushUntil(&shove, 40, 0, 8, HostSideLeft, start) {
		t.Error("a sustained push should arm the crossing")
	}
}

func TestEdgeEntryPressureOnlyCountsOutwardMotion(t *testing.T) {
	start := time.Now()

	// Sliding along the border — down the right edge towards a scrollbar —
	// must never accumulate, however far it goes.
	var along edgeEntryPressure
	if pushUntil(&along, 0, 60, 40, HostSideLeft, start) {
		t.Error("motion along the edge must not build entry pressure")
	}

	// Nor must motion away from it.
	var away edgeEntryPressure
	if pushUntil(&away, -60, 0, 40, HostSideLeft, start) {
		t.Error("motion away from the edge must not build entry pressure")
	}
}

func TestEdgeEntryPressurePerHostSide(t *testing.T) {
	start := time.Now()
	// Outward is the border opposite the host, matching
	// isOuterActivationEdgePoint.
	cases := []struct {
		hostSide string
		dx, dy   int
	}{
		{HostSideLeft, 40, 0},   // cross at the right border
		{HostSideRight, -40, 0}, // cross at the left border
		{HostSideTop, 0, -40},   // cross at the top border
		{HostSideBottom, 0, 40}, // cross at the bottom border
	}
	for _, tc := range cases {
		var outward, inward edgeEntryPressure
		if !pushUntil(&outward, tc.dx, tc.dy, 8, tc.hostSide, start) {
			t.Errorf("host %s: pushing (%d,%d) should arm", tc.hostSide, tc.dx, tc.dy)
		}
		if pushUntil(&inward, -tc.dx, -tc.dy, 8, tc.hostSide, start) {
			t.Errorf("host %s: pushing (%d,%d) is inward and must not arm",
				tc.hostSide, -tc.dx, -tc.dy)
		}
	}
}

func TestEdgeEntryPressureDecaysWhenPushingStops(t *testing.T) {
	start := time.Now()
	almost := edgeEntryPressureThreshold - 40

	// Control: without a pause, one more shove tips it over. This is what the
	// timing case below has to defeat, so assert it rather than assume it.
	var continuous edgeEntryPressure
	if continuous.push(almost, 0, HostSideLeft, start) {
		t.Fatal("the first shove should stop just short of the threshold")
	}
	if !continuous.push(40, 0, HostSideLeft, start.Add(10*time.Millisecond)) {
		t.Fatal("a second shove inside the window should arm")
	}

	// The real case: rest against the border for longer than the window and
	// the accumulated pressure is gone, so a pointer parked on an edge can
	// never creep over the line.
	var paused edgeEntryPressure
	if paused.push(almost, 0, HostSideLeft, start) {
		t.Fatal("the first shove should stop just short of the threshold")
	}
	if paused.push(40, 0, HostSideLeft, start.Add(edgeEntryPressureWindow+time.Millisecond)) {
		t.Error("pressure must not survive a pause longer than the window")
	}
}

func TestEdgeEntryPressureResets(t *testing.T) {
	var p edgeEntryPressure
	start := time.Now()
	if pushUntil(&p, 40, 0, 4, HostSideLeft, start) {
		t.Fatal("four steps should not reach the threshold yet")
	}
	p.reset()
	if pushUntil(&p, 40, 0, 4, HostSideLeft, start) {
		t.Error("reset should discard accumulated pressure")
	}
}
