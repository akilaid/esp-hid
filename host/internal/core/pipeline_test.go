package core

import "testing"

func TestAccumulatorDrainsAndResets(t *testing.T) {
	var acc MovementAccumulator
	acc.Add(3, -2)
	acc.Add(1, 1)
	dx, dy := acc.Drain()
	if dx != 4 || dy != -1 {
		t.Errorf("drain = (%d,%d), want (4,-1)", dx, dy)
	}
	if dx, dy = acc.Drain(); dx != 0 || dy != 0 {
		t.Errorf("second drain = (%d,%d), want zero", dx, dy)
	}
	acc.Add(5, 5)
	acc.Reset()
	if dx, dy = acc.Drain(); dx != 0 || dy != 0 {
		t.Errorf("post-reset drain = (%d,%d), want zero", dx, dy)
	}
}

func TestShaperDeadzone(t *testing.T) {
	shaper := MovementShaper{Deadzone: 1}
	if dx, dy := shaper.Shape(1, -1); dx != 0 || dy != 0 {
		t.Errorf("deadzone leaked (%d,%d)", dx, dy)
	}
	if dx, _ := shaper.Shape(2, 0); dx != 2 {
		t.Errorf("dx=2 should pass deadzone, got %d", dx)
	}
}

func TestShaperSmoothsOnlySmallDeltas(t *testing.T) {
	shaper := MovementShaper{Smoothing: 0.2}
	// Large delta untouched.
	if dx, _ := shaper.Shape(50, 0); dx != 50 {
		t.Errorf("large delta shaped to %d", dx)
	}
	// Small delta mixed with previous (50): 0.8*4 + 0.2*50 = 13.2 -> 13.
	if dx, _ := shaper.Shape(4, 0); dx != 13 {
		t.Errorf("smoothed delta = %d, want 13", dx)
	}
}

func TestBackpressureTiers(t *testing.T) {
	bp := Backpressure{Enabled: true}
	if bp.AllowSend(90) {
		t.Error(">=85%% must drop")
	}
	// >=65%: one in three.
	sent := 0
	bp = Backpressure{Enabled: true}
	for i := 0; i < 9; i++ {
		if bp.AllowSend(70) {
			sent++
		}
	}
	if sent != 3 {
		t.Errorf("65%% tier sent %d of 9, want 3", sent)
	}
	// >=45%: one in two.
	bp = Backpressure{Enabled: true}
	sent = 0
	for i := 0; i < 8; i++ {
		if bp.AllowSend(50) {
			sent++
		}
	}
	if sent != 4 {
		t.Errorf("45%% tier sent %d of 8, want 4", sent)
	}
	// Low utilization always sends and resets the cadence.
	if !bp.AllowSend(10) {
		t.Error("low utilization must send")
	}
	// Disabled: always send.
	bp = Backpressure{Enabled: false}
	if !bp.AllowSend(100) {
		t.Error("disabled backpressure must always send")
	}
}

func TestKeyTrackerDedup(t *testing.T) {
	var keys KeyTracker
	if !keys.OnKeyDown(0x04) {
		t.Error("first down suppressed")
	}
	if keys.OnKeyDown(0x04) {
		t.Error("auto-repeat not suppressed")
	}
	if !keys.OnKeyUp(0x04) {
		t.Error("up suppressed")
	}
	if keys.OnKeyUp(0x04) {
		t.Error("spurious up not suppressed")
	}
}

func TestClamps(t *testing.T) {
	if ClampMove(100000) != 32767 || ClampMove(-100000) != -32768 {
		t.Error("move clamp broken")
	}
	if ClampWheel(300) != 127 || ClampWheel(-300) != -127 {
		t.Error("wheel clamp broken")
	}
}
