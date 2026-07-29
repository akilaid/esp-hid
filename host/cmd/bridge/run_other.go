//go:build !windows

package main

import (
	"log"

	"esp-hid/host/internal/config"
)

// Non-Windows builds support CLI diagnostics only; capture and GUI are
// Windows features (macOS capture is a planned follow-up).
func run(cfg config.Config) error {
	if !cfg.CLIMode {
		log.Print("capture/GUI are Windows-only for now; running CLI mode")
	}
	return runCLI(cfg)
}
