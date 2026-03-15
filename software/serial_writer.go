//go:build windows

package main

import (
	"context"
	"io"
	"log"
	"time"

	serial "go.bug.st/serial"
)

func sendResetState(port serial.Port) error {
	_, err := io.WriteString(port, "RELEASE\nKEYRELEASE\n")
	return err
}

func writeLoop(ctx context.Context, cfg config, queue <-chan string) {
	var port serial.Port
	activePortName := cfg.portName
	defer func() {
		if port != nil {
			_ = port.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case command, ok := <-queue:
			if !ok {
				return
			}

			for {
				if port == nil {
					targetPort := cfg.portName
					if cfg.autoPort {
						autoPort, autoErr := autoSelectPort()
						if autoErr != nil {
							log.Printf("serial auto-detect failed: %v", autoErr)
							if !sleepWithContext(ctx, cfg.reconnectDelay) {
								return
							}
							continue
						}
						targetPort = autoPort
					}

					openedPort, err := serial.Open(targetPort, &serial.Mode{BaudRate: cfg.baudRate})
					if err != nil {
						available := "unavailable"
						if ports, listErr := listSerialPorts(); listErr == nil {
							available = portsToString(ports)
						}
						log.Printf("serial open failed on %s: %v (available: %s)", targetPort, err, available)
						if !sleepWithContext(ctx, cfg.reconnectDelay) {
							return
						}
						continue
					}

					if err := sendResetState(openedPort); err != nil {
						log.Printf("serial init write failed on %s: %v", targetPort, err)
						_ = openedPort.Close()
						if !sleepWithContext(ctx, cfg.reconnectDelay) {
							return
						}
						continue
					}

					port = openedPort
					activePortName = targetPort
					log.Printf("serial connected on %s at %d baud", activePortName, cfg.baudRate)
				}

				if _, err := io.WriteString(port, command+"\n"); err != nil {
					log.Printf("serial write failed on %s: %v", activePortName, err)
					_ = port.Close()
					port = nil

					if !sleepWithContext(ctx, cfg.reconnectDelay) {
						return
					}
					continue
				}

				break
			}
		}
	}
}

func sleepWithContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
