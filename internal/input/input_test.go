package input

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func event(typ, code uint16, value int32) []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint16(b[16:], typ)
	binary.LittleEndian.PutUint16(b[18:], code)
	binary.LittleEndian.PutUint32(b[20:], uint32(value))
	return b
}

func TestRead(t *testing.T) {
	var b []byte
	for _, e := range [][]byte{
		event(1, 183, 1), // key 0 down
		event(0, 0, 0),   // EV_SYN
		event(1, 183, 2), // autorepeat
		event(1, 30, 1),  // not a keypad key
		event(1, 149, 1), // wide key down
		event(1, 183, 0), // key 0 up
	} {
		b = append(b, e...)
	}
	b = append(b, 1, 2, 3) // truncated
	p := filepath.Join(t.TempDir(), "event0")
	os.WriteFile(p, b, 0o644)

	ch := make(chan Event, 10)
	if err := Read(p, ch); err == nil {
		t.Error("no error at the end of the device")
	}
	close(ch)
	var got []Event
	for e := range ch {
		got = append(got, e)
	}
	want := []Event{{0, true}, {13, true}, {0, false}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d: %v, want %v", i, got[i], want[i])
		}
	}
	if Read(filepath.Join(t.TempDir(), "missing"), ch) == nil {
		t.Error("opened a missing device")
	}
}

func TestFind(t *testing.T) {
	old := sysInput
	defer func() { sysInput = old }()
	sysInput = t.TempDir()
	if Find() != "" {
		t.Error("found a keypad in an empty tree")
	}
	for n, name := range map[string]string{"event0": "gpio-keys", "event3": "adc-keypad"} {
		d := filepath.Join(sysInput, n, "device")
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "name"), []byte(name+"\n"), 0o644)
	}
	if got := Find(); got != "/dev/input/event3" {
		t.Errorf("Find() = %q", got)
	}
}
