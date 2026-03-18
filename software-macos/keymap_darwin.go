//go:build darwin

package main

// macOS CGKeyCode constants (hardware-independent virtual key codes).
// These differ significantly from Windows VK codes.
const (
	// Letters (ANSI layout)
	vkA = 0x00
	vkB = 0x0B
	vkC = 0x08
	vkD = 0x02
	vkE = 0x0E
	vkF = 0x03
	vkG = 0x05
	vkH = 0x04
	vkI = 0x22
	vkJ = 0x26
	vkK = 0x28
	vkL = 0x25
	vkM = 0x2E
	vkN = 0x2D
	vkO = 0x1F
	vkP = 0x23
	vkQ = 0x0C
	vkR = 0x0F
	vkS = 0x01
	vkT = 0x11
	vkU = 0x20
	vkV = 0x09
	vkW = 0x0D
	vkX = 0x07
	vkY = 0x10
	vkZ = 0x06

	// Digits
	vk0 = 0x1D
	vk1 = 0x12
	vk2 = 0x13
	vk3 = 0x14
	vk4 = 0x15
	vk5 = 0x17
	vk6 = 0x16
	vk7 = 0x1A
	vk8 = 0x1C
	vk9 = 0x19

	// Function keys (non-contiguous on macOS)
	vkF1  = 0x7A
	vkF2  = 0x78
	vkF3  = 0x63
	vkF4  = 0x76
	vkF5  = 0x60
	vkF6  = 0x61
	vkF7  = 0x62
	vkF8  = 0x64
	vkF9  = 0x65
	vkF10 = 0x6D
	vkF11 = 0x67
	vkF12 = 0x6F
	vkF13 = 0x69
	vkF14 = 0x6B
	vkF15 = 0x71
	vkF16 = 0x6A
	vkF17 = 0x40
	vkF18 = 0x4F
	vkF19 = 0x50
	vkF20 = 0x5A

	// Modifiers
	vkShift    = 0x38
	vkRShift   = 0x3C
	vkLShift   = 0x38
	vkControl  = 0x3B
	vkLControl = 0x3B
	vkRControl = 0x3E
	vkMenu     = 0x3A // Option/Alt
	vkLMenu    = 0x3A
	vkRMenu    = 0x3D
	vkLWin     = 0x37 // Command (left)
	vkRWin     = 0x36 // Command (right)

	// Navigation
	vkReturn   = 0x24
	vkTab      = 0x30
	vkSpace    = 0x31
	vkBack     = 0x33 // Delete/Backspace
	vkEscape   = 0x35
	vkCapital  = 0x39 // Caps Lock
	vkDelete   = 0x75 // Forward Delete
	vkHome     = 0x73
	vkEnd      = 0x77
	vkPrior    = 0x74 // Page Up
	vkNext     = 0x79 // Page Down
	vkLeft     = 0x7B
	vkRight    = 0x7C
	vkDown     = 0x7D
	vkUp       = 0x7E
	vkSnapshot = 0x69 // F13 used as Print Screen

	// Numpad
	vkNumpad0  = 0x52
	vkNumpad1  = 0x53
	vkNumpad2  = 0x54
	vkNumpad3  = 0x55
	vkNumpad4  = 0x56
	vkNumpad5  = 0x57
	vkNumpad6  = 0x58
	vkNumpad7  = 0x59
	vkNumpad8  = 0x5B
	vkNumpad9  = 0x5C
	vkMultiply = 0x43
	vkAdd      = 0x45
	vkSubtract = 0x4E
	vkDecimal  = 0x41
	vkDivide   = 0x4B

	// OEM / punctuation
	vkOEM1      = 0x29 // ;
	vkOEMPlus   = 0x18 // =
	vkOEMComma  = 0x2B // ,
	vkOEMMinus  = 0x1B // -
	vkOEMPeriod = 0x2F // .
	vkOEM2      = 0x2C // /
	vkOEM3      = 0x32 // `
	vkOEM4      = 0x21 // [
	vkOEM5      = 0x2A // \
	vkOEM6      = 0x1E // ]
	vkOEM7      = 0x27 // '
	vkOEM102    = 0x0A // ISO extra key
)

// BLE key codes (same as Windows version — these are sent to the ESP32)
const (
	bleKeyLeftCtrl   = 0x80
	bleKeyLeftShift  = 0x81
	bleKeyLeftAlt    = 0x82
	bleKeyLeftGUI    = 0x83
	bleKeyRightCtrl  = 0x84
	bleKeyRightShift = 0x85
	bleKeyRightAlt   = 0x86
	bleKeyRightGUI   = 0x87

	bleKeyReturn    = 0xB0
	bleKeyEsc       = 0xB1
	bleKeyBackspace = 0xB2
	bleKeyTab       = 0xB3

	bleKeyCapsLock = 0xC1
	bleKeyF1       = 0xC2
	bleKeyF13      = 0xF0

	bleKeyPrtSc    = 0xCE
	bleKeyInsert   = 0xD1
	bleKeyHome     = 0xD2
	bleKeyPageUp   = 0xD3
	bleKeyDelete   = 0xD4
	bleKeyEnd      = 0xD5
	bleKeyPageDown = 0xD6
	bleKeyRight    = 0xD7
	bleKeyLeft     = 0xD8
	bleKeyDown     = 0xD9
	bleKeyUp       = 0xDA

	bleKeyNum0        = 0xEA
	bleKeyNum1        = 0xE1
	bleKeyNum2        = 0xE2
	bleKeyNum3        = 0xE3
	bleKeyNum4        = 0xE4
	bleKeyNum5        = 0xE5
	bleKeyNum6        = 0xE6
	bleKeyNum7        = 0xE7
	bleKeyNum8        = 0xE8
	bleKeyNum9        = 0xE9
	bleKeyNumSlash    = 0xDC
	bleKeyNumAsterisk = 0xDD
	bleKeyNumMinus    = 0xDE
	bleKeyNumPlus     = 0xDF
	bleKeyNumEnter    = 0xE0
	bleKeyNumPeriod   = 0xEB
)

