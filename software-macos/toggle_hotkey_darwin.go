//go:build darwin

package main

import (
	"fmt"
	"strings"
)

const defaultToggleHotkeyName = "F9"

// Modifier bitmask constants — same values as Windows for config compatibility.
const (
	hotkeyModCtrl  = uint32(1 << 0)
	hotkeyModAlt   = uint32(1 << 1) // Option key on macOS
	hotkeyModShift = uint32(1 << 2)
	hotkeyModWin   = uint32(1 << 3) // Command key on macOS
)

// modVKs maps macOS modifier CGKeyCodes to their bitmask.
var modVKs = []struct {
	vk  uint32
	bit uint32
}{
	{vkLControl, hotkeyModCtrl},
	{vkRControl, hotkeyModCtrl},
	{vkControl, hotkeyModCtrl},
	{vkLMenu, hotkeyModAlt},
	{vkRMenu, hotkeyModAlt},
	{vkMenu, hotkeyModAlt},
	{vkLShift, hotkeyModShift},
	{vkRShift, hotkeyModShift},
	{vkShift, hotkeyModShift},
	{vkLWin, hotkeyModWin},
	{vkRWin, hotkeyModWin},
}

// IsModifierVK returns true if the CGKeyCode is a pure modifier key.
func IsModifierVK(vk uint32) bool {
	for _, m := range modVKs {
		if m.vk == vk {
			return true
		}
	}
	return false
}

// ModBitForVK returns the modifier bitmask for a modifier CGKeyCode, or 0.
func ModBitForVK(vk uint32) uint32 {
	for _, m := range modVKs {
		if m.vk == vk {
			return m.bit
		}
	}
	return 0
}

// vkToHotkeyName converts a macOS CGKeyCode to a human-readable hotkey name.
func vkToHotkeyName(vk uint32) (string, bool) {
	switch vk {
	case vkF1:
		return "F1", true
	case vkF2:
		return "F2", true
	case vkF3:
		return "F3", true
	case vkF4:
		return "F4", true
	case vkF5:
		return "F5", true
	case vkF6:
		return "F6", true
	case vkF7:
		return "F7", true
	case vkF8:
		return "F8", true
	case vkF9:
		return "F9", true
	case vkF10:
		return "F10", true
	case vkF11:
		return "F11", true
	case vkF12:
		return "F12", true
	case vkDelete:
		return "Delete", true
	case vkHome:
		return "Home", true
	case vkEnd:
		return "End", true
	case vkPrior:
		return "Page Up", true
	case vkNext:
		return "Page Down", true
	case vkNumpad0:
		return "Num 0", true
	case vkNumpad1:
		return "Num 1", true
	case vkNumpad2:
		return "Num 2", true
	case vkNumpad3:
		return "Num 3", true
	case vkNumpad4:
		return "Num 4", true
	case vkNumpad5:
		return "Num 5", true
	case vkNumpad6:
		return "Num 6", true
	case vkNumpad7:
		return "Num 7", true
	case vkNumpad8:
		return "Num 8", true
	case vkNumpad9:
		return "Num 9", true
	case vkDivide:
		return "Num /", true
	case vkMultiply:
		return "Num *", true
	case vkSubtract:
		return "Num -", true
	case vkAdd:
		return "Num +", true
	// Block pure modifiers and Escape
	// Note: on macOS vkShift==vkLShift, vkControl==vkLControl, vkMenu==vkLMenu
	case vkShift, vkRShift,
		vkControl, vkRControl,
		vkMenu, vkRMenu,
		vkLWin, vkRWin, vkEscape:
		return "", false
	}

	// Letters A-Z
	for cgCode, ch := range cgLetterKeys {
		if vk == cgCode {
			return fmt.Sprintf("%c", ch-32), true // uppercase
		}
	}

	// Digits 0-9
	for cgCode, ch := range cgDigitKeys {
		if vk == cgCode {
			return fmt.Sprintf("%c", ch), true
		}
	}

	return "", false
}

// modsToPrefix builds the "Ctrl+Alt+Shift+" prefix for a modifier bitmask.
func modsToPrefix(mods uint32) string {
	var parts []string
	if mods&hotkeyModCtrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&hotkeyModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&hotkeyModShift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&hotkeyModWin != 0 {
		parts = append(parts, "Cmd")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "+") + "+"
}

