package keymap

import "testing"

func TestCGKeyCodeLettersAreAlphabeticalFrom0x04(t *testing.T) {
	// HID lays a..z out contiguously at 0x04. macOS key codes are scattered,
	// so this is the check that the hand-written table stayed in order.
	ordered := []uint32{
		cgKeyA, cgKeyB, cgKeyC, cgKeyD, cgKeyE, cgKeyF, cgKeyG, cgKeyH,
		cgKeyI, cgKeyJ, cgKeyK, cgKeyL, cgKeyM, cgKeyN, cgKeyO, cgKeyP,
		cgKeyQ, cgKeyR, cgKeyS, cgKeyT, cgKeyU, cgKeyV, cgKeyW, cgKeyX,
		cgKeyY, cgKeyZ,
	}
	if len(cgLetters) != 26 {
		t.Fatalf("cgLetters has %d entries, want 26", len(cgLetters))
	}
	for i, code := range ordered {
		want := byte(0x04 + i)
		got, ok := CGKeyCodeToUsage(code)
		if !ok || got != want {
			t.Errorf("letter %c (code 0x%02X): got 0x%02X ok=%v, want 0x%02X", 'a'+i, code, got, ok, want)
		}
	}
}

func TestCGKeyCodeDigits(t *testing.T) {
	ordered := []uint32{cgKey1, cgKey2, cgKey3, cgKey4, cgKey5, cgKey6, cgKey7, cgKey8, cgKey9}
	for i, code := range ordered {
		want := byte(0x1E + i)
		got, ok := CGKeyCodeToUsage(code)
		if !ok || got != want {
			t.Errorf("digit %d: got 0x%02X ok=%v, want 0x%02X", i+1, got, ok, want)
		}
	}
	// Zero sits after nine, not before one.
	if got, ok := CGKeyCodeToUsage(cgKey0); !ok || got != 0x27 {
		t.Errorf("digit 0: got 0x%02X ok=%v, want 0x27", got, ok)
	}
}

func TestCGKeyCodeFunctionKeys(t *testing.T) {
	f1to12 := []uint32{cgKeyF1, cgKeyF2, cgKeyF3, cgKeyF4, cgKeyF5, cgKeyF6,
		cgKeyF7, cgKeyF8, cgKeyF9, cgKeyF10, cgKeyF11, cgKeyF12}
	for i, code := range f1to12 {
		want := byte(0x3A + i)
		if got, ok := CGKeyCodeToUsage(code); !ok || got != want {
			t.Errorf("F%d: got 0x%02X ok=%v, want 0x%02X", i+1, got, ok, want)
		}
	}
	f13to20 := []uint32{cgKeyF13, cgKeyF14, cgKeyF15, cgKeyF16, cgKeyF17, cgKeyF18, cgKeyF19, cgKeyF20}
	for i, code := range f13to20 {
		want := byte(0x68 + i)
		if got, ok := CGKeyCodeToUsage(code); !ok || got != want {
			t.Errorf("F%d: got 0x%02X ok=%v, want 0x%02X", i+13, got, ok, want)
		}
	}
}

func TestCGKeyCodeKeypad(t *testing.T) {
	keypad := []struct {
		code uint32
		want byte
	}{
		{cgKeypad1, 0x59}, {cgKeypad2, 0x5A}, {cgKeypad3, 0x5B},
		{cgKeypad4, 0x5C}, {cgKeypad5, 0x5D}, {cgKeypad6, 0x5E},
		{cgKeypad7, 0x5F}, {cgKeypad8, 0x60}, {cgKeypad9, 0x61},
		{cgKeypad0, 0x62}, {cgKeypadDecimal, 0x63}, {cgKeypadEnter, 0x58},
		{cgKeypadDivide, 0x54}, {cgKeypadMultiply, 0x55},
		{cgKeypadMinus, 0x56}, {cgKeypadPlus, 0x57},
	}
	for _, tc := range keypad {
		if got, ok := CGKeyCodeToUsage(tc.code); !ok || got != tc.want {
			t.Errorf("keypad code 0x%02X: got 0x%02X ok=%v, want 0x%02X", tc.code, got, ok, tc.want)
		}
	}
}

