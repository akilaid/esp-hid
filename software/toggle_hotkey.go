//go:build windows

package main

import (
	"fmt"
	"strings"
)

const defaultToggleHotkeyName = "F9"

// Modifier bitmask constants.
const (
	hotkeyModCtrl  = uint32(1 << 0)
	hotkeyModAlt   = uint32(1 << 1)
	hotkeyModShift = uint32(1 << 2)
	hotkeyModWin   = uint32(1 << 3)
)

// modVKs lists modifier VK codes and their modifier bit.
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

// IsModifierVK returns whether a VK code is a pure modifier.
func IsModifierVK(vk uint32) bool {
	for _, m := range modVKs {
		if m.vk == vk {
			return true
		}
	}
	return false
}

// ModBitForVK returns the modifier bit for a modifier VK, or 0.
func ModBitForVK(vk uint32) uint32 {
	for _, m := range modVKs {
		if m.vk == vk {
			return m.bit
		}
	}
	return 0
}

// vkToHotkeyName converts a non-modifier Windows VK code to a human-readable name.
func vkToHotkeyName(vk uint32) (string, bool) {
	switch vk {
	// Function keys F1-F12
	case vkF1:
		return "F1", true
	case vkF1 + 1:
		return "F2", true
	case vkF1 + 2:
		return "F3", true
	case vkF1 + 3:
		return "F4", true
	case vkF1 + 4:
		return "F5", true
	case vkF1 + 5:
		return "F6", true
	case vkF1 + 6:
		return "F7", true
	case vkF1 + 7:
		return "F8", true
	case vkF1 + 8:
		return "F9", true
	case vkF1 + 9:
		return "F10", true
	case vkF1 + 10:
		return "F11", true
	case vkF12:
		return "F12", true
	// Navigation / editing keys
	case vkInsert:
		return "Insert", true
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
	case vkSnapshot:
		return "Print Screen", true
	// Numpad
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
	case vkShift, vkLShift, vkRShift,
		vkControl, vkLControl, vkRControl,
		vkMenu, vkLMenu, vkRMenu,
		vkLWin, vkRWin, vkEscape:
		return "", false
	}
	// A-Z
	if vk >= vkA && vk <= vkZ {
		return fmt.Sprintf("%c", 'A'+vk-vkA), true
	}
	// 0-9
	if vk >= vk0 && vk <= vk9 {
		return fmt.Sprintf("%c", '0'+vk-vk0), true
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
		parts = append(parts, "Win")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "+") + "+"
}

// FormatHotkeyCombo formats a VK + mods into a display string like "Alt+F7".
func FormatHotkeyCombo(vk, mods uint32) string {
	keyName, ok := vkToHotkeyName(vk)
	if !ok {
		keyName = defaultToggleHotkeyName
	}
	return modsToPrefix(mods) + keyName
}

// normalizeToggleHotkeyName validates and normalises a hotkey combo string.
// The name may be plain ("F9") or with modifiers ("Alt+F7", "Ctrl+Shift+X").
func normalizeToggleHotkeyName(name string) (string, bool) {
	vk, mods := parseCombo(name)
	if vk == 0 {
		return "", false
	}
	return FormatHotkeyCombo(vk, mods), true
}

// toggleHotkeyNameToVK is kept for legacy call sites — returns just the VK.
func toggleHotkeyNameToVK(name string) (uint32, bool) {
	vk, _ := parseCombo(name)
	return vk, vk != 0
}

// toggleHotkeyNameToVKMods parses a combo name and returns VK + modifier mask.
func toggleHotkeyNameToVKMods(name string) (vk, mods uint32) {
	return parseCombo(name)
}

// parseCombo parses strings like "Alt+F7", "Ctrl+Shift+X", or plain "F9".
func parseCombo(name string) (vk, mods uint32) {
	parts := strings.Split(strings.TrimSpace(name), "+")
	// Walk parts: everything except the last is expected to be a modifier name.
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if i == len(parts)-1 {
			// Last segment is the key name.
			vk = nameToVK(part)
			return vk, mods
		}
		switch strings.ToUpper(part) {
		case "CTRL", "CONTROL":
			mods |= hotkeyModCtrl
		case "ALT":
			mods |= hotkeyModAlt
		case "SHIFT":
			mods |= hotkeyModShift
		case "WIN", "SUPER", "META":
			mods |= hotkeyModWin
		default:
			// Unknown modifier prefix — try treating the whole remaining string as the key.
			rest := strings.Join(parts[i:], "+")
			vk = nameToVK(rest)
			return vk, mods
		}
	}
	return 0, 0
}

// nameToVK maps a plain key name (no modifiers) to a VK code.
func nameToVK(name string) uint32 {
	upper := strings.ToUpper(strings.TrimSpace(name))
	switch upper {
	case "F1":
		return vkF1
	case "F2":
		return vkF1 + 1
	case "F3":
		return vkF1 + 2
	case "F4":
		return vkF1 + 3
	case "F5":
		return vkF1 + 4
	case "F6":
		return vkF1 + 5
	case "F7":
		return vkF1 + 6
	case "F8":
		return vkF1 + 7
	case "F9":
		return vkF1 + 8
	case "F10":
		return vkF1 + 9
	case "F11":
		return vkF1 + 10
	case "F12":
		return vkF12
	case "INSERT":
		return vkInsert
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
	case "PRINT SCREEN", "PRINTSCREEN":
		return vkSnapshot
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
	// Single letter A-Z
	if len(upper) == 1 && upper[0] >= 'A' && upper[0] <= 'Z' {
		return vkA + uint32(upper[0]-'A')
	}
	// Single digit 0-9
	if len(upper) == 1 && upper[0] >= '0' && upper[0] <= '9' {
		return vk0 + uint32(upper[0]-'0')
	}
	return 0
}
