package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/internal/screen"
	"github.com/riptidewave93/d200-deck/proto"
)

func newTestDeckd() *deckd {
	d := &deckd{cfg: deck.DefaultConfig(), canvas: gfx.NewCanvas(screen.Width, screen.Height),
		redraw: make(chan int, deck.KeyCount), ui: make(chan func(), 16)}
	d.pages = []string{d.cfg.Home}
	d.run = newRunner(nil)
	d.pc.fail = func(key int) { d.ui <- func() { d.flashError(key) } }
	return d
}

// keyColour is the background colour key i is drawn in.
func keyColour(d *deckd, i int) uint32 {
	r := screen.Key(i)
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
}

func TestPressColours(t *testing.T) {
	d := newTestDeckd()
	d.drawPage()
	if got := keyColour(d, 1); got != keyColours[keyIdle] {
		t.Errorf("idle key colour %#x", got)
	}
	d.press(1, true)
	if got := keyColour(d, 1); got != keyColours[keyPressed] {
		t.Errorf("pressed key colour %#x", got)
	}
	d.press(1, false)
	if got := keyColour(d, 1); got != keyColours[keyIdle] {
		t.Errorf("released key colour %#x", got)
	}
	d.press(12, true) // blank key
	if got := keyColour(d, 12); got != 0 {
		t.Errorf("blank key colour %#x", got)
	}
}

func TestFlash(t *testing.T) {
	d := newTestDeckd()
	d.flash(2)
	if got := keyColour(d, 2); got != keyColours[keyIdentify] {
		t.Errorf("identify colour %#x", got)
	}
	d.flashError(3)
	if got := keyColour(d, 3); got != keyColours[keyError] {
		t.Errorf("error colour %#x", got)
	}
	select {
	case i := <-d.redraw:
		if i != 3 {
			t.Errorf("redraw of key %d", i)
		}
	case <-time.After(2 * time.Second):
		t.Error("error flash never ends")
	}
}

func TestPCActionWithoutHost(t *testing.T) {
	d := newTestDeckd()
	d.press(13, true) // Source Repo: open-url
	if got := keyColour(d, 13); got != keyColours[keyError] {
		t.Errorf("no red flash without deckhost: %#x", got)
	}
}

func TestPCActionEvent(t *testing.T) {
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDeckd()
	d.ch.set(raw)
	defer d.ch.set(nil)
	d.pc.heartbeat()
	d.press(13, true)

	var msg []byte
	buf := make([]byte, 4096)
	r.SetReadDeadline(time.Now().Add(2 * time.Second))
	for done := false; !done; {
		n, err := r.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		for b := buf[:n]; len(b) >= proto.RawInSize; b = b[proto.RawInSize:] {
			part, err := proto.DecodeReply(b[:proto.RawInSize])
			if err != nil || part.Seq != proto.EventSeq || part.Status != proto.EventAction {
				t.Fatalf("report %+v, %v", part, err)
			}
			msg = append(msg, part.Payload...)
			done = !part.More
		}
	}
	var ev proto.ActionEvent
	if err := json.Unmarshal(msg, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Key != 13 || ev.Action.URL != deck.RepoURL {
		t.Errorf("event %+v", ev)
	}
	// a failed result flashes the key
	d.handle(proto.Request{Cmd: proto.CmdResult, Payload: []byte(`{"id":` + itoa(ev.ID) + `,"error":"no browser"}`)})
	runUI(t, d)
	if got := keyColour(d, 13); got != keyColours[keyError] {
		t.Errorf("no red flash after a failed result: %#x", got)
	}
}

func itoa(n uint32) string { b, _ := json.Marshal(n); return string(b) }

func TestPCActionSendFails(t *testing.T) {
	d := newTestDeckd()
	d.pc.heartbeat()
	d.press(13, true) // channel closed: the send fails
	runUI(t, d)
	if got := keyColour(d, 13); got != keyColours[keyError] {
		t.Errorf("no red flash: %#x", got)
	}
}

func TestPCActions(t *testing.T) {
	var failed []int
	p := pcActions{fail: func(k int) { failed = append(failed, k) }}
	if p.hostAlive() {
		t.Error("host alive before any heartbeat")
	}
	p.heartbeat()
	if !p.hostAlive() {
		t.Error("host not alive after a heartbeat")
	}
	ok := p.start(1)
	p.result(proto.Result{ID: ok})
	bad := p.start(2)
	p.result(proto.Result{ID: bad, Error: "boom"})
	p.result(proto.Result{ID: bad, Error: "again"}) // already finished
	p.result(proto.Result{ID: 999, Error: "unknown"})
	if len(failed) != 1 || failed[0] != 2 {
		t.Errorf("failed keys %v", failed)
	}

	old := pcActionTimeout
	pcActionTimeout = 10 * time.Millisecond
	defer func() { pcActionTimeout = old }()
	done := make(chan int, 1)
	p.fail = func(k int) { done <- k }
	p.start(7)
	select {
	case k := <-done:
		if k != 7 {
			t.Errorf("timed out key %d", k)
		}
	case <-time.After(2 * time.Second):
		t.Error("no timeout")
	}
}

func TestHandle(t *testing.T) {
	d := newTestDeckd()
	status, b := d.handle(proto.Request{Cmd: proto.CmdHello})
	var h proto.Hello
	if status != proto.StatusOK || json.Unmarshal(b, &h) != nil || h.Protocol != proto.Version || h.Keys != deck.KeyCount {
		t.Errorf("hello: %d %s", status, b)
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdIdentify, Payload: []byte{deck.KeyCount}}); s != proto.StatusBadRequest {
		t.Errorf("identify of a missing key: %d", s)
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdIdentify, Payload: []byte{4}}); s != proto.StatusOK {
		t.Errorf("identify: %d", s)
	}
	runUI(t, d)
	if got := keyColour(d, 4); got != keyColours[keyIdentify] {
		t.Errorf("identify colour %#x", got)
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdActionHost}); s != proto.StatusOK || !d.pc.hostAlive() {
		t.Error("action-host heartbeat not taken")
	}
	if s, _ := d.handle(proto.Request{Cmd: proto.CmdResult, Payload: []byte("{")}); s != proto.StatusBadRequest {
		t.Errorf("bad result: %d", s)
	}
	if s, _ := d.handle(proto.Request{Cmd: 0x7f}); s != proto.StatusUnknownCmd {
		t.Errorf("unknown command: %d", s)
	}
}

