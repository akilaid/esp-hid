package keymap

// macOS CGKeyCode (Carbon kVK_*) to USB HID Keyboard/Keypad usage.
//
// These are positional hardware key codes, which is exactly what a KVM wants:
// the usage describes *which key was pressed*, and the slave device applies
// its own keyboard layout. The US-layout assumption therefore only affects
// how punctuation keys are labelled here, not correctness on the far side.
//
// This file has no build tag and no _darwin suffix on purpose. It is a pure
// lookup table over uint32, so keeping it portable means the table and its
// tests compile and run on every platform's CI, not just macOS. Only the
// caller (internal/capture/capture_darwin.go) is platform-specific.

// macOS virtual key codes. Deliberately non-contiguous — Apple assigned them
// by physical position on the original ADB keyboard, so the letter, digit and
// function-key blocks all need explicit tables rather than arithmetic.
const (
	cgKeyA = 0x00
	cgKeyS = 0x01
	cgKeyD = 0x02
	cgKeyF = 0x03
	cgKeyH = 0x04
	cgKeyG = 0x05
	cgKeyZ = 0x06
	cgKeyX = 0x07
	cgKeyC = 0x08
	cgKeyV = 0x09
	cgKeyB = 0x0B
	cgKeyQ = 0x0C
	cgKeyW = 0x0D
	cgKeyE = 0x0E
	cgKeyR = 0x0F
	cgKeyY = 0x10
	cgKeyT = 0x11
	cgKeyO = 0x1F
	cgKeyU = 0x20
	cgKeyI = 0x22
	cgKeyP = 0x23
	cgKeyL = 0x25
	cgKeyJ = 0x26
	cgKeyK = 0x28
	cgKeyN = 0x2D
	cgKeyM = 0x2E

	cgKey1 = 0x12
	cgKey2 = 0x13
	cgKey3 = 0x14
	cgKey4 = 0x15
	cgKey6 = 0x16
	cgKey5 = 0x17
	cgKey9 = 0x19
	cgKey7 = 0x1A
	cgKey8 = 0x1C
	cgKey0 = 0x1D

	cgKeyEqual        = 0x18
	cgKeyMinus        = 0x1B
	cgKeyRightBracket = 0x1E
	cgKeyLeftBracket  = 0x21
	cgKeyQuote        = 0x27
	cgKeySemicolon    = 0x29
	cgKeyBackslash    = 0x2A
	cgKeyComma        = 0x2B
	cgKeySlash        = 0x2C
	cgKeyPeriod       = 0x2F
	cgKeyGrave        = 0x32
	cgKeyISOSection   = 0x0A // the extra key on ISO/European keyboards

	cgKeyReturn    = 0x24
	cgKeyTab       = 0x30
	cgKeySpace     = 0x31
	cgKeyBackspace = 0x33 // kVK_Delete — backspace, not forward delete
	cgKeyEscape    = 0x35
	cgKeyCapsLock  = 0x39
	cgKeyFunction  = 0x3F

	// Modifiers.
	cgKeyCommand      = 0x37
	cgKeyShift        = 0x38
	cgKeyOption       = 0x3A
	cgKeyControl      = 0x3B
	cgKeyRightShift   = 0x3C
	cgKeyRightOption  = 0x3D
	cgKeyRightControl = 0x3E
	cgKeyRightCommand = 0x36

	// Keypad.
	cgKeypadDecimal  = 0x41
	cgKeypadMultiply = 0x43
	cgKeypadPlus     = 0x45
	cgKeypadClear    = 0x47
	cgKeypadDivide   = 0x4B
	cgKeypadEnter    = 0x4C
	cgKeypadMinus    = 0x4E
	cgKeypadEquals   = 0x51
	cgKeypad0        = 0x52
	cgKeypad1        = 0x53
	cgKeypad2        = 0x54
	cgKeypad3        = 0x55
	cgKeypad4        = 0x56
	cgKeypad5        = 0x57
	cgKeypad6        = 0x58
	cgKeypad7        = 0x59
	cgKeypad8        = 0x5B
	cgKeypad9        = 0x5C

	// Navigation. kVK_Help occupies the Insert position on PC keyboards.
	cgKeyHelp          = 0x72
	cgKeyHome          = 0x73
	cgKeyPageUp        = 0x74
	cgKeyForwardDelete = 0x75
	cgKeyEnd           = 0x77
	cgKeyPageDown      = 0x79
	cgKeyLeftArrow     = 0x7B
	cgKeyRightArrow    = 0x7C
	cgKeyDownArrow     = 0x7D
	cgKeyUpArrow       = 0x7E
	cgKeyContextMenu   = 0x6E

	// Function keys, scattered across the range.
	cgKeyF1  = 0x7A
	cgKeyF2  = 0x78
	cgKeyF3  = 0x63
	cgKeyF4  = 0x76
	cgKeyF5  = 0x60
	cgKeyF6  = 0x61
	cgKeyF7  = 0x62
	cgKeyF8  = 0x64
	cgKeyF9  = 0x65
	cgKeyF10 = 0x6D
	cgKeyF11 = 0x67
	cgKeyF12 = 0x6F
	cgKeyF13 = 0x69
	cgKeyF14 = 0x6B
	cgKeyF15 = 0x71
	cgKeyF16 = 0x6A
	cgKeyF17 = 0x40
	cgKeyF18 = 0x4F
	cgKeyF19 = 0x50
	cgKeyF20 = 0x5A

	// JIS keyboard extras with unambiguous HID equivalents.
	cgKeyJISYen         = 0x5D
	cgKeyJISUnderscore  = 0x5E
	cgKeyJISKeypadComma = 0x5F
)