// The Windows and macOS tables are independent transcriptions of the same HID
// page. Where both name the same physical key, they must agree — otherwise
// the two hosts would type different things on the same slave.
func TestCGKeyCodeAgreesWithWindowsTable(t *testing.T) {
	pairs := []struct {
		name string
		vk   uint32
		cg   uint32
	}{
		{"Return", vkReturn, cgKeyReturn},
		{"Escape", vkEscape, cgKeyEscape},
		{"Backspace", vkBack, cgKeyBackspace},
		{"Tab", vkTab, cgKeyTab},
		{"Space", vkSpace, cgKeySpace},
		{"CapsLock", vkCapital, cgKeyCapsLock},
		{"Home", vkHome, cgKeyHome},
		{"End", vkEnd, cgKeyEnd},
		{"PageUp", vkPrior, cgKeyPageUp},
		{"PageDown", vkNext, cgKeyPageDown},
		{"ForwardDelete", vkDelete, cgKeyForwardDelete},
		{"Insert", vkInsert, cgKeyHelp},
		{"Left", vkLeft, cgKeyLeftArrow},
		{"Right", vkRight, cgKeyRightArrow},
		{"Up", vkUp, cgKeyUpArrow},
		{"Down", vkDown, cgKeyDownArrow},
		{"Minus", vkOemMinus, cgKeyMinus},
		{"Equal", vkOemPlus, cgKeyEqual},
		{"LeftBracket", vkOem4, cgKeyLeftBracket},
		{"RightBracket", vkOem6, cgKeyRightBracket},
		{"Backslash", vkOem5, cgKeyBackslash},
		{"Semicolon", vkOem1, cgKeySemicolon},
		{"Quote", vkOem7, cgKeyQuote},
		{"Grave", vkOem3, cgKeyGrave},
		{"Comma", vkOemComma, cgKeyComma},
		{"Period", vkOemPeriod, cgKeyPeriod},
		{"Slash", vkOem2, cgKeySlash},
		{"NonUSBackslash", vkOem102, cgKeyISOSection},
		{"Application", vkApps, cgKeyContextMenu},
		{"A", vkA, cgKeyA},
		{"F1", vkF1, cgKeyF1},
		{"F12", vkF12, cgKeyF12},
		{"LeftCtrl", vkLControl, cgKeyControl},
		{"RightGUI", vkRWin, cgKeyRightCommand},
	}
	for _, pair := range pairs {
		winUsage, winOK := VKToUsage(pair.vk)
		macUsage, macOK := CGKeyCodeToUsage(pair.cg)
		if !winOK || !macOK {
			t.Errorf("%s: unmapped (windows ok=%v, macos ok=%v)", pair.name, winOK, macOK)
			continue
		}
		if winUsage != macUsage {
			t.Errorf("%s: windows 0x%02X != macos 0x%02X", pair.name, winUsage, macUsage)
		}
	}
}

func TestCGKeyCodeUsagesAreUnique(t *testing.T) {
	seen := map[byte]uint32{}
	for _, table := range []map[uint32]byte{cgLetters, cgDigits, cgOther} {
		for code, usage := range table {
			if prev, dup := seen[usage]; dup {
				t.Errorf("usage 0x%02X mapped from both code 0x%02X and 0x%02X", usage, prev, code)
			}
			seen[usage] = code
		}
	}
}

func TestModifierTableCoversAllEightUsages(t *testing.T) {
	seenUsage := map[byte]bool{}
	seenBit := map[uint64]bool{}
	seenCode := map[uint32]bool{}
	for i, mod := range Modifiers {
		want := byte(UsageLeftCtrl + i)
		if mod.Usage != want {
			t.Errorf("Modifiers[%d].Usage = 0x%02X, want 0x%02X (table must be in HID usage order)", i, mod.Usage, want)
		}
		if !IsModifierUsage(mod.Usage) {
			t.Errorf("Modifiers[%d].Usage 0x%02X is not recognized as a modifier", i, mod.Usage)
		}
		if seenUsage[mod.Usage] {
			t.Errorf("duplicate usage 0x%02X in Modifiers", mod.Usage)
		}
		if seenBit[mod.DeviceBit] {
			t.Errorf("duplicate device bit 0x%X in Modifiers", mod.DeviceBit)
		}
		if seenCode[mod.CGKeyCode] {
			t.Errorf("duplicate key code 0x%02X in Modifiers", mod.CGKeyCode)
		}
		if mod.DeviceBit == 0 || mod.BasicMask == 0 {
			t.Errorf("Modifiers[%d] has a zero mask", i)
		}
		seenUsage[mod.Usage] = true
		seenBit[mod.DeviceBit] = true
		seenCode[mod.CGKeyCode] = true

		// The general table must agree with the modifier table.
		if usage, ok := CGKeyCodeToUsage(mod.CGKeyCode); !ok || usage != mod.Usage {
			t.Errorf("code 0x%02X: general table gives 0x%02X ok=%v, modifier table gives 0x%02X",
				mod.CGKeyCode, usage, ok, mod.Usage)
		}
	}
}

func TestCapsLockAndFnAreNotForwardableModifiers(t *testing.T) {
	// Caps Lock latches — forwarding down/up pairs would desync the slave.
	if _, ok := ModifierForCGKeyCode(cgKeyCapsLock); ok {
		t.Error("Caps Lock must not be in the modifier table")
	}
	// Fn has no HID usage at all.
	if _, ok := ModifierForCGKeyCode(cgKeyFunction); ok {
		t.Error("Fn must not be in the modifier table")
	}
	if _, ok := CGKeyCodeToUsage(cgKeyFunction); ok {
		t.Error("Fn must not map to any HID usage")
	}
}

func TestModifierForCGKeyCodeRoundTrip(t *testing.T) {
	mod, ok := ModifierForCGKeyCode(cgKeyRightOption)
	if !ok {
		t.Fatal("right option should be a modifier")
	}
	if mod.Usage != UsageRightAlt {
		t.Errorf("right option usage = 0x%02X, want 0x%02X", mod.Usage, UsageRightAlt)
	}
	if mod.BasicMask != FlagMaskAlternate {
		t.Errorf("right option basic mask = 0x%X, want 0x%X", mod.BasicMask, FlagMaskAlternate)
	}
	if _, ok := ModifierForCGKeyCode(cgKeyA); ok {
		t.Error("a letter key must not be reported as a modifier")
	}
}

func TestCGKeyCodeUnmappedKeys(t *testing.T) {
	// Volume keys live on the HID Consumer page, which the firmware does not
	// implement, so they must not be forwarded as keyboard usages.
	for _, code := range []uint32{0x48, 0x49, 0x4A} {
		if _, ok := CGKeyCodeToUsage(code); ok {
			t.Errorf("media key 0x%02X should not map to a keyboard usage", code)
		}
	}
	if _, ok := CGKeyCodeToUsage(0xFFFF); ok {
		t.Error("an out-of-range key code should not map")
	}
}
