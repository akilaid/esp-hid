// Package core implements the input-shaping pipeline between OS capture and
// the device link: movement accumulation, shaping, congestion backpressure,
// and key-state tracking. Ported from the legacy software/events.go with the
// tuning constants preserved.
package core

import "sync"

// MovementAccumulator batches mouse deltas between ticker drains.
type MovementAccumulator struct {
	mu sync.Mutex
	dx int
	dy int
}

func (a *MovementAccumulator) Add(dx, dy int) {
	a.mu.Lock()
	a.dx += dx
	a.dy += dy
	a.mu.Unlock()
}

func (a *MovementAccumulator) Drain() (int, int) {
	a.mu.Lock()
	dx, dy := a.dx, a.dy
	a.dx, a.dy = 0, 0
	a.mu.Unlock()
	return dx, dy
}

func (a *MovementAccumulator) Reset() {
	a.mu.Lock()
	a.dx, a.dy = 0, 0
	a.mu.Unlock()
}

// MovementShaper applies a per-axis deadzone and micro-smoothing. Smoothing
// only touches small deltas (|d| <= microSmoothingLimit): fast movement
// passes through unfiltered, slow precise movement is stabilized.
type MovementShaper struct {
	Deadzone  int
	Smoothing float64

	lastDX int
	lastDY int
}

const microSmoothingLimit = 12

func (s *MovementShaper) Shape(dx, dy int) (int, int) {
	if s.Deadzone > 0 {
		if abs(dx) <= s.Deadzone {
			dx = 0
		}
		if abs(dy) <= s.Deadzone {
			dy = 0
		}
	}
	if s.Smoothing > 0 {
		if dx != 0 && abs(dx) <= microSmoothingLimit {
			dx = roundMix(dx, s.lastDX, s.Smoothing)
		}
		if dy != 0 && abs(dy) <= microSmoothingLimit {
			dy = roundMix(dy, s.lastDY, s.Smoothing)
		}
	}
	s.lastDX = dx
	s.lastDY = dy
	return dx, dy
}

func (s *MovementShaper) Reset() {
	s.lastDX = 0
	s.lastDY = 0
}

func roundMix(current, last int, smoothing float64) int {
	mixed := (1-smoothing)*float64(current) + smoothing*float64(last)
	if mixed >= 0 {
		return int(mixed + 0.5)
	}
	return int(mixed - 0.5)
}

// Backpressure throttles movement sends by queue congestion. Tiers preserved
// from the legacy controller: >=85% drop, >=65% one in three, >=45% one in
// two, otherwise send everything.
type Backpressure struct {
	Enabled bool
	tick    int
}

func (b *Backpressure) AllowSend(utilizationPercent int) bool {
	if !b.Enabled {
		return true
	}
	switch {
	case utilizationPercent >= 85:
		return false
	case utilizationPercent >= 65:
		b.tick++
		return b.tick%3 == 0
	case utilizationPercent >= 45:
		b.tick++
		return b.tick%2 == 0
	default:
		b.tick = 0
		return true
	}
}

// KeyTracker de-duplicates OS auto-repeat, indexed by HID usage.
type KeyTracker struct {
	down [256]bool
}

// OnKeyDown reports whether the event should be forwarded (i.e. the key was
// not already down).
func (k *KeyTracker) OnKeyDown(usage byte) bool {
	if k.down[usage] {
		return false
	}
	k.down[usage] = true
	return true
}

// OnKeyUp reports whether the event should be forwarded.
func (k *KeyTracker) OnKeyUp(usage byte) bool {
	if !k.down[usage] {
		return false
	}
	k.down[usage] = false
	return true
}

func (k *KeyTracker) Reset() {
	k.down = [256]bool{}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ClampMove limits an accumulated delta to the protocol's int16 range.
func ClampMove(v int) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// ClampWheel limits a wheel delta to the protocol's int8 range.
func ClampWheel(v int) int8 {
	if v > 127 {
		return 127
	}
	if v < -127 {
		return -127
	}
	return int8(v)
}
