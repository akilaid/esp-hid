package main

import "testing"

// The port set this repo is developed against: an ESP32-C3 exposes a native
// USB Serial/JTAG peripheral (usbmodem*), alongside the ports every Mac has.
var macPortsWithC3 = []string{
	"/dev/cu.Bluetooth-Incoming-Port",
	"/dev/cu.debug-console",
	"/dev/cu.soundcoreR50iNC",
	"/dev/cu.usbmodem2301",
	"/dev/tty.Bluetooth-Incoming-Port",
	"/dev/tty.debug-console",
	"/dev/tty.soundcoreR50iNC",
	"/dev/tty.usbmodem2301",
}

func TestMacOSPortPriorityRejectsNonUSBPorts(t *testing.T) {
	for _, port := range []string{
		"/dev/cu.Bluetooth-Incoming-Port",
		"/dev/tty.Bluetooth-Incoming-Port",
		"/dev/cu.debug-console",
		"/dev/cu.soundcoreR50iNC",
		"COM3",
		"",
	} {
		if got := macOSPortPriority(port); got >= 0 {
			t.Errorf("macOSPortPriority(%q) = %d, want negative", port, got)
		}
	}
}

func TestMacOSPortPriorityPrefersCalloutOverTTY(t *testing.T) {
	for _, name := range []string{"usbmodem2301", "usbserial-0001"} {
		callout := macOSPortPriority("/dev/cu." + name)
		tty := macOSPortPriority("/dev/tty." + name)
		if callout <= tty {
			t.Errorf("%s: cu=%d tty=%d, want cu > tty", name, callout, tty)
		}
	}
}

// Regression: native USB (usbmodem, ESP32-C3/S3) used to rank below CP210x and
// FTDI bridges, so an unrelated adapter could outrank the actual board.
func TestMacOSPortPriorityRanksNativeUSBWithBridges(t *testing.T) {
	native := macOSPortPriority("/dev/cu.usbmodem2301")
	for _, bridge := range []string{
		"/dev/cu.usbserial-0001",
		"/dev/cu.SLAB_USBtoUART",
		"/dev/cu.wchusbserial14330",
	} {
		if got := macOSPortPriority(bridge); got != native {
			t.Errorf("macOSPortPriority(%q) = %d, want %d (same as native USB)", bridge, got, native)
		}
	}
}

func TestSortPortsByPrioritySelectsC3CalloutDevice(t *testing.T) {
	ports := append([]string(nil), macPortsWithC3...)
	sortPortsByPriority(ports)

	if want := "/dev/cu.usbmodem2301"; ports[0] != want {
		t.Fatalf("best candidate = %q, want %q (full order: %v)", ports[0], want, ports)
	}
}

// When a native-USB board and an unrelated bridge are both plugged in, nothing
// in the port name says which is the ESP32. Auto-detect must not pretend to
// know: it reports the tie so the operator can pass -port.
func TestEqualPriorityPortsReportsAmbiguity(t *testing.T) {
	ports := append([]string(nil), macPortsWithC3...)
	ports = append(ports, "/dev/cu.SLAB_USBtoUART")
	sortPortsByPriority(ports)

	tied := equalPriorityPorts(ports, macOSPortPriority(ports[0]))
	if len(tied) != 2 {
		t.Fatalf("tied candidates = %v, want both USB adapters", tied)
	}

	got := map[string]bool{tied[0]: true, tied[1]: true}
	for _, want := range []string{"/dev/cu.usbmodem2301", "/dev/cu.SLAB_USBtoUART"} {
		if !got[want] {
			t.Errorf("tied candidates %v missing %q", tied, want)
		}
	}
}

// The tty.* twin scores strictly lower, so a single board must never look
// ambiguous just because macOS exposes it under both names.
func TestEqualPriorityPortsIgnoresTTYTwin(t *testing.T) {
	ports := append([]string(nil), macPortsWithC3...)
	sortPortsByPriority(ports)

	if tied := equalPriorityPorts(ports, macOSPortPriority(ports[0])); len(tied) != 1 {
		t.Fatalf("tied candidates = %v, want exactly the cu.* device", tied)
	}
}
