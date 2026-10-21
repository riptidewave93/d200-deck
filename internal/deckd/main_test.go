package deckd

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/proto/prototest"
	"github.com/riptidewave93/d200-deck/render"
)

func newTestDeckd() *deckd {
	pressDelay = 0
	d := &deckd{cfg: deck.DefaultConfig(), canvas: gfx.NewCanvas(render.Width, render.Height),
		redraw: make(chan int, deck.KeyCount), ui: make(chan func(), 16)}
	d.pages = []string{d.cfg.Home}
	b, _ := json.Marshal(d.cfg)
	d.file.mu = &d.fileMu
	d.file.set(d.cfg, b, "", nil)
	d.renderer = render.New(d.cfg)
	d.renderer.Extra = systemIcons
	d.run = newRunner("")
	d.pc.fail = func(key int) { d.ui <- func() { d.flashError(key) } }
	return d
}

// serve answers requests from dev as the config channel does.
func serve(d *deckd, dev *prototest.Device) {
	d.reply = func(seq uint16, status byte, b []byte) { dev.Send(seq, status, b) }
	for req := range dev.Requests() {
		if status, reply := d.handle(req); status != statusLater {
			dev.Send(req.Seq, status, reply)
		}
	}
}

// keyColour is the background colour key i is drawn in.
func keyColour(d *deckd, i int) uint32 {
	r := render.Key(i)
	return d.canvas.Pix[(r.Min.Y+2)*d.canvas.W+r.Min.X+2]
}

// runUI runs the next function queued for the main loop.
func runUI(t *testing.T, d *deckd) {
	t.Helper()
	select {
	case f := <-d.ui:
		f()
	case <-time.After(2 * time.Second):
		t.Fatal("nothing queued for the main loop")
	}
}

// fifo returns a named pipe and its read end.
func fifo(t *testing.T) (string, *os.File) {
	p := filepath.Join(t.TempDir(), "gadget")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return p, r
}

func TestNavigation(t *testing.T) {
	d := newTestDeckd()
	d.press(0, true) // Browser folder
	d.press(0, false)
	if d.page() != "browser" {
		t.Fatalf("on page %q after the Browser key", d.page())
	}
	d.press(0, true) // Back
	d.press(0, false)
	if d.page() != "main" {
		t.Fatalf("on page %q after Back", d.page())
	}
	d.press(0, true)
	d.press(0, false)
	d.cfg.Pages["browser"].Keys[5] = deck.Button{Icon: "home", Action: deck.Action{Type: deck.ActionHome}}
	d.press(5, true)
	d.press(5, false)
	if len(d.pages) != 1 || d.page() != "main" {
		t.Fatalf("pages %v after Home", d.pages)
	}
	d.press(0, false) // release without a press
	d.setPages([]string{"main"})
	d.cfg.Pages["main"].Keys[11] = deck.Button{Action: deck.Action{Type: deck.ActionBack}}
	d.press(11, true) // Back on the home page stays there
	if d.page() != "main" {
		t.Error("Back left the home page")
	}
	d.press(11, false)

	// pages that link to each other don't grow the stack
	d.cfg.Pages["browser"].Keys[6] = deck.Button{Action: deck.Action{Type: deck.ActionPage, Page: "main"}}
	for range 5 {
		d.press(0, true) // to browser
		d.press(0, false)
		d.press(6, true) // to main
		d.press(6, false)
	}
	if len(d.pages) != 1 || d.page() != "main" {
		t.Errorf("pages %v", d.pages)
	}
}

func TestPressColours(t *testing.T) {
	d := newTestDeckd()
	d.drawPage()
	if got := keyColour(d, 1); got != render.Colours[render.Idle] {
		t.Errorf("idle key colour %#x", got)
	}
	d.press(1, true)
	if got := keyColour(d, 1); got != render.Colours[render.Pressed] {
		t.Errorf("pressed key colour %#x", got)
	}
	d.press(1, false)
	if got := keyColour(d, 1); got != render.Colours[render.Idle] {
		t.Errorf("released key colour %#x", got)
	}
	d.press(12, true) // blank key
	if got := keyColour(d, 12); got != 0 {
		t.Errorf("blank key colour %#x", got)
	}
}

func TestSavePNG(t *testing.T) {
	c := gfx.NewCanvas(4, 4)
	p := filepath.Join(t.TempDir(), "screen.png")
	if err := savePNG(p, c); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(p)
	defer f.Close()
	if img, err := png.Decode(f); err != nil || img.Bounds().Dx() != 4 {
		t.Errorf("bad PNG: %v", err)
	}
	if savePNG(filepath.Join(t.TempDir(), "no", "dir.png"), c) == nil {
		t.Error("saved into a missing directory")
	}
}

func TestCommand(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = out
	t.Cleanup(func() { os.Stdout = old })
	for _, name := range []string{"hid-descriptor", "raw-descriptor", "fonts", "default-config"} {
		if !Command(name) {
			t.Errorf("%s isn't a command", name)
		}
	}
	if Command("keys") || Command("nope") || Command("icons") {
		t.Error("not a command, but ran")
	}
	if fi, _ := out.Stat(); fi.Size() < 1000 {
		t.Errorf("only %d bytes printed", fi.Size())
	}
}
