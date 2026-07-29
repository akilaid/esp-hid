// esp-hid-bridge: forwards this PC's mouse/keyboard to an ESP32-C3 bridge
// that replays them as BLE HID on a paired phone/tablet.
package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"esp-hid/host/internal/config"
)

// version is stamped by the build via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	setupFileLog()

	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		reportFatal(err)
		os.Exit(2)
	}

	log.Printf("esp-hid-bridge %s", version)
	if err := run(cfg); err != nil {
		log.Print(err)
		reportFatal(err)
		os.Exit(1)
	}
}

// setupFileLog mirrors logs into %AppData%\ESP HID Bridge\bridge.log — the
// GUI build has no console (windowsgui subsystem), so without this a startup
// failure would be undiagnosable.
func setupFileLog() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "ESP HID Bridge", "bridge.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.Create(path)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, file))
}
