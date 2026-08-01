package hotkey

// macOS CGKeyCode lookups for the combo grammar.
//
// The Windows virtual-key code is this package's canonical key identity on
// every platform: settings persist the *string* name ("Ctrl+Alt+F7"), Parse
// turns that into a VK, and this file turns the VK into the macOS key code
// the event tap actually compares against. That keeps a settings file
// portable between the Windows and macOS hosts unchanged.
//
// Like keymap_cgkeycode.go this file is deliberately untagged and named
// _cgkeycode rather than _darwin: it is a pure lookup table, so it and its
// tests compile and run on every platform's CI.

// macOS key codes are assigned by physical position, not alphabetically, so
// every block needs an explicit table — none of the arithmetic that works for
// Windows VKs applies here.
var vkToCGKey = map[uint32]uint32{
	// Letters.
	VKA: 0x00, VKA + 1: 0x0B, VKA + 2: 0x08, VKA + 3: 0x02, VKA + 4: 0x0E,
	VKA + 5: 0x03, VKA + 6: 0x05, VKA + 7: 0x04, VKA + 8: 0x22, VKA + 9: 0x26,
	VKA + 10: 0x28, VKA + 11: 0x25, VKA + 12: 0x2E, VKA + 13: 0x2D, VKA + 14: 0x1F,
	VKA + 15: 0x23, VKA + 16: 0x0C, VKA + 17: 0x0F, VKA + 18: 0x01, VKA + 19: 0x11,
	VKA + 20: 0x20, VKA + 21: 0x09, VKA + 22: 0x0D, VKA + 23: 0x07, VKA + 24: 0x10,
	VKA + 25: 0x06,

	// Digits, in 0..9 order.
	VK0: 0x1D, VK0 + 1: 0x12, VK0 + 2: 0x13, VK0 + 3: 0x14, VK0 + 4: 0x15,
	VK0 + 5: 0x17, VK0 + 6: 0x16, VK0 + 7: 0x1A, VK0 + 8: 0x1C, VK0 + 9: 0x19,

	// Function keys F1..F12.
	VKF1: 0x7A, VKF1 + 1: 0x78, VKF1 + 2: 0x63, VKF1 + 3: 0x76,
	VKF1 + 4: 0x60, VKF1 + 5: 0x61, VKF1 + 6: 0x62, VKF1 + 7: 0x64,
	VKF1 + 8: 0x65, VKF1 + 9: 0x6D, VKF1 + 10: 0x67, VKF1 + 11: 0x6F,

	// Keypad digits.
	VKNumpad0: 0x52, VKNumpad0 + 1: 0x53, VKNumpad0 + 2: 0x54, VKNumpad0 + 3: 0x55,
	VKNumpad0 + 4: 0x56, VKNumpad0 + 5: 0x57, VKNumpad0 + 6: 0x58, VKNumpad0 + 7: 0x59,
	VKNumpad0 + 8: 0x5B, VKNumpad0 + 9: 0x5C,

	// Keypad operators.
	VKDivide:   0x4B,
	VKMultiply: 0x43,
	VKSubtract: 0x4E,
	VKAdd:      0x45,

	// Navigation. Apple keyboards label the Insert position "Help", and the
	// PC "Delete" key is macOS's forward delete.
	VKInsert: 0x72,
	VKDelete: 0x75,
	VKHome:   0x73,
	VKEnd:    0x77,
	VKPrior:  0x74,
	VKNext:   0x79,

	// Macs have no Print Screen key; F13 sits in that position on Apple
	// extended keyboards. Aliasing it keeps a settings file written on
	// Windows working after it is copied to a Mac. Parse never produces F13
	// by name, so nothing else can collide with this.
	VKSnapshot: 0x69,
}

// CGKeyCodeFor maps a Windows virtual-key code to the macOS key code for the
// same physical key. ok is false for keys with no macOS equivalent.
func CGKeyCodeFor(vk uint32) (cgKeyCode uint32, ok bool) {
	cgKeyCode, ok = vkToCGKey[vk]
	return cgKeyCode, ok
}

// ParseDarwin parses a combo name straight into the macOS key code and
// modifier mask that the event tap compares against. cgKeyCode is 0 when the
// name is invalid or has no macOS equivalent.
func ParseDarwin(name string) (cgKeyCode, mods uint32) {
	vk, mods := Parse(name)
	if vk == 0 {
		return 0, 0
	}
	cgKeyCode, ok := CGKeyCodeFor(vk)
	if !ok {
		return 0, 0
	}
	return cgKeyCode, mods
}
