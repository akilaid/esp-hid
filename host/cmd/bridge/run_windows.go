//go:build windows

package main

import (
	"log"
	"os"
	"os/signal"
	"unsafe"

	"golang.org/x/sys/windows"

	"esp-hid/host/internal/bridge"
	"esp-hid/host/internal/config"
	"esp-hid/host/internal/ui"
)

// reportFatal shows a message box: the GUI build has no console, so this is
// the only way a startup error reaches the user.
func reportFatal(err error) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	text, _ := windows.UTF16PtrFromString(
		"ESP HID Bridge failed to start:\n\n" + err.Error() +
			"\n\nDetails: %AppData%\\ESP HID Bridge\\bridge.log")
	caption, _ := windows.UTF16PtrFromString("ESP HID Bridge")
	const mbIconError = 0x10
	messageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), mbIconError)
}

func run(cfg config.Config) error {
	if cfg.CLIMode {
		return runCLI(cfg)
	}
	if cfg.GUIMode {
		return ui.Run(cfg)
	}
	return runHeadlessBridge(cfg)
}

// runHeadlessBridge runs the full capture+forwarding pipeline without a GUI
// (-gui=false): logs to the console, stops on Ctrl-C.
func runHeadlessBridge(cfg config.Config) error {
	events := make(chan bridge.Event, 256)
	runtime := bridge.New(events)
	if err := runtime.Start(cfg); err != nil {
		return err
	}
	defer runtime.Stop()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	for {
		select {
		case <-interrupt:
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
