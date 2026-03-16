//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

func main() {
	cfg, err := parseConfig()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	if cfg.guiMode {
		if err := runGUI(cfg); err != nil {
			log.Fatalf("gui failed: %v", err)
		}
		return
	}

	runCLIBridge(cfg)
}

func runCLIBridge(cfg config) {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf(
		"starting mouse/keyboard bridge: port=%s baud=%d rate=%dHz deadzone=%d smooth=%.2f adaptive=%t keyboard=%t leftreturn=%t slave=%dx%d hostside=%s",
		cfg.portName,
		cfg.baudRate,
		cfg.moveRateHz,
		cfg.moveDeadzone,
		cfg.moveSmoothing,
		cfg.adaptiveMoves,
		cfg.captureKeyboard,
		cfg.leftwardReturn,
		cfg.slaveWidth,
		cfg.slaveHeight,
		cfg.hostSide,
	)
	startupPortHint(cfg)

	commandQueue := make(chan string, 1024)
	var serialConnected atomic.Bool
	serialReporter := func(event bridgeEvent) {
		updateSerialConnectionState(&serialConnected, event.Type)
	}

	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		writeLoop(ctx, cfg, commandQueue, serialReporter)
	}()

	if err := runCaptureLoop(ctx, cfg, commandQueue, serialConnected.Load, nil); err != nil {
		log.Printf("capture loop stopped: %v", err)
	}

	enqueueCommand(commandQueue, "RELEASE")
	enqueueCommand(commandQueue, "KEYRELEASE")
	close(commandQueue)
	cancel()
	writerWG.Wait()
}
