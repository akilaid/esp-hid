//go:build windows || darwin

// Package bridge wires capture -> shaping pipeline -> device link and
// exposes a start/stop lifecycle plus a unified event stream for the UI.
//
// Nothing in this file is platform-specific; the build tag only tracks which
// platforms internal/capture can supply a Run implementation for.
package bridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"esp-hid/host/internal/capture"
	"esp-hid/host/internal/config"
	"esp-hid/host/internal/core"
	"esp-hid/host/internal/device"
	"esp-hid/host/internal/protocol"
)

// EventKind discriminates runtime events for the UI.
type EventKind int

const (
	EventStarting EventKind = iota
	EventSerialConnected
	EventSerialDown
	EventHello
	EventBleState
	EventDeviceError
	EventRemoteMode
	EventCaptureError
	EventStopped
	EventLog
	// EventPermissionRequired is a capture failure the user can fix: macOS
	// withheld Accessibility or Input Monitoring. It is separated from
	// EventCaptureError so the UI can offer the fix instead of an error box.
	EventPermissionRequired
)

// Event is a unified runtime event.
type Event struct {
	Kind     EventKind
	Port     string
	Detail   string
	Hello    protocol.Hello
	BleState protocol.BleState
	Active   bool // EventRemoteMode
}

// Runtime owns one bridge session.
type Runtime struct {
	events chan<- Event

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	link    *device.Link
	running bool
}

// New creates a runtime reporting on the given channel.
func New(events chan<- Event) *Runtime {
	return &Runtime{events: events}
}

// Running reports whether a session is active.
func (r *Runtime) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// ClearBonds asks the device to wipe its BLE bonds.
func (r *Runtime) ClearBonds() {
	r.mu.Lock()
	link := r.link
	r.mu.Unlock()
	if link != nil {
		link.Enqueue(protocol.EncodeClearBonds())
	}
}

// Start launches the capture, pipeline, and link goroutines.
func (r *Runtime) Start(cfg config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return fmt.Errorf("bridge already running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	deviceEvents := make(chan device.Event, 64)
	captureEvents := make(chan capture.Event, 4096)
	captureErrors := make(chan error, 1)

	link := device.New(deviceEvents, cfg.PortOverride)
	go link.Run()

	// Remote mode may engage only while the serial link is up AND the device
	// reports a BLE host — swallowing input that goes nowhere is the one
	// state this must never allow.
	activationAllowed := func() bool {
		return link.SerialUp() && link.BleConnected()
	}
	go func() {
		opts := capture.Options{
			CaptureKeyboard:   cfg.CaptureKeyboard,
			ToggleHotkey:      cfg.ToggleHotkey,
			LeftwardReturn:    cfg.LeftwardReturn,
			SlaveWidth:        cfg.SlaveWidth,
			SlaveHeight:       cfg.SlaveHeight,
			HostSide:          cfg.HostSide,
			AutoSwitch:        cfg.AutoSwitch,
			DebugStallCapture: cfg.DebugStallCapture,
		}
		if err := capture.Run(ctx, opts, captureEvents, activationAllowed); err != nil {
			select {
			case captureErrors <- err:
			default:
			}
		}
	}()

	done := make(chan struct{})
	go r.pump(ctx, cfg, link, deviceEvents, captureEvents, captureErrors, done)

	r.cancel = cancel
	r.done = done
	r.link = link
	r.running = true
	r.emit(Event{Kind: EventStarting})
	return nil
}

// Stop tears the session down and waits for the pump to finish.
func (r *Runtime) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	cancel := r.cancel
	done := r.done
	link := r.link
	r.running = false
	r.cancel = nil
	r.link = nil
	r.mu.Unlock()

	// Best effort: release everything on the device before closing.
	link.Enqueue(protocol.EncodeReleaseAll())
	time.Sleep(50 * time.Millisecond)
	cancel()
	link.Close()
	<-done
	r.emit(Event{Kind: EventStopped})
}

