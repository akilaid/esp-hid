//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

type inputEventKind int

const (
	inputMouseMoveEvent inputEventKind = iota + 1
	inputMouseDeltaEvent
	inputMouseLeftDownEvent
	inputMouseLeftUpEvent
	inputMouseRightDownEvent
	inputMouseRightUpEvent
	inputMouseScrollEvent
	inputKeyboardDownEvent
	inputKeyboardUpEvent
	inputRemoteModeChangedEvent
)

type inputEvent struct {
	kind    inputEventKind
	x       int
	y       int
	scroll  int
	keyCode uint8
	active  bool
	source  string
}

type movementAccumulator struct {
	mu          sync.Mutex
	initialized bool
	lastX       int
	lastY       int
	pendingDX   int
	pendingDY   int
}

func (a *movementAccumulator) addAbsolutePosition(x, y int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.initialized {
		a.initialized = true
		a.lastX = x
		a.lastY = y
		return
	}

	a.pendingDX += x - a.lastX
	a.pendingDY += y - a.lastY
	a.lastX = x
	a.lastY = y
}

func (a *movementAccumulator) addDelta(dx, dy int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.pendingDX += dx
	a.pendingDY += dy
}

func (a *movementAccumulator) drain() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	dx := a.pendingDX
	dy := a.pendingDY
	a.pendingDX = 0
	a.pendingDY = 0
	return dx, dy
}

func (a *movementAccumulator) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.initialized = false
	a.pendingDX = 0
	a.pendingDY = 0
}

type movementShaper struct {
	deadzone  int
	smoothing float64
	lastDX    int
	lastDY    int
}

func newMovementShaper(deadzone int, smoothing float64) *movementShaper {
	return &movementShaper{deadzone: deadzone, smoothing: smoothing}
}

func (shaper *movementShaper) reset() {
	shaper.lastDX = 0
	shaper.lastDY = 0
}

func (shaper *movementShaper) shape(dx int, dy int) (int, int) {
	if shaper.deadzone > 0 {
		if absInt(dx) <= shaper.deadzone {
			dx = 0
		}
		if absInt(dy) <= shaper.deadzone {
			dy = 0
		}
	}

	const microSmoothingLimit = 12
	if shaper.smoothing > 0 {
		if absInt(dx) <= microSmoothingLimit {
			dx = int(math.Round((1-shaper.smoothing)*float64(dx) + shaper.smoothing*float64(shaper.lastDX)))
		}
		if absInt(dy) <= microSmoothingLimit {
			dy = int(math.Round((1-shaper.smoothing)*float64(dy) + shaper.smoothing*float64(shaper.lastDY)))
		}
	}

	shaper.lastDX = dx
	shaper.lastDY = dy

	return dx, dy
}

type moveBackpressureController struct {
	enabled bool
	tick    uint64
}

func newMoveBackpressureController(enabled bool) *moveBackpressureController {
	return &moveBackpressureController{enabled: enabled}
}

func (controller *moveBackpressureController) allowSend(queue chan string) bool {
	if !controller.enabled {
		return true
	}

	queueCapacity := cap(queue)
	if queueCapacity <= 0 {
		return true
	}

	queueLength := len(queue)
	utilization := queueLength * 100 / queueCapacity

	if utilization >= 85 {
		return false
	}

	controller.tick++
	if utilization >= 65 {
		return controller.tick%3 == 0
	}
	if utilization >= 45 {
		return controller.tick%2 == 0
	}

	controller.tick = 0
	return true
}

type keyStateTracker struct {
	pressed [256]bool
}

func (tracker *keyStateTracker) reset() {
	for i := range tracker.pressed {
		tracker.pressed[i] = false
	}
}

func (tracker *keyStateTracker) onKeyDown(code uint8) bool {
	if tracker.pressed[code] {
		return false
	}

	tracker.pressed[code] = true
	return true
}

