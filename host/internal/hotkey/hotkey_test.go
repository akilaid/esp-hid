package hotkey

import "testing"

func TestParsePlainKey(t *testing.T) {
	vk, mods := Parse("F9")
	if vk != VKF1+8 || mods != 0 {
		t.Errorf("F9 -> vk 0x%02X mods 0x%X", vk, mods)
	}
}

func TestParseWithModifiers(t *testing.T) {
	vk, mods := Parse("Ctrl+Alt+F7")
	if vk != VKF1+6 || mods != ModCtrl|ModAlt {
		t.Errorf("Ctrl+Alt+F7 -> vk 0x%02X mods 0x%X", vk, mods)
	}
	vk, mods = Parse("shift+x")
	if vk != VKA+23 || mods != ModShift {
		t.Errorf("shift+x -> vk 0x%02X mods 0x%X", vk, mods)
	}
}

func TestNormalizeCanonicalOrder(t *testing.T) {
	got, ok := Normalize("alt+ctrl+f7")
	if !ok || got != "Ctrl+Alt+F7" {
		t.Errorf("normalize = %q ok=%v", got, ok)
	}
	if _, ok := Normalize("Escape"); ok {
		t.Error("Escape must be rejected as a trigger key")
	}
	if _, ok := Normalize(""); ok {
		t.Error("empty combo must be rejected")
	}
}

func TestFormatRoundTrip(t *testing.T) {
	for _, name := range []string{"F1", "F12", "Ctrl+F9", "Ctrl+Shift+A", "Num 5", "Page Up", "Win+Home"} {
		vk, mods := Parse(name)
		if vk == 0 {
			t.Errorf("%q failed to parse", name)
			continue
		}
		if got := Format(vk, mods); got != name {
			t.Errorf("round trip %q -> %q", name, got)
		}
	}
}

func TestModifierClassification(t *testing.T) {
	if !IsModifierVK(VKLShift) || IsModifierVK(VKF1) {
		t.Error("modifier classification broken")
	}
	if ModBitForVK(VKRMenu) != ModAlt {
		t.Error("RAlt should map to ModAlt")
	}
}
