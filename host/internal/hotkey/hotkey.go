// Package hotkey parses and formats toggle-hotkey combos like "F9" or
// "Ctrl+Alt+F7". Ported from the legacy software/toggle_hotkey.go; the combo
// grammar and canonical formatting are unchanged so persisted settings and
// user habits carry over.
package hotkey

import (
	"fmt"
	"strings"
)

// DefaultName is the default toggle hotkey.
const DefaultName = "F9"

// Modifier bitmask.
const (
	ModCtrl  = uint32(1 << 0)
	ModAlt   = uint32(1 << 1)
	ModShift = uint32(1 << 2)
	ModWin   = uint32(1 << 3)
)

// Windows virtual-key codes used by the combo grammar.
const (
	VKEscape   = 0x1B
	VKPrior    = 0x21
	VKNext     = 0x22
	VKEnd      = 0x23
	VKHome     = 0x24
	VKSnapshot = 0x2C
	VKInsert   = 0x2D
	VKDelete   = 0x2E
	VK0        = 0x30
	VK9        = 0x39
	VKA        = 0x41
	VKZ        = 0x5A
	VKLWin     = 0x5B
	VKRWin     = 0x5C
	VKNumpad0  = 0x60
	VKNumpad9  = 0x69
	VKMultiply = 0x6A
	VKAdd      = 0x6B
	VKSubtract = 0x6D
	VKDivide   = 0x6F
	VKF1       = 0x70
	VKF12      = 0x7B
	VKShift    = 0x10
	VKControl  = 0x11
	VKMenu     = 0x12
	VKLShift   = 0xA0
	VKRShift   = 0xA1
	VKLControl = 0xA2
	VKRControl = 0xA3
	VKLMenu    = 0xA4
	VKRMenu    = 0xA5
)

var modifierVKs = map[uint32]uint32{
	VKLControl: ModCtrl,
	VKRControl: ModCtrl,
	VKControl:  ModCtrl,
	VKLMenu:    ModAlt,
	VKRMenu:    ModAlt,
	VKMenu:     ModAlt,
	VKLShift:   ModShift,
	VKRShift:   ModShift,
	VKShift:    ModShift,
	VKLWin:     ModWin,
	VKRWin:     ModWin,
}

// IsModifierVK reports whether the VK is a pure modifier key.
func IsModifierVK(vk uint32) bool {
	_, ok := modifierVKs[vk]
	return ok
}

// ModBitForVK returns the modifier bit for a modifier VK, or 0.
func ModBitForVK(vk uint32) uint32 {
	return modifierVKs[vk]
}

// Parse parses a combo like "Alt+F7", "Ctrl+Shift+X", or plain "F9".
// vk is 0 when the name is invalid.
func Parse(name string) (vk, mods uint32) {
	parts := strings.Split(strings.TrimSpace(name), "+")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if i == len(parts)-1 {
			return nameToVK(part), mods
		}
		switch strings.ToUpper(part) {
		case "CTRL", "CONTROL":
			mods |= ModCtrl
		case "ALT":
			mods |= ModAlt
		case "SHIFT":
			mods |= ModShift
		case "WIN", "SUPER", "META":
			mods |= ModWin
		default:
			rest := strings.Join(parts[i:], "+")
			return nameToVK(rest), mods
		}
	}
	return 0, 0
}

// Format renders a VK + modifier mask canonically ("Ctrl+Alt+F7").
func Format(vk, mods uint32) string {
	keyName, ok := vkToName(vk)
	if !ok {
		keyName = DefaultName
	}
	return modsPrefix(mods) + keyName
}

// Normalize validates a combo string and returns its canonical form.
func Normalize(name string) (string, bool) {
	vk, mods := Parse(name)
	if vk == 0 {
		return "", false
	}
	return Format(vk, mods), true
}

func modsPrefix(mods uint32) string {
	var parts []string
	if mods&ModCtrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ModWin != 0 {
		parts = append(parts, "Win")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "+") + "+"
}

func vkToName(vk uint32) (string, bool) {
	switch {
	case vk >= VKF1 && vk <= VKF12:
		return fmt.Sprintf("F%d", vk-VKF1+1), true
	case vk >= VKA && vk <= VKZ:
		return string(rune('A' + vk - VKA)), true
	case vk >= VK0 && vk <= VK9:
		return string(rune('0' + vk - VK0)), true
	case vk >= VKNumpad0 && vk <= VKNumpad9:
		return fmt.Sprintf("Num %d", vk-VKNumpad0), true
	}
	switch vk {
	case VKInsert:
		return "Insert", true
	case VKDelete:
		return "Delete", true
	case VKHome:
		return "Home", true
	case VKEnd:
		return "End", true
	case VKPrior:
		return "Page Up", true
	case VKNext:
		return "Page Down", true
	case VKSnapshot:
		return "Print Screen", true
	case VKDivide:
		return "Num /", true
	case VKMultiply:
		return "Num *", true
	case VKSubtract:
		return "Num -", true
	case VKAdd:
		return "Num +", true
	}
	return "", false
}

func nameToVK(name string) uint32 {
	upper := strings.ToUpper(strings.TrimSpace(name))
	switch upper {
	case "INSERT":
		return VKInsert
	case "DELETE":
		return VKDelete
	case "HOME":
		return VKHome
	case "END":
		return VKEnd
	case "PAGE UP", "PAGEUP", "PGUP":
		return VKPrior
	case "PAGE DOWN", "PAGEDOWN", "PGDN":
		return VKNext
	case "PRINT SCREEN", "PRINTSCREEN":
		return VKSnapshot
	case "NUM /":
		return VKDivide
	case "NUM *":
		return VKMultiply
	case "NUM -":
		return VKSubtract
	case "NUM +":
		return VKAdd
	}
	if strings.HasPrefix(upper, "NUM ") && len(upper) == 5 {
		if digit := upper[4]; digit >= '0' && digit <= '9' {
			return VKNumpad0 + uint32(digit-'0')
		}
	}
	if strings.HasPrefix(upper, "F") && len(upper) >= 2 && len(upper) <= 3 {
		var n int
		if _, err := fmt.Sscanf(upper, "F%d", &n); err == nil && n >= 1 && n <= 12 {
			return VKF1 + uint32(n-1)
		}
	}
	if len(upper) == 1 && upper[0] >= 'A' && upper[0] <= 'Z' {
		return VKA + uint32(upper[0]-'A')
	}
	if len(upper) == 1 && upper[0] >= '0' && upper[0] <= '9' {
		return VK0 + uint32(upper[0]-'0')
	}
	return 0
}