func (tracker *keyStateTracker) onKeyUp(code uint8) bool {
	if !tracker.pressed[code] {
		return false
	}

	tracker.pressed[code] = false
	return true
}

func enqueueCommand(queue chan string, command string) {
	select {
	case queue <- command:
		return
	default:
	}

	if strings.HasPrefix(command, "MOVE ") {
		return
	}

	select {
	case <-queue:
	default:
	}

	select {
	case queue <- command:
	default:
	}
}

func handleInputEvent(event inputEvent, accumulator *movementAccumulator, queue chan string, keys *keyStateTracker) {
	switch event.kind {
	case inputMouseMoveEvent:
		accumulator.addAbsolutePosition(event.x, event.y)
	case inputMouseDeltaEvent:
		accumulator.addDelta(event.x, event.y)
	case inputMouseLeftDownEvent:
		enqueueCommand(queue, "MOUSEDOWN LEFT")
	case inputMouseLeftUpEvent:
		enqueueCommand(queue, "MOUSEUP LEFT")
	case inputMouseRightDownEvent:
		enqueueCommand(queue, "MOUSEDOWN RIGHT")
	case inputMouseRightUpEvent:
		enqueueCommand(queue, "MOUSEUP RIGHT")
	case inputMouseScrollEvent:
		if event.scroll != 0 {
			enqueueCommand(queue, fmt.Sprintf("SCROLL %d", event.scroll))
		}
	case inputKeyboardDownEvent:
		if keys == nil || keys.onKeyDown(event.keyCode) {
			enqueueCommand(queue, fmt.Sprintf("KEYDOWN %d", event.keyCode))
		}
	case inputKeyboardUpEvent:
		if keys == nil || keys.onKeyUp(event.keyCode) {
			enqueueCommand(queue, fmt.Sprintf("KEYUP %d", event.keyCode))
		}
	}
}

func runCaptureLoop(ctx context.Context, cfg config, queue chan string, remoteActivationAllowed func() bool, reporter bridgeEventReporter) error {
	if cfg.moveRateHz <= 0 {
		return errors.New("move rate must be greater than 0")
	}

	accumulator := &movementAccumulator{}
	movementShaper := newMovementShaper(cfg.moveDeadzone, cfg.moveSmoothing)
	moveBackpressure := newMoveBackpressureController(cfg.adaptiveMoves)
	keys := &keyStateTracker{}
	eventChannel := make(chan inputEvent, 4096)
	errorChannel := make(chan error, 1)

	go func() {
		errorChannel <- runInputHooks(ctx, cfg.captureKeyboard, cfg.toggleHotkeyVK, eventChannel, remoteActivationAllowed)
		close(eventChannel)
		close(errorChannel)
	}()

	ticker := time.NewTicker(time.Second / time.Duration(cfg.moveRateHz))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case hookErr, ok := <-errorChannel:
			if ok && hookErr != nil {
				return hookErr
			}
			return nil
		case <-ticker.C:
			if !moveBackpressure.allowSend(queue) {
				continue
			}

			dx, dy := accumulator.drain()
			dx, dy = movementShaper.shape(dx, dy)
			if dx != 0 || dy != 0 {
				enqueueCommand(queue, fmt.Sprintf("MOVE %d %d", dx, dy))
			}
		case event, ok := <-eventChannel:
			if !ok {
				return nil
			}

			if event.kind == inputRemoteModeChangedEvent {
				eventType := bridgeEventRemoteModeOff
				if event.active {
					eventType = bridgeEventRemoteModeOn
				}
				emitBridgeEvent(reporter, eventType, "", event.source)

				accumulator.reset()
				movementShaper.reset()
				keys.reset()
				if !event.active {
					enqueueCommand(queue, "RELEASE")
					enqueueCommand(queue, "KEYRELEASE")
				}
				continue
			}

			handleInputEvent(event, accumulator, queue, keys)
		}
	}
}