// macOS CGKeyCode → BLE key code map for non-letter/digit keys.
// Letters (A-Z) and digits (0-9) are handled via lookup tables below.
var cgKeyToBle = map[uint32]uint8{
	vkReturn:  bleKeyReturn,
	vkBack:    bleKeyBackspace,
	vkTab:     bleKeyTab,
	vkEscape:  bleKeyEsc,
	vkCapital: bleKeyCapsLock,
	vkSpace:   ' ',

	// Navigation
	vkDelete: bleKeyDelete,
	vkHome:   bleKeyHome,
	vkEnd:    bleKeyEnd,
	vkPrior:  bleKeyPageUp,
	vkNext:   bleKeyPageDown,
	vkLeft:   bleKeyLeft,
	vkRight:  bleKeyRight,
	vkUp:     bleKeyUp,
	vkDown:   bleKeyDown,

	// Modifiers
	vkLControl: bleKeyLeftCtrl,
	vkRControl: bleKeyRightCtrl,
	vkLShift:   bleKeyLeftShift,
	vkRShift:   bleKeyRightShift,
	vkLMenu:    bleKeyLeftAlt,
	vkRMenu:    bleKeyRightAlt,
	vkLWin:     bleKeyLeftGUI,
	vkRWin:     bleKeyRightGUI,

	// Numpad
	vkNumpad0:  bleKeyNum0,
	vkNumpad1:  bleKeyNum1,
	vkNumpad2:  bleKeyNum2,
	vkNumpad3:  bleKeyNum3,
	vkNumpad4:  bleKeyNum4,
	vkNumpad5:  bleKeyNum5,
	vkNumpad6:  bleKeyNum6,
	vkNumpad7:  bleKeyNum7,
	vkNumpad8:  bleKeyNum8,
	vkNumpad9:  bleKeyNum9,
	vkDivide:   bleKeyNumSlash,
	vkMultiply: bleKeyNumAsterisk,
	vkSubtract: bleKeyNumMinus,
	vkAdd:      bleKeyNumPlus,
	vkDecimal:  bleKeyNumPeriod,

	// Function keys
	vkF1:  bleKeyF1,
	vkF2:  bleKeyF1 + 1,
	vkF3:  bleKeyF1 + 2,
	vkF4:  bleKeyF1 + 3,
	vkF5:  bleKeyF1 + 4,
	vkF6:  bleKeyF1 + 5,
	vkF7:  bleKeyF1 + 6,
	vkF8:  bleKeyF1 + 7,
	vkF9:  bleKeyF1 + 8,
	vkF10: bleKeyF1 + 9,
	vkF11: bleKeyF1 + 10,
	vkF12: bleKeyF1 + 11,
	vkF13: bleKeyF13,
	vkF14: bleKeyF13 + 1,
	vkF15: bleKeyF13 + 2,
	vkF16: bleKeyF13 + 3,
	vkF17: bleKeyF13 + 4,
	vkF18: bleKeyF13 + 5,
	vkF19: bleKeyF13 + 6,
	vkF20: bleKeyF13 + 7,

	// Punctuation
	vkOEMMinus:  '-',
	vkOEMPlus:   '=',
	vkOEM4:      '[',
	vkOEM6:      ']',
	vkOEM5:      '\\',
	vkOEM1:      ';',
	vkOEM7:      '\'',
	vkOEM3:      '`',
	vkOEMComma:  ',',
	vkOEMPeriod: '.',
	vkOEM2:      '/',
	vkOEM102:    '\\',
}

// Letter key code table: index = (CGKeyCode) → letter 'a'+'offset'
// Since macOS letter key codes are not contiguous, use a lookup map.
var cgLetterKeys = map[uint32]uint8{
	vkA: 'a', vkB: 'b', vkC: 'c', vkD: 'd', vkE: 'e',
	vkF: 'f', vkG: 'g', vkH: 'h', vkI: 'i', vkJ: 'j',
	vkK: 'k', vkL: 'l', vkM: 'm', vkN: 'n', vkO: 'o',
	vkP: 'p', vkQ: 'q', vkR: 'r', vkS: 's', vkT: 't',
	vkU: 'u', vkV: 'v', vkW: 'w', vkX: 'x', vkY: 'y',
	vkZ: 'z',
}

// Digit key code table
var cgDigitKeys = map[uint32]uint8{
	vk0: '0', vk1: '1', vk2: '2', vk3: '3', vk4: '4',
	vk5: '5', vk6: '6', vk7: '7', vk8: '8', vk9: '9',
}

// vkToBleKeyCode maps a macOS CGKeyCode to a BLE key code.
// This is the macOS equivalent of the Windows vkToBleKeyCode function.
func vkToBleKeyCode(vkCode uint32) (uint8, bool) {
	if ch, ok := cgLetterKeys[vkCode]; ok {
		return ch, true
	}
	if ch, ok := cgDigitKeys[vkCode]; ok {
		return ch, true
	}
	if bleCode, ok := cgKeyToBle[vkCode]; ok {
		return bleCode, true
	}
	return 0, false
}
