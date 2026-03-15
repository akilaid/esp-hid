//go:build windows

package main

const (
	vkBack      = 0x08
	vkTab       = 0x09
	vkReturn    = 0x0D
	vkShift     = 0x10
	vkControl   = 0x11
	vkMenu      = 0x12
	vkCapital   = 0x14
	vkEscape    = 0x1B
	vkSpace     = 0x20
	vkPrior     = 0x21
	vkNext      = 0x22
	vkEnd       = 0x23
	vkHome      = 0x24
	vkLeft      = 0x25
	vkUp        = 0x26
	vkRight     = 0x27
	vkDown      = 0x28
	vkSnapshot  = 0x2C
	vkInsert    = 0x2D
	vkDelete    = 0x2E
	vk0         = 0x30
	vk9         = 0x39
	vkA         = 0x41
	vkZ         = 0x5A
	vkLWin      = 0x5B
	vkRWin      = 0x5C
	vkNumpad0   = 0x60
	vkNumpad1   = 0x61
	vkNumpad2   = 0x62
	vkNumpad3   = 0x63
	vkNumpad4   = 0x64
	vkNumpad5   = 0x65
	vkNumpad6   = 0x66
	vkNumpad7   = 0x67
	vkNumpad8   = 0x68
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
	vkLShift    = 0xA0
	vkRShift    = 0xA1
	vkLControl  = 0xA2
	vkRControl  = 0xA3
	vkLMenu     = 0xA4
	vkRMenu     = 0xA5
	vkOEM1      = 0xBA
	vkOEMPlus   = 0xBB
	vkOEMComma  = 0xBC
	vkOEMMinus  = 0xBD
	vkOEMPeriod = 0xBE
	vkOEM2      = 0xBF
	vkOEM3      = 0xC0
	vkOEM4      = 0xDB
	vkOEM5      = 0xDC
	vkOEM6      = 0xDD
	vkOEM7      = 0xDE
	vkOEM102    = 0xE2
)

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

func vkToBleKeyCode(vkCode uint32) (uint8, bool) {
	switch {
	case vkCode >= vkA && vkCode <= vkZ:
		return uint8('a' + (vkCode - vkA)), true
	case vkCode >= vk0 && vkCode <= vk9:
		return uint8('0' + (vkCode - vk0)), true
	case vkCode >= vkF1 && vkCode <= vkF12:
		return uint8(bleKeyF1 + (vkCode - vkF1)), true
	case vkCode >= vkF13 && vkCode <= vkF24:
		return uint8(bleKeyF13 + (vkCode - vkF13)), true
	}

	switch vkCode {
	case vkSpace:
		return ' ', true
	case vkBack:
		return bleKeyBackspace, true
	case vkTab:
		return bleKeyTab, true
	case vkReturn:
		return bleKeyReturn, true
	case vkEscape:
		return bleKeyEsc, true
	case vkInsert:
		return bleKeyInsert, true
	case vkDelete:
		return bleKeyDelete, true
	case vkHome:
		return bleKeyHome, true
	case vkEnd:
		return bleKeyEnd, true
	case vkPrior:
		return bleKeyPageUp, true
	case vkNext:
		return bleKeyPageDown, true
	case vkLeft:
		return bleKeyLeft, true
	case vkRight:
		return bleKeyRight, true
	case vkUp:
		return bleKeyUp, true
	case vkDown:
		return bleKeyDown, true
	case vkCapital:
		return bleKeyCapsLock, true
	case vkSnapshot:
		return bleKeyPrtSc, true
	case vkLControl:
		return bleKeyLeftCtrl, true
	case vkRControl:
		return bleKeyRightCtrl, true
	case vkControl:
		return bleKeyLeftCtrl, true
	case vkLShift:
		return bleKeyLeftShift, true
	case vkRShift:
		return bleKeyRightShift, true
	case vkShift:
		return bleKeyLeftShift, true
	case vkLMenu:
		return bleKeyLeftAlt, true
	case vkRMenu:
		return bleKeyRightAlt, true
	case vkMenu:
		return bleKeyLeftAlt, true
	case vkLWin:
		return bleKeyLeftGUI, true
	case vkRWin:
		return bleKeyRightGUI, true
	case vkNumpad0:
		return bleKeyNum0, true
	case vkNumpad1:
		return bleKeyNum1, true
	case vkNumpad2:
		return bleKeyNum2, true
	case vkNumpad3:
		return bleKeyNum3, true
	case vkNumpad4:
		return bleKeyNum4, true
	case vkNumpad5:
		return bleKeyNum5, true
	case vkNumpad6:
		return bleKeyNum6, true
	case vkNumpad7:
		return bleKeyNum7, true
	case vkNumpad8:
		return bleKeyNum8, true
	case vkNumpad9:
		return bleKeyNum9, true
	case vkDivide:
		return bleKeyNumSlash, true
	case vkMultiply:
		return bleKeyNumAsterisk, true
	case vkSubtract:
		return bleKeyNumMinus, true
	case vkAdd:
		return bleKeyNumPlus, true
	case vkDecimal:
		return bleKeyNumPeriod, true
	case vkOEMMinus:
		return '-', true
	case vkOEMPlus:
		return '=', true
	case vkOEM4:
		return '[', true
	case vkOEM6:
		return ']', true
	case vkOEM5:
		return '\\', true
	case vkOEM1:
		return ';', true
	case vkOEM7:
		return '\'', true
	case vkOEM3:
		return '`', true
	case vkOEMComma:
		return ',', true
	case vkOEMPeriod:
		return '.', true
	case vkOEM2:
		return '/', true
	case vkOEM102:
		return '\\', true
	}

	return 0, false
}
