package deckd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riptidewave93/d200-deck/deck"
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
	asleep := make(chan bool, 8)
	done := make(chan error)
	go func() { done <- watchUSB(path, wait, asleep) }()
	for _, s := range []string{"suspended", "suspended", "configured", "default", "suspended"} {
		changes <- s
	}
	close(changes)
	if err := <-done; err != stop {
		t.Fatalf("watchUSB returned %v", err)
	}
	close(asleep)
	var got []bool
	for v := range asleep {
		got = append(got, v)
	}
	if want := []bool{false, true, false, true}; !slices.Equal(got, want) {
		t.Errorf("sent %v, want %v", got, want)
	}
	if err := watchUSB(filepath.Join(t.TempDir(), "none"), wait, asleep); err == nil {
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

	d.setAsleep(true)
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

	// presses run nothing (key 0 opens a page) and ask the PC to wake once
	for range 2 {
		d.press(0, true)
		d.press(0, false)
	}
	if len(d.pages) != 1 || wakes != 1 {
		t.Errorf("asleep presses: pages %v, %d wake requests", d.pages, wakes)
	}

	d.setAsleep(false)
	if brightness() != "6" {
		t.Errorf("awake: brightness %q, want 6", brightness())
	}
	if !slices.Equal(d.canvas.Pix, page) {
		t.Error("awake: the page isn't drawn as before")
	}
}
