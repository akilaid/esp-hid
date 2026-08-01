package capture

import "time"

// Tuning constants. These are the values the legacy Windows sender was tuned
// to in practice; they are shared verbatim by every platform so remote mode
// feels the same everywhere.
const (
	// How close to the screen border the cursor must be to arm edge entry.
	hostEdgeActivationThreshold = 1
	// Accumulated overflow past the slave's far edge needed to snap home.
	edgeReturnPressureThreshold = 48
	// Where the slave cursor lands on edge entry, as an inset from that edge.
	edgeEntryInsetMin = 24
	edgeEntryInsetMax = 160
	// Left-swipe return gesture.
	leftwardReturnMinStep   = 6
	leftwardReturnThreshold = 900
	leftwardReturnWindow    = 450 * time.Millisecond
)

type point struct {
	X int32
	Y int32
}

// monitorRect is a half-open rectangle: Right and Bottom are exclusive, which
// matches the Win32 RECT convention and CGDisplayBounds alike.
type monitorRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (rect monitorRect) containsPoint(p point) bool {
	return p.X >= rect.Left && p.X < rect.Right && p.Y >= rect.Top && p.Y < rect.Bottom
}

func (rect monitorRect) centerPoint() point {
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	return point{X: rect.Left + width/2, Y: rect.Top + height/2}
}