func (r *Runtime) emit(event Event) {
	select {
	case r.events <- event:
	default:
	}
}

func (r *Runtime) pump(
	ctx context.Context,
	cfg config.Config,
	link *device.Link,
	deviceEvents <-chan device.Event,
	captureEvents <-chan capture.Event,
	captureErrors <-chan error,
	done chan<- struct{},
) {
	defer close(done)

	accumulator := &core.MovementAccumulator{}
	shaper := &core.MovementShaper{Deadzone: cfg.MoveDeadzone, Smoothing: cfg.MoveSmoothing}
	backpressure := &core.Backpressure{Enabled: cfg.AdaptiveMoves}
	keys := &core.KeyTracker{}
	var buttons byte

	ticker := time.NewTicker(time.Second / time.Duration(cfg.MoveRateHz))
	defer ticker.Stop()

	releaseAll := func() {
		buttons = 0
		keys.Reset()
		accumulator.Reset()
		shaper.Reset()
		link.Enqueue(protocol.EncodeReleaseAll())
	}

	for {
		select {
		case <-ctx.Done():
			return

		case err := <-captureErrors:
			kind := EventCaptureError
			if errors.Is(err, capture.ErrPermissionDenied) {
				kind = EventPermissionRequired
			}
			r.emit(Event{Kind: kind, Detail: err.Error()})
			return

		case event := <-deviceEvents:
			switch event.Kind {
			case device.EventConnected:
				r.emit(Event{Kind: EventSerialConnected, Port: event.Port})
			case device.EventDisconnected:
				r.emit(Event{Kind: EventSerialDown, Detail: event.Detail})
			case device.EventHello:
				r.emit(Event{Kind: EventHello, Hello: event.Hello})
			case device.EventBleState:
				r.emit(Event{Kind: EventBleState, BleState: event.BleState})
			case device.EventDeviceError:
				r.emit(Event{Kind: EventDeviceError,
					Detail: fmt.Sprintf("code %d detail 0x%02X", event.ErrCode, event.ErrData)})
			case device.EventLog:
				r.emit(Event{Kind: EventLog, Detail: event.Detail})
			}

		case event := <-captureEvents:
			switch event.Kind {
			case capture.EventMouseDelta:
				accumulator.Add(event.DX, event.DY)
			case capture.EventButtonDown:
				buttons |= event.Button
				link.Enqueue(protocol.EncodeButtons(buttons))
			case capture.EventButtonUp:
				buttons &^= event.Button
				link.Enqueue(protocol.EncodeButtons(buttons))
			case capture.EventScroll:
				link.Enqueue(protocol.EncodeWheel(
					core.ClampWheel(event.ScrollV), core.ClampWheel(event.ScrollH)))
			case capture.EventKeyDown:
				if keys.OnKeyDown(event.Usage) {
					link.Enqueue(protocol.EncodeKeyDown(event.Usage))
				}
			case capture.EventKeyUp:
				if keys.OnKeyUp(event.Usage) {
					link.Enqueue(protocol.EncodeKeyUp(event.Usage))
				}
			case capture.EventRemoteMode:
				if !event.Active {
					// Leaving remote mode: nothing may stay pressed on the
					// far side, and stale motion must not replay later.
					releaseAll()
				} else {
					accumulator.Reset()
					shaper.Reset()
				}
				r.emit(Event{Kind: EventRemoteMode, Active: event.Active, Detail: event.Source})
			}

		case <-ticker.C:
			dx, dy := accumulator.Drain()
			if dx == 0 && dy == 0 {
				continue
			}
			dx, dy = shaper.Shape(dx, dy)
			if dx == 0 && dy == 0 {
				continue
			}
			if !backpressure.AllowSend(link.QueueUtilization()) {
				continue
			}
			link.EnqueueMove(protocol.EncodeMove(core.ClampMove(dx), core.ClampMove(dy)))
		}
	}
}
