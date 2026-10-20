package deckd

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/screen"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/render"
)

// alignDeckd is a test deckd with a panel, its offset saved in a temp dir.
func alignDeckd(t *testing.T) (*deckd, string) {
	usbConf = filepath.Join(t.TempDir(), "usb-gadget.conf")
	oldDelay, oldTimeout := saveDelay, patternTimeout
	t.Cleanup(func() { usbConf, saveDelay, patternTimeout = "/data/usb-gadget.conf", oldDelay, oldTimeout })
	saveDelay = 10 * time.Millisecond
	d := newTestDeckd()
	d.scan = &screen.Scanout{Width: render.Height, Height: render.Width, Stride: render.Height}
	d.scan.Pixels = make([]uint32, render.Width*render.Height)
	file := filepath.Join(t.TempDir(), "screen_offset")
	d.offset.load(file)
	d.drawPage()
	return d, file
}

// runQueued runs what the settings handler queued for the main loop.
func runQueued(d *deckd) {
	for {
		select {
		case f := <-d.ui:
			f()
		default:
			return
		}
	}
}

func alignSettings(t *testing.T, d *deckd, payload string) proto.Settings {
	t.Helper()
	status, reply := d.settings(1, []byte(payload))
	var s proto.Settings
	if status != proto.StatusOK || json.Unmarshal(reply, &s) != nil {
		t.Fatalf("%s: status %d %s", payload, status, reply)
	}
	runQueued(d)
	return s
}

// panel is the colour shown at canvas point p, on the panel.
func panel(d *deckd, p image.Point) uint32 {
	return d.scan.Pixels[(d.scan.Height-1-p.X)*d.scan.Stride+p.Y]
}

// The range lets every key window move overscan past the screen's edge, no
// further.
func TestOffsetRange(t *testing.T) {
	screenRect := image.Rect(0, 0, render.Width, render.Height)
	for _, off := range []image.Point{offsetMin, offsetMax, {offsetMin.X, offsetMax.Y}, {offsetMax.X, offsetMin.Y}} {
		for i := range deck.KeyCount {
			r := render.Key(i).Add(off)
			if !r.Inset(overscan).In(screenRect) {
				t.Errorf("offset %v: key %d at %v", off, i, r)
			}
		}
	}
	if offsetMin.X >= 0 || offsetMin.Y >= 0 || offsetMax.X <= 0 || offsetMax.Y <= 0 {
		t.Errorf("range %v-%v", offsetMin, offsetMax)
	}
	if got := clampOffset(image.Pt(-1000, 1000)); got != image.Pt(offsetMin.X, offsetMax.Y) {
		t.Errorf("clamped to %v", got)
	}
}

func TestScreenOffset(t *testing.T) {
	d, file := alignDeckd(t)
	if s := alignSettings(t, d, ""); s.ScreenOffset == nil || *s.ScreenOffset != (proto.Offset{}) ||
		*s.ScreenOffsetMin != (proto.Offset{X: offsetMin.X, Y: offsetMin.Y}) || *s.ScreenOffsetMax != (proto.Offset{X: offsetMax.X, Y: offsetMax.Y}) {
		t.Fatalf("defaults %+v", s)
	}
	key := render.Key(6).Min.Add(image.Pt(3, 3))
	colour := panel(d, key)
	if s := alignSettings(t, d, `{"screen_offset": {"x": 5, "y": -3}}`); *s.ScreenOffset != (proto.Offset{X: 5, Y: -3}) {
		t.Errorf("set: %+v", *s.ScreenOffset)
	}
	if got := panel(d, key.Add(image.Pt(5, -3))); got != colour {
		t.Errorf("key not moved: %#x, want %#x", got, colour)
	}
	// what's drawn later lands moved too
	d.drawKey(6, render.Pressed)
	if got := panel(d, key.Add(image.Pt(5, -3))); got != render.Colours[render.Pressed] {
		t.Errorf("redrawn key not moved: %#x", got)
	}
	if s := alignSettings(t, d, `{"screen_offset": {"x": 999, "y": -999}}`); *s.ScreenOffset != (proto.Offset{X: offsetMax.X, Y: offsetMin.Y}) {
		t.Errorf("not clamped: %+v", *s.ScreenOffset)
	}
	time.Sleep(50 * time.Millisecond)
	if b, _ := os.ReadFile(file); string(b) != "37 -9\n" {
		t.Errorf("saved %q", b)
	}
	var again screenOffset
	again.load(file)
	if again.get() != image.Pt(offsetMax.X, offsetMin.Y) {
		t.Errorf("not kept across a restart: %v", again.get())
	}
	// a device without a screen has none of it
	d.scan = nil
	if s := alignSettings(t, d, `{"screen_offset": {"x": 1, "y": 1}, "screen_pattern": true}`); s.ScreenOffset != nil || s.ScreenPattern != nil || s.ScreenOffsetMax != nil {
		t.Errorf("without a screen: %+v", s)
	}
}

func TestScreenPattern(t *testing.T) {
	d, _ := alignDeckd(t)
	edge := render.Key(0).Min                              // the outer white band
	inside := edge.Add(image.Pt(patternBand, patternBand)) // the black one in it
	isPattern := func() bool { return panel(d, edge) == 0xffffff && panel(d, inside) == 0 }
	if s := alignSettings(t, d, `{"screen_pattern": true}`); !*s.ScreenPattern || !isPattern() {
		t.Fatalf("pattern not shown: %+v", s)
	}
	// keys do nothing
	d.press(0, true)
	if d.held[0] != nil || d.page() != d.cfg.Home {
		t.Error("a key worked during the pattern")
	}
	d.press(0, false)
	// an offset moves the pattern too and keeps it on
	if alignSettings(t, d, `{"screen_offset": {"x": 2, "y": 2}}`); !d.pattern.shown || panel(d, edge.Add(image.Pt(2, 2))) != 0xffffff {
		t.Error("pattern not moved")
	}
	if s := alignSettings(t, d, `{"screen_pattern": false}`); *s.ScreenPattern || d.pattern.shown {
		t.Error("pattern not ended")
	}
	if panel(d, inside.Add(image.Pt(2, 2))) == 0 {
		t.Error("keys not drawn again")
	}

	// it ends by itself
	patternTimeout = 20 * time.Millisecond
	alignSettings(t, d, `{"screen_pattern": true}`)
	select {
	case f := <-d.ui:
		f()
	case <-time.After(time.Second):
		t.Fatal("pattern didn't time out")
	}
	if d.pattern.shown || d.pattern.on.Load() {
		t.Error("pattern still on after its timeout")
	}

	// and when the PC sleeps
	patternTimeout = time.Minute
	alignSettings(t, d, `{"screen_pattern": true}`)
	d.setAsleep(true)
	if d.pattern.shown || d.pattern.on.Load() {
		t.Error("pattern still on while the PC sleeps")
	}
	d.setAsleep(false)
	if d.pattern.shown {
		t.Error("pattern back after the PC woke")
	}
}