// FormatHotkeyCombo formats a CGKeyCode + mods into a display string like "Alt+F7".
func FormatHotkeyCombo(vk, mods uint32) string {
	keyName, ok := vkToHotkeyName(vk)
	if !ok {
		keyName = defaultToggleHotkeyName
	}
	return modsToPrefix(mods) + keyName
}

// normalizeToggleHotkeyName validates and normalises a hotkey combo string.
func normalizeToggleHotkeyName(name string) (string, bool) {
	vk, mods := parseCombo(name)
	if vk == 0 {
		return "", false
	}
	return FormatHotkeyCombo(vk, mods), true
}

// toggleHotkeyNameToVK returns just the VK for a hotkey name.
func toggleHotkeyNameToVK(name string) (uint32, bool) {
	vk, _ := parseCombo(name)
	return vk, vk != 0
}

// toggleHotkeyNameToVKMods parses a combo name and returns CGKeyCode + modifier mask.
func toggleHotkeyNameToVKMods(name string) (vk, mods uint32) {
	return parseCombo(name)
}

// parseCombo parses strings like "Alt+F7", "Ctrl+Shift+X", or plain "F9".
func parseCombo(name string) (vk, mods uint32) {
	parts := strings.Split(strings.TrimSpace(name), "+")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if i == len(parts)-1 {
			vk = nameToVK(part)
			return vk, mods
		}
		switch strings.ToUpper(part) {
		case "CTRL", "CONTROL":
			mods |= hotkeyModCtrl
		case "ALT", "OPTION":
			mods |= hotkeyModAlt
		case "SHIFT":
			mods |= hotkeyModShift
		case "WIN", "CMD", "COMMAND", "SUPER", "META":
			mods |= hotkeyModWin
		default:
			rest := strings.Join(parts[i:], "+")
			vk = nameToVK(rest)
			return vk, mods
		}
	}
	return 0, 0
}

// nameToVK maps a plain key name to a macOS CGKeyCode.
func nameToVK(name string) uint32 {
	upper := strings.ToUpper(strings.TrimSpace(name))
	switch upper {
	case "F1":
		return vkF1
	case "F2":
		return vkF2
	case "F3":
		return vkF3
	case "F4":
		return vkF4
	case "F5":
		return vkF5
	case "F6":
		return vkF6
	case "F7":
		return vkF7
	case "F8":
		return vkF8
	case "F9":
		return vkF9
	case "F10":
		return vkF10
	case "F11":
		return vkF11
	case "F12":
		return vkF12
	case "DELETE":
		return vkDelete
	case "HOME":
		return vkHome
	case "END":
		return vkEnd
	case "PAGE UP", "PAGEUP", "PGUP":
		return vkPrior
	case "PAGE DOWN", "PAGEDOWN", "PGDN":
		return vkNext
	case "NUM 0":
		return vkNumpad0
	case "NUM 1":
		return vkNumpad1
	case "NUM 2":
		return vkNumpad2
	case "NUM 3":
		return vkNumpad3
	case "NUM 4":
		return vkNumpad4
	case "NUM 5":
		return vkNumpad5
	case "NUM 6":
		return vkNumpad6
	case "NUM 7":
		return vkNumpad7
	case "NUM 8":
		return vkNumpad8
	case "NUM 9":
		return vkNumpad9
	case "NUM /":
		return vkDivide
	case "NUM *":
		return vkMultiply
	case "NUM -":
		return vkSubtract
	case "NUM +":
		return vkAdd
	}

	// Single letter A-Z (using CGKeyCode lookup)
	if len(upper) == 1 && upper[0] >= 'A' && upper[0] <= 'Z' {
		want := uint8(upper[0] - 'A' + 'a')
		for cgCode, ch := range cgLetterKeys {
			if ch == want {
				return cgCode
			}
		}
	}

	// Single digit 0-9
	if len(upper) == 1 && upper[0] >= '0' && upper[0] <= '9' {
		want := uint8(upper[0])
		for cgCode, ch := range cgDigitKeys {
			if ch == want {
				return cgCode
			}
		}
	}

	return 0
}