func TestChannelSend(t *testing.T) {
	var c channel
	if c.send(1, 0, nil) == nil {
		t.Error("sent on a closed channel")
	}
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	c.set(raw)
	defer c.set(nil)
	if err := c.send(5, proto.StatusOK, bytes.Repeat([]byte("x"), 100)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if n != 2*proto.RawInSize {
		t.Errorf("%d bytes written, want 2 reports", n)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if c := loadConfig(filepath.Join(dir, "missing.json")); c.Home != "main" {
		t.Error("missing config didn't fall back to the default")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{"), 0o644)
	if c := loadConfig(bad); c.Home != "main" {
		t.Error("broken config didn't fall back to the default")
	}
	custom := &deck.Config{Version: deck.ConfigVersion, Home: "only", Pages: map[string]deck.Page{
		"only": {Keys: map[int]deck.Button{0: {Icon: "mute", Label: "Mute"}}},
	}}
	good := filepath.Join(dir, "good.json")
	b, _ := json.Marshal(custom)
	os.WriteFile(good, b, 0o644)
	if c := loadConfig(good); c.Home != "only" {
		t.Error("good config not loaded")
	}
	custom.Pages["only"].Keys[0] = deck.Button{Icon: "no-such-icon"}
	b, _ = json.Marshal(custom)
	os.WriteFile(good, b, 0o644)
	if c := loadConfig(good); c.Home != "main" {
		t.Error("config with an unknown icon was loaded")
	}
}

func TestDrawButton(t *testing.T) {
	icon := deck.Button{Icon: "mute"}
	label := deck.Button{Label: "Label only"}
	both := deck.Button{Icon: "github", Label: "Source Repo"}
	cases := []struct {
		name string
		key  int
		b    deck.Button
	}{
		{"icon", 0, icon}, {"label", 1, label}, {"icon and label", 2, both}, {"wide", 13, both},
	}
	for _, tc := range cases {
		c := gfx.NewCanvas(screen.Width, screen.Height)
		r := screen.Key(tc.key)
		drawButton(c, r, tc.b, true, keyIdle)
		fg := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if c.Pix[y*c.W+x] == fgColour {
					fg++
				}
			}
		}
		if fg < 50 {
			t.Errorf("%s: %d foreground pixels", tc.name, fg)
		}
	}
	c := gfx.NewCanvas(screen.Width, screen.Height)
	c.Fill(image.Rect(0, 0, screen.Width, screen.Height), 0x123456)
	drawButton(c, screen.Key(5), deck.Button{}, false, keyIdle)
	if c.Pix[screen.Key(5).Min.Y*c.W+screen.Key(5).Min.X] != 0 {
		t.Error("blank key not cleared")
	}
}

func TestFitText(t *testing.T) {
	if px := fitText("Hi", 1000, 36); px != 36 {
		t.Errorf("short text shrunk to %d", px)
	}
	long := "A label far too long for one key"
	px := fitText(long, 180, 36)
	if px >= 36 || (px > 12 && gfx.TextWidth(long, px) > 180) {
		t.Errorf("long text at %d px is %d wide", px, gfx.TextWidth(long, px))
	}
}

func TestTestPattern(t *testing.T) {
	d := newTestDeckd()
	d.test = true
	drawGrid(d.canvas)
	d.drawPage()
	d.press(3, true)
	r := screen.Key(3)
	if got := d.canvas.Pix[(r.Min.Y+40)*d.canvas.W+r.Min.X+40]; got != 0xffffff {
		t.Errorf("pressed test key colour %#x", got)
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