// CGEventFlags bits. The basic masks say a modifier is down but not which
// side; the device-dependent bits are side-specific and are what this package
// uses to drive per-key HID usages.
const (
	FlagMaskShift     uint64 = 0x00020000
	FlagMaskControl   uint64 = 0x00040000
	FlagMaskAlternate uint64 = 0x00080000
	FlagMaskCommand   uint64 = 0x00100000

	deviceLeftControl  uint64 = 0x00000001
	deviceLeftShift    uint64 = 0x00000002
	deviceRightShift   uint64 = 0x00000004
	deviceLeftCommand  uint64 = 0x00000008
	deviceRightCommand uint64 = 0x00000010
	deviceLeftOption   uint64 = 0x00000020
	deviceRightOption  uint64 = 0x00000040
	deviceRightControl uint64 = 0x00002000
)

// Modifier ties a macOS modifier key to its HID usage and to both the
// side-specific and side-agnostic flag bits that report it.
type Modifier struct {
	CGKeyCode uint32
	Usage     byte
	DeviceBit uint64 // side-specific, preferred
	BasicMask uint64 // side-agnostic fallback
}

// Modifiers lists the eight forwardable modifier keys in HID usage order
// (0xE0..0xE7). macOS reports these as kCGEventFlagsChanged rather than as
// key down/up events, so the capture layer reconciles this whole table
// against the event's flags on every change.
//
// Caps Lock and Fn are deliberately absent. Caps Lock is a latching toggle
// whose flag reports LED state rather than key state, so forwarding down/up
// pairs would desync the slave's caps state permanently; Fn has no HID usage
// at all.
var Modifiers = [8]Modifier{
	{cgKeyControl, UsageLeftCtrl, deviceLeftControl, FlagMaskControl},
	{cgKeyShift, UsageLeftShift, deviceLeftShift, FlagMaskShift},
	{cgKeyOption, UsageLeftAlt, deviceLeftOption, FlagMaskAlternate},
	{cgKeyCommand, UsageLeftGUI, deviceLeftCommand, FlagMaskCommand},
	{cgKeyRightControl, UsageRightCtrl, deviceRightControl, FlagMaskControl},
	{cgKeyRightShift, UsageRightShift, deviceRightShift, FlagMaskShift},
	{cgKeyRightOption, UsageRightAlt, deviceRightOption, FlagMaskAlternate},
	{cgKeyRightCommand, UsageRightGUI, deviceRightCommand, FlagMaskCommand},
}

// ModifierForCGKeyCode returns the modifier entry for a key code, if it is one
// of the eight forwardable modifiers.
func ModifierForCGKeyCode(code uint32) (Modifier, bool) {
	for _, mod := range Modifiers {
		if mod.CGKeyCode == code {
			return mod, true
		}
	}
	return Modifier{}, false
}

var cgLetters = map[uint32]byte{
	cgKeyA: 0x04, cgKeyB: 0x05, cgKeyC: 0x06, cgKeyD: 0x07, cgKeyE: 0x08,
	cgKeyF: 0x09, cgKeyG: 0x0A, cgKeyH: 0x0B, cgKeyI: 0x0C, cgKeyJ: 0x0D,
	cgKeyK: 0x0E, cgKeyL: 0x0F, cgKeyM: 0x10, cgKeyN: 0x11, cgKeyO: 0x12,
	cgKeyP: 0x13, cgKeyQ: 0x14, cgKeyR: 0x15, cgKeyS: 0x16, cgKeyT: 0x17,
	cgKeyU: 0x18, cgKeyV: 0x19, cgKeyW: 0x1A, cgKeyX: 0x1B, cgKeyY: 0x1C,
	cgKeyZ: 0x1D,
}

