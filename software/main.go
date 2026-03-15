//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func main() {
	cfg, err := parseConfig()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("starting mouse/keyboard bridge: port=%s baud=%d rate=%dHz keyboard=%t", cfg.portName, cfg.baudRate, cfg.moveRateHz, cfg.captureKeyboard)
	startupPortHint(cfg)

	commandQueue := make(chan string, 1024)

	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		writeLoop(ctx, cfg, commandQueue)
	}()

	if err := runCaptureLoop(ctx, cfg, commandQueue); err != nil {
		log.Printf("capture loop stopped: %v", err)
	}

	enqueueCommand(commandQueue, "RELEASE")
	enqueueCommand(commandQueue, "KEYRELEASE")
	close(commandQueue)
	cancel()
	writerWG.Wait()
}
