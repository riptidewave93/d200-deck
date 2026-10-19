// Package input reads the D200 front keys from their evdev device.
package input

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Key codes the keypad emits, in key order (see deck.KeyCount).
var keyCodes = map[uint16]int{
	183: 0, 184: 1, 185: 2, 186: 3, 187: 4, // KEY_F13..F17: top row
	188: 5, 189: 6, 190: 7, 191: 8, 192: 9, // KEY_F18..F22: middle row
	193: 10, 194: 11, 148: 12, // KEY_F23, F24, PROG1: bottom row
	149: 13, // KEY_PROG2: the wide key
}

// Event is a key going down or up.
type Event struct {
	Key     int
	Pressed bool
}

// sysInput is where the kernel lists input devices.
var sysInput = "/sys/class/input"

// Find returns the event device of the keypad, or "" if there is none.
func Find() string {
	names, _ := filepath.Glob(sysInput + "/event*/device/name")
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err == nil && strings.Contains(string(b), "keypad") {
			return "/dev/input/" + filepath.Base(filepath.Dir(filepath.Dir(n)))
		}
	}
	return ""
}

// Read sends key events from the evdev device at path to ch until it fails.
func Read(path string, ch chan<- Event) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// struct input_event on 64-bit: 16 byte timestamp, u16 type, u16 code, s32 value
	var ev [24]byte
	for {
		if _, err := io.ReadFull(f, ev[:]); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		typ := binary.LittleEndian.Uint16(ev[16:])
		code := binary.LittleEndian.Uint16(ev[18:])
		value := int32(binary.LittleEndian.Uint32(ev[20:]))
		key, ok := keyCodes[code]
		if typ != 1 || !ok || value == 2 { // EV_KEY only, no autorepeat
			continue
		}
		ch <- Event{Key: key, Pressed: value == 1}
	}
}
