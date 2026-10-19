package deck

import (
	"fmt"
	"strings"
)

// Keyboard modifier bits (HID boot keyboard byte 0).
var Modifiers = map[string]byte{
	"ctrl": 0x01, "shift": 0x02, "alt": 0x04, "gui": 0x08,
	"rctrl": 0x10, "rshift": 0x20, "ralt": 0x40, "rgui": 0x80,
	// aliases
	"control": 0x01, "win": 0x08, "super": 0x08, "meta": 0x08, "cmd": 0x08,
	"altgr": 0x40,
}

// Keys are the HID keyboard usages (page 0x07) by name.
var Keys = func() map[string]byte {
	k := map[string]byte{
		"enter": 0x28, "esc": 0x29, "backspace": 0x2a, "tab": 0x2b, "space": 0x2c,
		"minus": 0x2d, "equal": 0x2e, "leftbrace": 0x2f, "rightbrace": 0x30,
		"backslash": 0x31, "semicolon": 0x33, "apostrophe": 0x34, "grave": 0x35,
		"comma": 0x36, "dot": 0x37, "slash": 0x38, "capslock": 0x39,
		"printscreen": 0x46, "scrolllock": 0x47, "pause": 0x48, "insert": 0x49,
		"home": 0x4a, "pageup": 0x4b, "delete": 0x4c, "end": 0x4d, "pagedown": 0x4e,
		"right": 0x4f, "left": 0x50, "down": 0x51, "up": 0x52, "menu": 0x65,
		"return": 0x28, "escape": 0x29, "del": 0x4c,
	}
	for i := 0; i < 26; i++ {
		k[string(rune('a'+i))] = byte(0x04 + i)
	}
	for i := 1; i <= 9; i++ {
		k[string(rune('0'+i))] = byte(0x1e + i - 1)
	}
	k["0"] = 0x27
	for i := 1; i <= 12; i++ {
		k[fmt.Sprintf("f%d", i)] = byte(0x3a + i - 1)
	}
	for i := 13; i <= 24; i++ {
		k[fmt.Sprintf("f%d", i)] = byte(0x68 + i - 13)
	}
	return k
}()

// MaxKeys is how many non-modifier keys one report can hold.
const MaxKeys = 6

// ParseKeys turns key names (e.g. "ctrl", "shift", "m") into modifier bits
// and key usages.
func ParseKeys(names []string) (mods byte, keys []byte, err error) {
	for _, n := range names {
		n = strings.ToLower(n)
		if m, ok := Modifiers[n]; ok {
			mods |= m
		} else if k, ok := Keys[n]; ok {
			keys = append(keys, k)
		} else {
			return 0, nil, fmt.Errorf("unknown key %q", n)
		}
	}
	if len(keys) > MaxKeys {
		return 0, nil, fmt.Errorf("more than %d keys", MaxKeys)
	}
	return mods, keys, nil
}

// usShifted maps the shifted characters of a US keyboard to their unshifted key.
var usShifted = map[rune]rune{
	'!': '1', '@': '2', '#': '3', '$': '4', '%': '5', '^': '6', '&': '7', '*': '8',
	'(': '9', ')': '0', '_': '-', '+': '=', '{': '[', '}': ']', '|': '\\',
	':': ';', '"': '\'', '~': '`', '<': ',', '>': '.', '?': '/',
}

var usKeys = map[rune]byte{
	' ': 0x2c, '\n': 0x28, '\t': 0x2b, '-': 0x2d, '=': 0x2e, '[': 0x2f, ']': 0x30,
	'\\': 0x31, ';': 0x33, '\'': 0x34, '`': 0x35, ',': 0x36, '.': 0x37, '/': 0x38,
}

// TypeKey is the key press that types r on a host using the US layout.
func TypeKey(r rune) (mods, key byte, ok bool) {
	if u, shifted := usShifted[r]; shifted {
		_, key, ok = TypeKey(u)
		return 0x02, key, ok
	}
	switch {
	case r >= 'a' && r <= 'z':
		return 0, byte(0x04 + r - 'a'), true
	case r >= 'A' && r <= 'Z':
		return 0x02, byte(0x04 + r - 'A'), true
	case r >= '1' && r <= '9':
		return 0, byte(0x1e + r - '1'), true
	case r == '0':
		return 0, 0x27, true
	}
	key, ok = usKeys[r]
	return 0, key, ok
}