func clampInt32(value, minValue, maxValue int32) int32 {
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

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// normalizeHostSide falls back to the left side for any unrecognized value.
func normalizeHostSide(hostSide string) string {
	switch hostSide {
	case HostSideLeft, HostSideRight, HostSideTop, HostSideBottom:
		return hostSide
	default:
		return HostSideLeft
	}
}

func findMonitorForPoint(p point, monitorRects []monitorRect) (monitorRect, bool) {
	for _, rect := range monitorRects {
		if rect.containsPoint(p) {
			return rect, true
		}
	}
	return monitorRect{}, false
}

func pointInsideAnyMonitor(p point, monitorRects []monitorRect) bool {
	_, found := findMonitorForPoint(p, monitorRects)
	return found
}

// isOuterActivationEdgePoint reports whether p sits on the activation edge of
// its monitor AND that edge is the outer boundary of the whole desktop — it
// probes one pixel past the edge and requires that point to be outside every
// monitor, so seams between physical monitors never activate.
//
// Passing a nil monitorRects with rect set to the whole virtual desktop is the
// single-monitor fallback: nothing is ever "inside another monitor", so only
// the true outer border activates.
func isOuterActivationEdgePoint(p point, rect monitorRect, monitorRects []monitorRect, hostSide string) bool {
	if !rect.containsPoint(p) {
		return false
	}
	switch hostSide {
	case HostSideRight:
		activationX := rect.Left + hostEdgeActivationThreshold
		if p.X > activationX {
			return false
		}
		sampleY := clampInt32(p.Y, rect.Top, rect.Bottom-1)
		return !pointInsideAnyMonitor(point{X: rect.Left - 1, Y: sampleY}, monitorRects)
	case HostSideTop:
		activationY := rect.Top + hostEdgeActivationThreshold
		if p.Y > activationY {
			return false
		}
		sampleX := clampInt32(p.X, rect.Left, rect.Right-1)
		return !pointInsideAnyMonitor(point{X: sampleX, Y: rect.Top - 1}, monitorRects)
	case HostSideBottom:
		activationY := rect.Bottom - 1 - hostEdgeActivationThreshold
		if p.Y < activationY {
			return false
		}
		sampleX := clampInt32(p.X, rect.Left, rect.Right-1)
		return !pointInsideAnyMonitor(point{X: sampleX, Y: rect.Bottom}, monitorRects)
	default: // host on the left: activation edge is the right border
		activationX := rect.Right - 1 - hostEdgeActivationThreshold
		if p.X < activationX {
			return false
		}
		sampleY := clampInt32(p.Y, rect.Top, rect.Bottom-1)
		return !pointInsideAnyMonitor(point{X: rect.Right, Y: sampleY}, monitorRects)
	}
}

// returnPointInRect picks where the real cursor lands when remote mode exits:
// just inside the host-side border of rect, on the same row/column the user
// was on, so the pointer reappears next to where it crossed over.
func returnPointInRect(current point, rect monitorRect, hostSide string) point {
	targetX := clampInt32(current.X, rect.Left, rect.Right-1)
	targetY := clampInt32(current.Y, rect.Top, rect.Bottom-1)
	switch hostSide {
	case HostSideRight:
		targetX = rect.Left + 1
		if targetX >= rect.Right {
			targetX = rect.Left
		}
	case HostSideTop:
		targetY = rect.Top + 1
		if targetY >= rect.Bottom {
			targetY = rect.Top
		}
	case HostSideBottom:
		targetY = rect.Bottom - 2
		if targetY < rect.Top {
			targetY = rect.Top
		}
	default:
		targetX = rect.Right - 2
		if targetX < rect.Left {
			targetX = rect.Left
		}
	}
	return point{X: targetX, Y: targetY}
}

// entryInsetForAxis keeps an edge entry from landing exactly on the slave's
// far border, which would immediately start building return pressure.
func entryInsetForAxis(axisLength int) int {
	inset := axisLength / 12
	if inset < edgeEntryInsetMin {
		inset = edgeEntryInsetMin
	}
	if inset > edgeEntryInsetMax {
		inset = edgeEntryInsetMax
	}
	if inset >= axisLength {
		inset = axisLength / 2
	}
	if inset < 0 {
		inset = 0
	}
	return inset
}

// virtualCursor dead-reckons where the pointer is on the slave device. The
// host cannot observe the slave's real cursor, so this model is the only way
// to know the user has pushed back against the edge they came in through.
type virtualCursor struct {
	width    int
	height   int
	hostSide string

	x        int
	y        int
	pressure int
}

func newVirtualCursor(width, height int, hostSide string) *virtualCursor {
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	return &virtualCursor{
		width:    width,
		height:   height,
		hostSide: normalizeHostSide(hostSide),
		x:        width / 2,
		y:        height / 2,
	}
}

func (v *virtualCursor) resetPressure() {
	v.pressure = 0
}

// resetForActivation places the slave cursor for a fresh entry. Entering by
// hotkey starts at the center; entering by edge starts inset from the edge
// the user crossed, so a slight overshoot does not bounce straight back.
func (v *virtualCursor) resetForActivation(source string) {
	entryX := v.width / 2
	entryY := v.height / 2
	if source == "edge" {
		insetX := entryInsetForAxis(v.width)
		insetY := entryInsetForAxis(v.height)
		switch v.hostSide {
		case HostSideLeft:
			entryX = insetX
		case HostSideRight:
			entryX = v.width - 1 - insetX
		case HostSideTop:
			entryY = insetY
		case HostSideBottom:
			entryY = v.height - 1 - insetY
		}
	}
	v.x = clampInt(entryX, 0, v.width-1)
	v.y = clampInt(entryY, 0, v.height-1)
	v.pressure = 0
}

// move advances the dead-reckoned cursor and reports whether the user has
// pushed hard enough past the host-facing edge to warrant returning home.
// Pressure builds with overflow and decays at twice the rate of any other
// motion, so a deliberate shove returns but incidental drift does not.
func (v *virtualCursor) move(dx, dy int) bool {
	nextX := v.x + dx
	nextY := v.y + dy
	overflow := 0
	switch v.hostSide {
	case HostSideLeft:
		if nextX < 0 && dx < 0 {
			overflow = -nextX
		}
	case HostSideRight:
		if nextX >= v.width && dx > 0 {
			overflow = nextX - (v.width - 1)
		}
	case HostSideTop:
		if nextY < 0 && dy < 0 {
			overflow = -nextY
		}
	case HostSideBottom:
		if nextY >= v.height && dy > 0 {
			overflow = nextY - (v.height - 1)
		}
	}
	if overflow > 0 {
		v.pressure += overflow
	} else {
		decay := absInt(dx) + absInt(dy)
		if decay < 1 {
			decay = 1
		}
		v.pressure -= decay * 2
		if v.pressure < 0 {
			v.pressure = 0
		}
	}
	v.x = clampInt(nextX, 0, v.width-1)
	v.y = clampInt(nextY, 0, v.height-1)
	return v.pressure >= edgeReturnPressureThreshold
}

// leftwardReturnTracker implements the optional left-swipe return gesture: a
// deliberate fast drag left, only meaningful when the host is on the left.
type leftwardReturnTracker struct {
	enabled  bool
	hostSide string

	distance    int
	windowStart time.Time
}

func (l *leftwardReturnTracker) reset() {
	l.distance = 0
	l.windowStart = time.Time{}
}

func (l *leftwardReturnTracker) update(dx, dy int, now time.Time) bool {
	if !l.enabled || l.hostSide != HostSideLeft {
		return false
	}
	if dx >= 0 {
		l.reset()
		return false
	}
	if absInt(dx) < leftwardReturnMinStep {
		return false
	}
	// Reject diagonal drift: a return swipe is meant to be horizontal.
	if absInt(dy) > absInt(dx)*2 {
		l.reset()
		return false
	}
	if l.distance == 0 || l.windowStart.IsZero() {
		l.windowStart = now
	} else if now.Sub(l.windowStart) > leftwardReturnWindow {
		l.distance = 0
		l.windowStart = now
	}
	l.distance += -dx
	if l.distance < 0 {
		l.distance = 0
	}
	return l.distance >= leftwardReturnThreshold
}

func clampInt(value, minValue, maxValue int) int {
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