var cgDigits = map[uint32]byte{
	cgKey1: 0x1E, cgKey2: 0x1F, cgKey3: 0x20, cgKey4: 0x21, cgKey5: 0x22,
	cgKey6: 0x23, cgKey7: 0x24, cgKey8: 0x25, cgKey9: 0x26, cgKey0: 0x27,
}

var cgOther = map[uint32]byte{
	cgKeyReturn:    0x28,
	cgKeyEscape:    0x29,
	cgKeyBackspace: 0x2A,
	cgKeyTab:       0x2B,
	cgKeySpace:     0x2C,

	// Punctuation, US layout.
	cgKeyMinus:        0x2D,
	cgKeyEqual:        0x2E,
	cgKeyLeftBracket:  0x2F,
	cgKeyRightBracket: 0x30,
	cgKeyBackslash:    0x31,
	cgKeySemicolon:    0x33,
	cgKeyQuote:        0x34,
	cgKeyGrave:        0x35,
	cgKeyComma:        0x36,
	cgKeyPeriod:       0x37,
	cgKeySlash:        0x38,
	cgKeyISOSection:   0x64, // Non-US backslash

	cgKeyCapsLock: 0x39,

	// Function keys.
	cgKeyF1: 0x3A, cgKeyF2: 0x3B, cgKeyF3: 0x3C, cgKeyF4: 0x3D,
	cgKeyF5: 0x3E, cgKeyF6: 0x3F, cgKeyF7: 0x40, cgKeyF8: 0x41,
	cgKeyF9: 0x42, cgKeyF10: 0x43, cgKeyF11: 0x44, cgKeyF12: 0x45,
	cgKeyF13: 0x68, cgKeyF14: 0x69, cgKeyF15: 0x6A, cgKeyF16: 0x6B,
	cgKeyF17: 0x6C, cgKeyF18: 0x6D, cgKeyF19: 0x6E, cgKeyF20: 0x6F,

	// Navigation.
	cgKeyHelp:          0x49, // Insert
	cgKeyHome:          0x4A,
	cgKeyPageUp:        0x4B,
	cgKeyForwardDelete: 0x4C,
	cgKeyEnd:           0x4D,
	cgKeyPageDown:      0x4E,
	cgKeyRightArrow:    0x4F,
	cgKeyLeftArrow:     0x50,
	cgKeyDownArrow:     0x51,
	cgKeyUpArrow:       0x52,
	cgKeyContextMenu:   0x65, // Application

	// Keypad. macOS Clear sits where Num Lock does on a PC keyboard.
	cgKeypadClear:    0x53,
	cgKeypadDivide:   0x54,
	cgKeypadMultiply: 0x55,
	cgKeypadMinus:    0x56,
	cgKeypadPlus:     0x57,
	cgKeypadEnter:    0x58,
	cgKeypad1:        0x59,
	cgKeypad2:        0x5A,
	cgKeypad3:        0x5B,
	cgKeypad4:        0x5C,
	cgKeypad5:        0x5D,
	cgKeypad6:        0x5E,
	cgKeypad7:        0x5F,
	cgKeypad8:        0x60,
	cgKeypad9:        0x61,
	cgKeypad0:        0x62,
	cgKeypadDecimal:  0x63,
	cgKeypadEquals:   0x67,

	// JIS keys with unambiguous HID equivalents. Eisu and Kana are omitted:
	// their usage assignment varies between vendors and guessing wrong is
	// worse than not forwarding them.
	cgKeyJISUnderscore:  0x87, // International1
	cgKeyJISYen:         0x89, // International3
	cgKeyJISKeypadComma: 0x85, // Keypad Comma

	// Modifiers.
	cgKeyControl:      UsageLeftCtrl,
	cgKeyShift:        UsageLeftShift,
	cgKeyOption:       UsageLeftAlt,
	cgKeyCommand:      UsageLeftGUI,
	cgKeyRightControl: UsageRightCtrl,
	cgKeyRightShift:   UsageRightShift,
	cgKeyRightOption:  UsageRightAlt,
	cgKeyRightCommand: UsageRightGUI,
}

// CGKeyCodeToUsage maps a macOS CGKeyCode to a HID usage.
// ok is false for keys with no mapping (they should not be forwarded) —
// notably Fn, the media keys, and the JIS input-mode keys.
func CGKeyCodeToUsage(code uint32) (usage byte, ok bool) {
	if usage, ok = cgLetters[code]; ok {
		return usage, true
	}
	if usage, ok = cgDigits[code]; ok {
		return usage, true
	}
	usage, ok = cgOther[code]
	return usage, ok
}
