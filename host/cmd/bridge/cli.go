package main

import (
	"log"
	"os"
	"os/signal"

	"esp-hid/host/internal/config"
	"esp-hid/host/internal/device"
	"esp-hid/host/internal/protocol"
)

// runCLI connects to the device and prints link/BLE events — the headless
// diagnostics mode, usable on any OS.
func runCLI(cfg config.Config) error {
	events := make(chan device.Event, 64)
	link := device.New(events, cfg.PortOverride)
	go link.Run()
	defer link.Close()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	log.Print("cli mode: watching device (Ctrl-C to quit)")
	for {
		select {
		case <-interrupt:
			return nil
		case event := <-events:
			logEvent(event)
		}
	}
}

func logEvent(event device.Event) {
	switch event.Kind {
	case device.EventConnected:
		log.Printf("serial connected on %s", event.Port)
	case device.EventDisconnected:
		log.Printf("serial down: %s", event.Detail)
	case device.EventHello:
		hello := event.Hello
		log.Printf("device hello: proto v%d, firmware %d.%d.%d, caps 0x%04X",
			hello.ProtoVersion, hello.FwMajor, hello.FwMinor, hello.FwPatch, hello.Caps)
	case device.EventBleState:
		state := event.BleState
		log.Printf("ble: %s (reason 0x%02X, %d bonds)",
			bleStateName(state.State), state.Reason, state.BondCount)
	case device.EventDeviceError:
		log.Printf("device error: %s (detail 0x%02X)",
			deviceErrorName(event.ErrCode), event.ErrData)
	case device.EventLog:
		log.Printf("device log: %s", event.Detail)
	}
}

func bleStateName(state byte) string {
	switch state {
	case protocol.BleIdle:
		return "idle"
	case protocol.BleAdvertising:
		return "advertising (waiting for phone)"
	case protocol.BleConnected:
		return "connected"
	default:
		return "unknown"
	}
}

func deviceErrorName(code byte) string {
	switch code {
	case protocol.ErrBadCRC:
		return "bad crc"
	case protocol.ErrUnknownType:
		return "unknown command"
	case protocol.ErrBadLen:
		return "bad length"
	case protocol.ErrHidSendFail:
		return "hid send failed"
	case protocol.ErrNotConnectedDrop:
		return "input dropped (no BLE host)"
	default:
		return "unknown error"
	}
}
