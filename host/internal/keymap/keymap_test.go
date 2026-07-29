package keymap

import "testing"

func TestLetterAndDigitRanges(t *testing.T) {
	cases := []struct {
		vk    uint32
		usage byte
	}{
		{0x41, 0x04}, // A -> a
		{0x5A, 0x1D}, // Z -> z
		{0x31, 0x1E}, // 1
		{0x39, 0x26}, // 9
		{0x30, 0x27}, // 0
		{0x70, 0x3A}, // F1
		{0x7B, 0x45}, // F12
		{0x60, 0x62}, // Numpad0
		{0x61, 0x59}, // Numpad1
		{0x69, 0x61}, // Numpad9
	}
	for _, c := range cases {
		usage, ok := VKToUsage(c.vk)
		if !ok || usage != c.usage {
			t.Errorf("VK 0x%02X -> 0x%02X ok=%v, want 0x%02X", c.vk, usage, ok, c.usage)
		}
	}
}

func TestModifiers(t *testing.T) {
	cases := []struct {
		vk    uint32
		usage byte
	}{
		{0xA2, UsageLeftCtrl},
		{0xA0, UsageLeftShift},
		{0xA4, UsageLeftAlt},
		{0x5B, UsageLeftGUI},
		{0xA3, UsageRightCtrl},
		{0xA1, UsageRightShift},
		{0xA5, UsageRightAlt},
		{0x5C, UsageRightGUI},
		{0x11, UsageLeftCtrl}, // generic folds left
	}
	for _, c := range cases {
		usage, ok := VKToUsage(c.vk)
		if !ok || usage != c.usage {
			t.Errorf("VK 0x%02X -> 0x%02X ok=%v, want 0x%02X", c.vk, usage, ok, c.usage)
		}
		if !IsModifierUsage(usage) {
			t.Errorf("usage 0x%02X not detected as modifier", usage)
		}
	}
}

func TestUnmappedKeysRejected(t *testing.T) {
	for _, vk := range []uint32{0x00, 0x01, 0x07, 0xFF, 0x15} {
		if usage, ok := VKToUsage(vk); ok {
			t.Errorf("VK 0x%02X unexpectedly mapped to 0x%02X", vk, usage)
		}
	}
}

func TestNavigationCluster(t *testing.T) {
	cases := map[uint32]byte{
		0x0D: 0x28, // Enter
		0x1B: 0x29, // Esc
		0x08: 0x2A, // Backspace
		0x09: 0x2B, // Tab
		0x20: 0x2C, // Space
		0x25: 0x50, // Left
		0x26: 0x52, // Up
		0x27: 0x4F, // Right
		0x28: 0x51, // Down
	}
	for vk, want := range cases {
		usage, ok := VKToUsage(vk)
		if !ok || usage != want {
			t.Errorf("VK 0x%02X -> 0x%02X ok=%v, want 0x%02X", vk, usage, ok, want)
		}
	}
}
