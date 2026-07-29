// esp-hid-bridge: forwards this PC's mouse/keyboard to an ESP32-C3 bridge
// that replays them as BLE HID on a paired phone/tablet.
package main

import (
	"fmt"
	"log"
	"os"

	"esp-hid/host/internal/config"
)

// version is stamped by the build via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	log.Printf("esp-hid-bridge %s", version)
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}
