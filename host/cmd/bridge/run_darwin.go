//go:build darwin

package main

import (
	"fmt"
	"os"

	"esp-hid/host/internal/config"
	"esp-hid/host/internal/ui"
)

func run(cfg config.Config) error {
	if cfg.CLIMode {
		return runCLI(cfg)
	}
	if cfg.GUIMode {
		return ui.Run(cfg)
	}
	return runHeadlessBridge(cfg)
}

func reportFatal(err error) {
	fmt.Fprintln(os.Stderr, err)
}
