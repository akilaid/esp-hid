//go:build !windows && !darwin

package main

import (
	"fmt"
	"log"
	"os"

	"esp-hid/host/internal/config"
)

// Input capture is implemented for Windows and macOS. Everywhere else the
// protocol, device, and pipeline packages still build, so the binary is
// useful as a diagnostics tool even though it cannot forward input.
func run(cfg config.Config) error {
	if !cfg.CLIMode {
		log.Print("input capture is available on Windows and macOS only; running CLI mode")
	}
	return runCLI(cfg)
}

func reportFatal(err error) {
	fmt.Fprintln(os.Stderr, err)
}
