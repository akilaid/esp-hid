//go:build windows

package main

import (
	"context"
	"sync"
	"sync/atomic"
)

type bridgeRuntime struct {
	mu       sync.Mutex
	cfg      config
	reporter bridgeEventReporter

	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func newBridgeRuntime(cfg config, reporter bridgeEventReporter) *bridgeRuntime {
	return &bridgeRuntime{
		cfg:      cfg,
		reporter: reporter,
	}
}

func (runtime *bridgeRuntime) Running() bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.running
}

func (runtime *bridgeRuntime) Start() error {
	runtime.mu.Lock()
	if runtime.running {
		runtime.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	commandQueue := make(chan string, 1024)
	done := make(chan struct{})

	runtime.running = true
	runtime.cancel = cancel
	runtime.done = done
	cfg := runtime.cfg
	reporter := runtime.reporter
	runtime.mu.Unlock()

	var serialConnected atomic.Bool
	serialReporter := func(event bridgeEvent) {
		updateSerialConnectionState(&serialConnected, event.Type)
		if reporter != nil {
			reporter(event)
		}
	}

	emitBridgeEvent(serialReporter, bridgeEventStarting, cfg.portName, "bridge starting")

	go func() {
		writeLoop(ctx, cfg, commandQueue, serialReporter)
	}()

	go func() {
		defer close(done)

		if err := runCaptureLoop(ctx, cfg, commandQueue, serialConnected.Load, serialReporter); err != nil {
			emitBridgeEvent(serialReporter, bridgeEventCaptureError, "", err.Error())
		}

		enqueueCommand(commandQueue, "RELEASE")
		enqueueCommand(commandQueue, "KEYRELEASE")
		close(commandQueue)
		cancel()

		runtime.mu.Lock()
		runtime.running = false
		runtime.cancel = nil
		runtime.done = nil
		runtime.mu.Unlock()

		emitBridgeEvent(serialReporter, bridgeEventStopped, "", "bridge stopped")
	}()

	// Trigger writer connection path quickly.
	enqueueCommand(commandQueue, "RELEASE")
	enqueueCommand(commandQueue, "KEYRELEASE")

	return nil
}

func (runtime *bridgeRuntime) Stop() {
	runtime.mu.Lock()
	cancel := runtime.cancel
	running := runtime.running
	runtime.mu.Unlock()

	if !running || cancel == nil {
		return
	}

	emitBridgeEvent(runtime.reporter, bridgeEventStopping, "", "stopping bridge")
	cancel()
}

func (runtime *bridgeRuntime) Wait() {
	runtime.mu.Lock()
	done := runtime.done
	runtime.mu.Unlock()

	if done != nil {
		<-done
	}
}
