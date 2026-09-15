// esp-hid-bridge: forwards this PC's mouse/keyboard to an ESP32-C3 bridge
// that replays them as BLE HID on a paired phone/tablet.
package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"esp-hid/host/internal/config"
	"esp-hid/host/internal/update"
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
	// Whatever the last self-update left behind (the previous bundle on
	// macOS, the renamed exe on Windows) goes now that it has clearly worked.
	update.Cleanup()
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
	// The file goes first. MultiWriter stops at the first writer that fails,
	// and in the Windows GUI build stderr is not a valid handle — with stderr
	// first, every line failed there and the file stayed at zero bytes, which
	// is how three broken Windows releases went by without a single log line.
	log.SetOutput(io.MultiWriter(file, os.Stderr))
}
