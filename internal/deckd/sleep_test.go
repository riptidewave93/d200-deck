package deckd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/render"
)

func TestWatchUSB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("configured")
	changes := make(chan string)
	stop := errors.New("stop")
	wait := func(*os.File) error {
		s, ok := <-changes
		if !ok {
			return stop
		}
		write(s)
		return nil
	}
	states := make(chan string, 8)
	done := make(chan error)
	go func() { done <- watchUSB(path, wait, states) }()
	for _, s := range []string{"suspended", "suspended", "configured", "default", "default", "suspended"} {
		changes <- s
	}
	close(changes)
	if err := <-done; err != stop {
		t.Fatalf("watchUSB returned %v", err)
	}
	close(states)
	var got []string
	for v := range states {
		got = append(got, v)
	}
	if want := []string{"configured", "suspended", "configured", "default", "suspended"}; !slices.Equal(got, want) {
		t.Errorf("sent %v, want %v", got, want)
	}
	if err := watchUSB(filepath.Join(t.TempDir(), "none"), wait, states); err == nil {
		t.Error("watchUSB of a missing file succeeded")
	}
}

// fakeBacklight makes a sysfs backlight with levels 0-8 at level, for
// backlights made after it.
func fakeBacklight(t *testing.T, level string) (brightness func() string) {
	dir, old := t.TempDir(), backlightGlob
	t.Cleanup(func() { backlightGlob = old })
	backlightGlob = dir
	os.WriteFile(filepath.Join(dir, "max_brightness"), []byte("8\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "brightness"), []byte(level+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "bl_power"), []byte("0\n"), 0o644)
	return func() string {
		b, _ := os.ReadFile(filepath.Join(dir, "brightness"))
		return string(b)
	}
}

func TestSleepScreen(t *testing.T) {
	// deckd starts dimmed, after a restart while the PC slept: the saved
	// level comes back
	brightness := fakeBacklight(t, "1")
	saved := filepath.Join(t.TempDir(), "brightness")
	os.WriteFile(saved, []byte("75\n"), 0o644)
	oldWake := wakePC
	t.Cleanup(func() { wakePC = oldWake })
	wakes := 0
	wakePC = func() error { wakes++; return nil }

	d := newTestDeckd()
	d.light = newBacklight(saved)
	if brightness() != "6" {
		t.Errorf("start: brightness %q, want 6", brightness())
	}
	d.drawPage()
	page := slices.Clone(d.canvas.Pix)

	// reset by the PC: asleep, but presses can't wake it
	d.setUSB("default")
	if brightness() != "1" {
		t.Errorf("asleep: brightness %q, want 1", brightness())
	}
	for i := range deck.KeyCount {
		r := render.Key(i)
		lit := false
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				lit = lit || d.canvas.Pix[y*d.canvas.W+x] != 0
			}
		}
		if lit != (i == sleepKey) {
			t.Errorf("asleep: key %d lit %v", i, lit)
		}
	}

	// presses run nothing (key 0 opens a page); suspended, they ask the PC
	// to wake once
	for _, state := range []string{"default", "suspended", "suspended"} {
		d.setUSB(state)
		d.press(0, true)
		d.press(0, false)
	}
	if len(d.pages) != 1 || wakes != 1 {
		t.Errorf("asleep presses: pages %v, %d wake requests", d.pages, wakes)
	}

	d.setUSB("configured")
	if brightness() != "6" {
		t.Errorf("awake: brightness %q, want 6", brightness())
	}
	if !slices.Equal(d.canvas.Pix, page) {
		t.Error("awake: the page isn't drawn as before")
	}
}

// blPower is the fake backlight's bl_power.
func blPower() string {
	b, _ := os.ReadFile(filepath.Join(backlightGlob, "bl_power"))
	return strings.TrimSpace(string(b))
}

// fakePower is a display's power: it notes each change, with the backlight
// and the screen as they were then.
type fakePower struct {
	d      *deckd
	events []string
	shown  [][]uint32 // the canvas at each On
}

func (f *fakePower) Off() error {
	f.events = append(f.events, "off, backlight "+blPower())
	return nil
}

func (f *fakePower) On() error {
	f.events = append(f.events, "on, backlight "+blPower())
	f.shown = append(f.shown, slices.Clone(f.d.canvas.Pix))
	return nil
}

func TestSleepScreenOff(t *testing.T) {
	brightness := fakeBacklight(t, "8")
	saved := filepath.Join(t.TempDir(), "brightness")
	os.WriteFile(saved, []byte("75\n"), 0o644)
	d := newTestDeckd()
	d.light = newBacklight(saved)
	power := &fakePower{d: d}
	d.power = power
	d.sleep.load(filepath.Join(t.TempDir(), "sleep_screen"))
	if status, reply := d.settings(1, []byte(`{"sleep_screen": "off"}`)); status != proto.StatusOK {
		t.Fatalf("set off: %d %q", status, reply)
	}
	(<-d.ui)() // as the main loop would
	d.drawPage()
	page := slices.Clone(d.canvas.Pix)

	d.setAsleep(true)
	if blPower() != powerOff || !slices.Equal(power.events, []string{"off, backlight " + powerOff}) {
		t.Errorf("asleep: bl_power %q, events %q", blPower(), power.events)
	}
	d.setAsleep(false)
	// the panel is back on, showing the page, before the backlight
	if len(power.events) != 2 || power.events[1] != "on, backlight "+powerOff || !slices.Equal(power.shown[0], page) {
		t.Errorf("awake: events %q, page shown %v", power.events, len(power.shown) > 0 && slices.Equal(power.shown[0], page))
	}
	if blPower() != powerOn || brightness() != "6" {
		t.Errorf("awake: bl_power %q, brightness %q", blPower(), brightness())
	}

	// the default dims, and leaves the panel on
	d.settings(1, []byte(`{"sleep_screen": "zzz"}`))
	(<-d.ui)()
	d.setAsleep(true)
	if len(power.events) != 2 || blPower() != powerOn || brightness() != "1" {
		t.Errorf("zzz asleep: events %q, bl_power %q, brightness %q", power.events, blPower(), brightness())
	}
	// turned off while the PC sleeps, it goes off at once
	d.settings(1, []byte(`{"sleep_screen": "off"}`))
	(<-d.ui)()
	if len(power.events) != 3 || blPower() != powerOff {
		t.Errorf("set off asleep: events %q, bl_power %q", power.events, blPower())
	}
	d.setAsleep(false)
	if blPower() != powerOn || brightness() != "6" {
		t.Errorf("awake again: bl_power %q, brightness %q", blPower(), brightness())
	}
}

// A deckd started after one stopped with the backlight off turns it on.
func TestBacklightOnAtStart(t *testing.T) {
	fakeBacklight(t, "8")
	os.WriteFile(filepath.Join(backlightGlob, "bl_power"), []byte(powerOff), 0o644)
	newBacklight(filepath.Join(t.TempDir(), "brightness"))
	if blPower() != powerOn {
		t.Errorf("bl_power %q", blPower())
	}
}
