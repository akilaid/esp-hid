//go:build windows || darwin

package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"esp-hid/host/internal/bridge"
	"esp-hid/host/internal/config"
)

// runHeadlessBridge runs the full capture+forwarding pipeline without a GUI
// (-gui=false): logs to the console, stops on Ctrl-C or SIGTERM.
//
// Handling SIGTERM matters more than it looks. On macOS, remote mode leaves
// the pointer hidden and decoupled from the mouse; the capture layer restores
// both as it unwinds, so a termination path that skips that unwind would
// leave the user with a frozen, invisible cursor.
func runHeadlessBridge(cfg config.Config) error {
	events := make(chan bridge.Event, 256)
	runtime := bridge.New(events)
	if err := runtime.Start(cfg); err != nil {
		return err
	}
	defer runtime.Stop()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-interrupt:
			log.Print("interrupted; shutting down")
			return nil
		case event := <-events:
			switch event.Kind {
			case bridge.EventSerialConnected:
				log.Printf("serial connected on %s", event.Port)
			case bridge.EventSerialDown:
				log.Printf("serial down: %s", event.Detail)
			case bridge.EventHello:
				log.Printf("firmware %d.%d.%d", event.Hello.FwMajor, event.Hello.FwMinor, event.Hello.FwPatch)
			case bridge.EventBleState:
				log.Printf("ble state %d (%d bonds)", event.BleState.State, event.BleState.BondCount)
			case bridge.EventRemoteMode:
				log.Printf("remote mode %v (%s)", event.Active, event.Detail)
			case bridge.EventPermissionRequired:
				log.Printf("permission required: %s", event.Detail)
				return nil
			case bridge.EventCaptureError:
				log.Printf("capture error: %s", event.Detail)
				return nil
			case bridge.EventDeviceError:
				log.Printf("device error: %s", event.Detail)
			case bridge.EventLog:
				log.Printf("device: %s", event.Detail)
			}
		}
	}
}
