//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
)

const defaultToggleHotkeyName = "F9"

var toggleHotkeyChoices = []string{
	"F1",
	"F2",
	"F3",
	"F4",
	"F5",
	"F6",
	"F7",
	"F8",
	"F9",
	"F10",
	"F11",
	"F12",
}

func normalizeToggleHotkeyName(name string) (string, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	if _, ok := toggleHotkeyNameToVK(normalized); !ok {
		return "", false
	}
	return normalized, true
}

func toggleHotkeyNameToVK(name string) (uint32, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	if !strings.HasPrefix(normalized, "F") {
		return 0, false
	}

	numberText := strings.TrimPrefix(normalized, "F")
	number, err := strconv.Atoi(numberText)
	if err != nil || number < 1 || number > 12 {
		return 0, false
	}

	return uint32(vkF1 + (number - 1)), true
}

func toggleHotkeyVKToName(vk uint32) string {
	if vk < uint32(vkF1) || vk > uint32(vkF12) {
		return defaultToggleHotkeyName
	}
	return fmt.Sprintf("F%d", int(vk-uint32(vkF1))+1)
}
