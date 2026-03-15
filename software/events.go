//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
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

func handleInputEvent(event inputEvent, accumulator *movementAccumulator, queue chan string) {
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
		enqueueCommand(queue, fmt.Sprintf("KEYDOWN %d", event.keyCode))
	case inputKeyboardUpEvent:
		enqueueCommand(queue, fmt.Sprintf("KEYUP %d", event.keyCode))
	}
}

func runCaptureLoop(ctx context.Context, cfg config, queue chan string) error {
	if cfg.moveRateHz <= 0 {
		return errors.New("move rate must be greater than 0")
	}

	accumulator := &movementAccumulator{}
	eventChannel := make(chan inputEvent, 4096)
	errorChannel := make(chan error, 1)

	go func() {
		errorChannel <- runInputHooks(ctx, cfg.captureKeyboard, eventChannel)
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
			dx, dy := accumulator.drain()
			if dx != 0 || dy != 0 {
				enqueueCommand(queue, fmt.Sprintf("MOVE %d %d", dx, dy))
			}
		case event, ok := <-eventChannel:
			if !ok {
				return nil
			}

			if event.kind == inputRemoteModeChangedEvent {
				accumulator.reset()
				if !event.active {
					enqueueCommand(queue, "RELEASE")
					enqueueCommand(queue, "KEYRELEASE")
				}
				continue
			}

			handleInputEvent(event, accumulator, queue)
		}
	}
}
