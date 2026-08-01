package hotkey

import (
	"testing"

	"esp-hid/host/internal/keymap"
)

// The property that matters: any hotkey the user can express must be usable
// on macOS. A missing table entry would otherwise show up as "my hotkey does
// nothing on the Mac" with no error anywhere.
func TestEveryNameableKeyHasACGKeyCode(t *testing.T) {
	checked := 0
	for vk := uint32(0); vk <= 0xFF; vk++ {
		name, nameable := vkToName(vk)
		if !nameable {
			continue
		}
		checked++
		if _, ok := CGKeyCodeFor(vk); !ok {
			t.Errorf("hotkey %q (VK 0x%02X) has no macOS key code", name, vk)
		}
	}
	if checked < 60 {
		t.Fatalf("only %d nameable keys found; the grammar should cover far more", checked)
	}
}

// Every macOS key code this package emits must also be a key the keymap
// package can turn into a HID usage — the two tables are transcribed
// independently, so this catches a typo in either one.
func TestCGKeyCodesAreKnownToKeymap(t *testing.T) {
	for vk, cgKeyCode := range vkToCGKey {
		if _, ok := keymap.CGKeyCodeToUsage(cgKeyCode); !ok {
			name, _ := vkToName(vk)
			t.Errorf("key %q (VK 0x%02X) maps to macOS code 0x%02X, which keymap does not know",
				name, vk, cgKeyCode)
		}
	}
}

func TestCGKeyCodesAreUnique(t *testing.T) {
	seen := map[uint32]uint32{}
	for vk, cgKeyCode := range vkToCGKey {
		if prev, dup := seen[cgKeyCode]; dup {
			t.Errorf("macOS code 0x%02X claimed by both VK 0x%02X and VK 0x%02X", cgKeyCode, prev, vk)
		}
		seen[cgKeyCode] = vk
	}
}

func TestParseDarwinSpotChecks(t *testing.T) {
	cases := []struct {
		name     string
		wantKey  uint32
		wantMods uint32
	}{
		{"F9", 0x65, 0},
		{"F1", 0x7A, 0},
		{"F12", 0x6F, 0},
		{"A", 0x00, 0},
		{"Z", 0x06, 0},
		{"5", 0x17, 0},
		{"0", 0x1D, 0},
		{"Ctrl+Alt+F7", 0x62, ModCtrl | ModAlt},
		{"Shift+X", 0x07, ModShift},
		{"Win+Q", 0x0C, ModWin},
		{"Num 5", 0x57, 0},
		{"Num +", 0x45, 0},
		{"Home", 0x73, 0},
		{"Page Down", 0x79, 0},
	}
	for _, tc := range cases {
		gotKey, gotMods := ParseDarwin(tc.name)
		if gotKey != tc.wantKey || gotMods != tc.wantMods {
			t.Errorf("ParseDarwin(%q) = (0x%02X, %d), want (0x%02X, %d)",
				tc.name, gotKey, gotMods, tc.wantKey, tc.wantMods)
		}
	}
}

func TestParseDarwinRejectsInvalid(t *testing.T) {
	for _, name := range []string{"", "Ctrl+", "NotAKey", "F99", "Ctrl+Alt"} {
		if key, _ := ParseDarwin(name); key != 0 {
			t.Errorf("ParseDarwin(%q) = 0x%02X, want 0 (invalid)", name, key)
		}
	}
}

// A settings file written by the Windows host must keep working when the
// same user opens it on a Mac.
func TestPersistedNameRoundTripsAcrossPlatforms(t *testing.T) {
	for _, name := range []string{"F9", "Ctrl+Alt+F7", "Shift+Num 3", "Win+Delete"} {
		canonical, ok := Normalize(name)
		if !ok {
			t.Errorf("Normalize(%q) failed", name)
			continue
		}
		vk, mods := Parse(canonical)
		if vk == 0 {
			t.Errorf("canonical form %q did not re-parse", canonical)
			continue
		}
		if Format(vk, mods) != canonical {
			t.Errorf("Format(Parse(%q)) = %q, not stable", canonical, Format(vk, mods))
		}
		if key, darwinMods := ParseDarwin(canonical); key == 0 || darwinMods != mods {
			t.Errorf("ParseDarwin(%q) = (0x%02X, %d), want a valid key with mods %d",
				canonical, key, darwinMods, mods)
		}
	}
}

func TestModifierKeysHaveNoStandaloneBinding(t *testing.T) {
	// Pure modifiers are not bindable as the combo's key, so they must not
	// appear in the macOS table either.
	for vk := range modifierVKs {
		if _, ok := CGKeyCodeFor(vk); ok {
			t.Errorf("modifier VK 0x%02X must not be bindable as a hotkey key", vk)
		}
	}
}
