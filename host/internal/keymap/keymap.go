// Package keymap translates Windows virtual-key codes to USB HID
// Keyboard/Keypad page usages — one direct table, replacing the legacy
// VK -> BLE-keycode -> usage double mapping. US-layout assumption on the
// OEM punctuation keys, as before.
package keymap

// Windows virtual-key constants used by the table.
const (
	vkBack      = 0x08
	vkTab       = 0x09
	vkReturn    = 0x0D
	vkShift     = 0x10
	vkControl   = 0x11
	vkMenu      = 0x12
	vkPause     = 0x13
	vkCapital   = 0x14
	vkEscape    = 0x1B
	vkSpace     = 0x20
	vkPrior     = 0x21 // Page Up
	vkNext      = 0x22 // Page Down
	vkEnd       = 0x23
	vkHome      = 0x24
	vkLeft      = 0x25
	vkUp        = 0x26
	vkRight     = 0x27
	vkDown      = 0x28
	vkSnapshot  = 0x2C // Print Screen
	vkInsert    = 0x2D
	vkDelete    = 0x2E
	vk0         = 0x30
	vk9         = 0x39
	vkA         = 0x41
	vkZ         = 0x5A
	vkLWin      = 0x5B
	vkRWin      = 0x5C
	vkApps      = 0x5D
	vkNumpad0   = 0x60
	vkNumpad9   = 0x69
	vkMultiply  = 0x6A
	vkAdd       = 0x6B
	vkSubtract  = 0x6D
	vkDecimal   = 0x6E
	vkDivide    = 0x6F
	vkF1        = 0x70
	vkF12       = 0x7B
	vkF13       = 0x7C
	vkF24       = 0x87
	vkNumLock   = 0x90
	vkScroll    = 0x91
	vkLShift    = 0xA0
	vkRShift    = 0xA1
	vkLControl  = 0xA2
	vkRControl  = 0xA3
	vkLMenu     = 0xA4
	vkRMenu     = 0xA5
	vkOem1      = 0xBA // ;:
	vkOemPlus   = 0xBB // =+
	vkOemComma  = 0xBC // ,<
	vkOemMinus  = 0xBD // -_
	vkOemPeriod = 0xBE // .>
	vkOem2      = 0xBF // /?
	vkOem3      = 0xC0 // `~
	vkOem4      = 0xDB // [{
	vkOem5      = 0xDC // \|
	vkOem6      = 0xDD // ]}
	vkOem7      = 0xDE // '"
	vkOem102    = 0xE2 // <> on non-US, extra backslash
)

// HID modifier usages (0xE0..0xE7 map to modifier bits on the device).
const (
	UsageLeftCtrl   = 0xE0
	UsageLeftShift  = 0xE1
	UsageLeftAlt    = 0xE2
	UsageLeftGUI    = 0xE3
	UsageRightCtrl  = 0xE4
	UsageRightShift = 0xE5
	UsageRightAlt   = 0xE6
	UsageRightGUI   = 0xE7
)

var explicit = map[uint32]byte{
	vkReturn:   0x28,
	vkEscape:   0x29,
	vkBack:     0x2A,
	vkTab:      0x2B,
	vkSpace:    0x2C,
	vkCapital:  0x39,
	vkSnapshot: 0x46,
	vkScroll:   0x47,
	vkPause:    0x48,
	vkInsert:   0x49,
	vkHome:     0x4A,
	vkPrior:    0x4B,
	vkDelete:   0x4C,
	vkEnd:      0x4D,
	vkNext:     0x4E,
	vkRight:    0x4F,
	vkLeft:     0x50,
	vkDown:     0x51,
	vkUp:       0x52,
	vkNumLock:  0x53,
	vkDivide:   0x54,
	vkMultiply: 0x55,
	vkSubtract: 0x56,
	vkAdd:      0x57,
	vkDecimal:  0x63,
	vkApps:     0x65,

	// Modifiers. The low-level hook delivers side-specific VKs; the generic
	// ones fold to the left variant for safety.
	vkLControl: UsageLeftCtrl,
	vkLShift:   UsageLeftShift,
	vkLMenu:    UsageLeftAlt,
	vkLWin:     UsageLeftGUI,
	vkRControl: UsageRightCtrl,
	vkRShift:   UsageRightShift,
	vkRMenu:    UsageRightAlt,
	vkRWin:     UsageRightGUI,
	vkControl:  UsageLeftCtrl,
	vkShift:    UsageLeftShift,
	vkMenu:     UsageLeftAlt,

	// OEM punctuation, US layout.
	vkOemMinus:  0x2D,
	vkOemPlus:   0x2E,
	vkOem4:      0x2F,
	vkOem6:      0x30,
	vkOem5:      0x31,
	vkOem1:      0x33,
	vkOem7:      0x34,
	vkOem3:      0x35,
	vkOemComma:  0x36,
	vkOemPeriod: 0x37,
	vkOem2:      0x38,
	vkOem102:    0x64,
}

// VKToUsage maps a Windows virtual-key code to a HID usage.
// ok is false for keys with no mapping (they should not be forwarded).
func VKToUsage(vk uint32) (usage byte, ok bool) {
	switch {
	case vk >= vkA && vk <= vkZ:
		return byte(0x04 + vk - vkA), true
	case vk >= 0x31 && vk <= vk9: // 1..9
		return byte(0x1E + vk - 0x31), true
	case vk == vk0:
		return 0x27, true
	case vk >= vkF1 && vk <= vkF12:
		return byte(0x3A + vk - vkF1), true
	case vk >= vkF13 && vk <= vkF24:
		return byte(0x68 + vk - vkF13), true
	case vk == vkNumpad0:
		return 0x62, true
	case vk >= 0x61 && vk <= vkNumpad9: // Numpad 1..9
		return byte(0x59 + vk - 0x61), true
	}
	usage, ok = explicit[vk]
	return usage, ok
}

// IsModifierUsage reports whether a usage is one of the 0xE0..0xE7 modifiers.
func IsModifierUsage(usage byte) bool {
	return usage >= UsageLeftCtrl && usage <= UsageRightGUI
}
