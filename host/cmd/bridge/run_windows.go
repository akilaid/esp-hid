//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"

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
