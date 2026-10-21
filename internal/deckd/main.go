// Package deckd runs the D200's keys: it shows the current page of buttons
// on the screen and carries out a button's action when its key is pressed.
package deckd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/alive"
	"github.com/riptidewave93/d200-deck/internal/drm"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/internal/input"
	"github.com/riptidewave93/d200-deck/internal/ota"
	"github.com/riptidewave93/d200-deck/internal/screen"
	"github.com/riptidewave93/d200-deck/internal/serial"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/render"
)

// held is a pressed key: the page it was pressed on and its action, so the
// release goes to the same action even if the press changed the page.
type held struct {
	page   string
	action deck.Action
	pcID   uint32 // the PC action sent, if any
}

type deckd struct {
	test   bool
	cfg    *deck.Config
	pages  []string // page stack; the last one is shown
	held   [deck.KeyCount]*held
	canvas *gfx.Canvas
	scan   *screen.Scanout // the panel; nil without one
	run    *runner
	// redraw asks the main loop to redraw a key, e.g. after an error flash
	redraw chan int
	// ui runs functions on the main loop, for the config channel
	ui       chan func()
	ch       channel
	pc       pcActions
	renderer *render.Renderer
	// overlay, if set, is shown instead of the pages (e.g. an update)
	overlay *overlay
	// file is the config file shown, and upload one being written; the
	// config channel's loop owns both
	fileMu sync.Mutex // for file and upload, also used by ask
	file   configFile
	upload *upload
	// asking is set while the device asks about a change (see ask)
	asking atomic.Bool
	clock  clock
	// state is how each key was drawn last, and frameAt when it changes
	// next, if it's animated; frames fires then
	state   [deck.KeyCount]render.State
	frameAt [deck.KeyCount]time.Time
	// white is when the screen stops showing white (flashAll); keys aren't
	// drawn until then
	white  time.Time
	frames *time.Timer
	// reply sends a reply later; nil sends it on ch
	reply func(seq uint16, status byte, b []byte)
	// keys queues key events for the config channel; nil sends none
	keys chan proto.KeyEvent
	// asleep is set while the PC hasn't configured the USB device, suspended
	// while the bus is suspended (see sleep.go)
	asleep    bool
	suspended bool
	askedWake bool
	light     *backlight // nil without one
	sleep     sleepScreen
	host      hostOS
	power     screenPower // nil without a display
	screenOff bool        // turned off for the PC's sleep
	// offset lines the picture up with the key windows; shownOffset is
	// the one on screen (see align.go)
	offset      screenOffset
	shownOffset image.Point
	pattern     alignPattern
	// editing is set while an editor selects keys by pressing them: keys
	// run nothing
	editing lease
}

func (d *deckd) page() string { return d.pages[len(d.pages)-1] }

func (d *deckd) show(r gfx.Rect) {
	if d.scan != nil {
		screen.Copy(*d.scan, d.canvas, r, d.shownOffset)
	}
}

func (d *deckd) drawKey(i int, state render.State) {
	if time.Now().Before(d.white) {
		d.state[i], d.frameAt[i] = state, time.Time{} // drawn when it ends
		return
	}
	r := render.Key(i)
	var next time.Time // when an animated key changes
	if d.asleep {
		drawSleepKey(d.canvas, i, r)
	} else if d.pattern.shown {
		drawTestKey(d.canvas, i, r, false, patternBand)
	} else if d.test {
		drawTestKey(d.canvas, i, r, state != render.Idle, 8)
	} else if d.overlay != nil {
		if draw, ok := d.overlay.drawn[i]; ok {
			draw(d.canvas, r)
		} else {
			b, ok := d.overlay.keys[i]
			d.renderer.Draw(d.canvas, r, b, ok, state)
		}
	} else {
		b, ok := d.cfg.Pages[d.page()].Keys[i]
		st := b.Action.State()
		b = b.Shown(st != "" && d.pc.getState(st))
		var turn time.Time
		if l, ok := d.pc.look(st); ok {
			b, turn = d.withLook(b, st, l, time.Now())
		}
		d.renderer.Draw(d.canvas, r, b, ok, state)
		next = d.renderer.Next()
		if !turn.IsZero() && (next.IsZero() || turn.Before(next)) {
			next = turn
		}
	}
	d.show(r)
	d.state[i], d.frameAt[i] = state, next
	d.scheduleFrames()
}

// scheduleFrames sets frames to fire when the next animated key changes.
func (d *deckd) scheduleFrames() {
	var soonest time.Time
	for _, t := range d.frameAt {
		if !t.IsZero() && (soonest.IsZero() || t.Before(soonest)) {
			soonest = t
		}
	}
	if d.frames == nil {
		d.frames = time.NewTimer(time.Hour)
	}
	d.frames.Stop()
	if !soonest.IsZero() {
		d.frames.Reset(max(time.Until(soonest), time.Millisecond))
	}
}

// nextFrames redraws the animated keys due a new frame.
func (d *deckd) nextFrames() {
	now := time.Now()
	for i, t := range d.frameAt {
		if !t.IsZero() && !t.After(now) {
			d.drawKey(i, d.state[i])
		}
	}
}

// flashError shows key i in the error colour for a moment.
func (d *deckd) flashError(i int) {
	d.drawKey(i, render.Error)
	time.AfterFunc(400*time.Millisecond, func() { d.redraw <- i })
}

// flash highlights key i for a moment, to identify it.
func (d *deckd) flash(i int) {
	d.drawKey(i, render.Identify)
	time.AfterFunc(time.Second, func() { d.redraw <- i })
}

// flashAll shows a white screen for a second, to tell this device from
// others.
func (d *deckd) flashAll() {
	r := gfx.Rect{Max: image.Pt(d.canvas.W, d.canvas.H)}
	d.canvas.Fill(r, 0xffffff)
	d.show(r)
	d.white = time.Now().Add(time.Second)
	time.AfterFunc(time.Second, func() {
		d.ui <- func() {
			if !time.Now().Before(d.white) { // not flashed again since
				d.redrawAll()
			}
		}
	})
}

// redrawAll draws the whole screen afresh: the background between the keys,
// then every key as it is.
func (d *deckd) redrawAll() {
	r := gfx.Rect{Max: image.Pt(d.canvas.W, d.canvas.H)}
	d.canvas.Fill(r, 0)
	for i := range deck.KeyCount {
		d.drawKey(i, d.state[i])
	}
	d.show(r)
}

func (d *deckd) drawPage() {
	d.renderer.NewPage()
	for i := range deck.KeyCount {
		d.drawKey(i, render.Idle)
	}
}

func (d *deckd) setPages(pages []string) {
	d.pages = pages
	d.renderer.SetConfig(d.cfg)
	d.drawPage()
}

func (d *deckd) press(i int, down bool) {
	if !down {
		h := d.held[i]
		if h == nil {
			return
		}
		d.held[i] = nil
		d.run.release(i, h.action)
		if h.action.Type.OnPC() {
			d.releasePC(i, h.action, h.pcID)
		}
		if h.page == d.page() {
			d.drawKey(i, render.Idle)
		}
		return
	}
	if d.asleep {
		d.pressAsleep()
		return
	}
	if d.pattern.shown {
		return
	}
	if d.overlay != nil {
		if d.overlay.press != nil && time.Since(d.overlay.shown) >= pressDelay {
			d.overlay.press(i)
		}
		return
	}

	b, ok := d.cfg.Pages[d.page()].Keys[i]
	if d.test || !ok || d.editing.on.Load() {
		d.held[i] = &held{page: d.page()}
		d.drawKey(i, render.Pressed)
		return
	}
	a := b.Action
	if a.Type.OnPC() && !d.pc.ready(a) {
		// e.g. Discord's keybind while deckhost can't reach Discord; the
		// release goes the same way as the press
		if f := a.FallbackAction(); f != nil {
			a = *f
		}
	}
	if d.host.mac() {
		a = a.ForMac()
	}
	d.held[i] = &held{page: d.page(), action: a}
	d.drawKey(i, render.Pressed)

	switch {
	case a.Type == deck.ActionPage:
		// a page already on the stack is gone back to, so pages that link to
		// each other don't grow it
		if i := slices.Index(d.pages, a.Page); i >= 0 {
			d.setPages(d.pages[:i+1])
		} else {
			d.setPages(append(d.pages, a.Page))
		}
	case a.Type == deck.ActionBack:
		if len(d.pages) > 1 {
			d.setPages(d.pages[:len(d.pages)-1])
		}
	case a.Type == deck.ActionHome:
		d.setPages([]string{d.cfg.Home})
	case a.Type.OnPC():
		d.held[i].pcID = d.sendPC(i, a)
	default:
		d.run.press(i, a)
	}
}

// startsFile counts deckd's recent starts. /run is emptied at boot.
var startsFile = "/run/deckd.starts"

const (
	maxQuickStarts = 3 // starts in a row that didn't last stableAfter
	stableAfter    = 30 * time.Second
)

// guardConfig makes the config's directory and counts this start. If deckd
// keeps dying soon after starting, the config may be what kills it: it's
// moved to path.bad, so deckd starts with the default config and can still
// be reached to fix it.
func guardConfig(path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Print(err) // the default config it is
	}
	b, _ := os.ReadFile(startsFile)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if n >= maxQuickStarts {
		if err := os.Rename(path, path+".bad"); err == nil {
			log.Printf("deckd keeps crashing: moved %s to %s.bad", path, path)
		}
		n = 0
	}
	if err := os.WriteFile(startsFile, []byte(strconv.Itoa(n+1)), 0o644); err != nil {
		log.Print(err)
	}
	time.AfterFunc(stableAfter, func() { os.Remove(startsFile) })
}

// loadConfig reads the config file, falling back to the default config when
// it is missing or broken. It returns the config and, for the default, its
// JSON.
func loadConfig(path string) (*deck.Config, []byte) {
	b, err := os.ReadFile(path)
	var c *deck.Config
	if err == nil {
		c, err = parseConfig(b)
	}
	switch {
	case err == nil:
		return c, nil
	case errors.Is(err, fs.ErrNotExist):
		log.Printf("no %s, using the default config", path)
	default:
		log.Printf("bad config, using the default: %v", err)
	}
	c = deck.DefaultConfig()
	b, _ = json.MarshalIndent(c, "", "  ")
	return c, append(b, '\n')
}

// parseConfig reads a config deckd can show. Keys whose icons it lacks show
// none; commit refuses those.
func parseConfig(b []byte) (*deck.Config, error) { return deck.Parse(b) }

// Command runs one of deckd's own commands, such as "fonts", reporting
// whether name is one.
func Command(name string) bool {
	switch name {
	case "hid-descriptor": // for the USB gadget setup
		os.Stdout.Write(hidg.ReportDescriptor)
	case "raw-descriptor":
		os.Stdout.Write(proto.RawDescriptor)
	case "raw-report-length": // the input report size, for the gadget's report_length
		fmt.Println(proto.RawInSize)
	case "usb-net": // for the USB gadget setup
		out, err := usbNet(serial.Path, usbConf)
		if err != nil {
			log.Fatal(err)
		}
		os.Stdout.WriteString(out)
	case "fonts":
		for _, n := range gfx.FontNames() {
			fmt.Println(n)
		}
	case "default-config":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(deck.DefaultConfig()); err != nil {
			log.Fatal(err)
		}
	default:
		return false
	}
	return true
}

// Main runs the keys with the given flags.
func Main(args []string) {
	fs := flag.NewFlagSet("keys", flag.ExitOnError)

	config := fs.String("config", "/data/deckd/config.json", "config file; the default config is used if it is missing")
	preview := fs.String("preview", "", "render the screen to this PNG file and exit")
	card := fs.String("card", "/dev/dri/card0", "DRM device")
	keys := fs.String("keys", "", "key event device (default: find the keypad)")
	hid := fs.String("hid", "/dev/hidg0", "USB HID gadget device")
	channel := fs.String("channel", "/dev/hidg1", "config channel gadget device")
	test := fs.Bool("test", false, "show numbered key windows on a 100 px grid; keys send nothing")
	fs.Parse(args)

	// The D200 has little RAM: keep the Go heap small.
	debug.SetMemoryLimit(30 << 20) // animated GIFs' frames take up to 8 MiB

	if !*test && *preview == "" {
		guardConfig(*config)
	}
	cfg, mem := loadConfig(*config)
	d := &deckd{cfg: cfg, canvas: gfx.NewCanvas(render.Width, render.Height),
		test: *test, redraw: make(chan int, deck.KeyCount), ui: make(chan func(), 4),
		file: configFile{path: *config}}
	d.file.mu = &d.fileMu
	if err := d.file.set(cfg, mem, "", nil); err != nil {
		log.Fatal(err) // it was there a moment ago
	}
	d.pages = []string{d.cfg.Home}
	d.renderer = render.New(d.cfg)
	d.renderer.Extra = systemIcons
	d.pc.fail = func(key int) { d.ui <- func() { d.flashError(key) } }
	d.pc.changed = func() { d.ui <- d.drawPage }
	if d.test {
		drawGrid(d.canvas)
	}

	if *preview != "" {
		d.drawPage()
		if err := savePNG(*preview, d.canvas); err != nil {
			log.Fatal(err)
		}
		return
	}

	disp, err := drm.Open(*card)
	if err != nil {
		log.Fatal(err)
	}
	defer disp.Close()
	d.scan = &screen.Scanout{Width: disp.Width, Height: disp.Height, Stride: disp.Stride, Pixels: disp.Pixels}
	d.power = disp
	d.offset.load(filepath.Join(filepath.Dir(*config), "screen_offset"))
	d.shownOffset = d.offset.get()
	d.light = newBacklight(filepath.Join(filepath.Dir(*config), "brightness"))
	d.sleep.load(filepath.Join(filepath.Dir(*config), "sleep_screen"))
	d.host.load(filepath.Join(filepath.Dir(*config), "host_os"))
	d.show(gfx.Rect{Max: image.Pt(render.Width, render.Height)})
	d.drawPage()
	if !d.test {
		writeReady(ota.ReadyFile)
		go d.serveUpdates(ota.UISocket)
	}

	if d.test {
		*hid = ""
	}
	d.run = newRunner(*hid)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	if !d.test {
		d.keys = make(chan proto.KeyEvent, 32)
		go d.sendKeys()
		go d.serveChannel(*channel)
	}

	if *keys == "" {
		*keys = input.Find()
	}
	if *keys == "" {
		log.Fatal("no key input device found")
	}
	events := make(chan input.Event)
	go func() { log.Fatal(input.Read(*keys, events)) }()
	usbState := make(chan string)
	if !d.test {
		go watchSleep(usbState)
	}
	beat := time.NewTicker(alive.Period)
	alive.Beat()
	for {
		select {
		case <-beat.C:
			alive.Beat()
		case ev := <-events:
			d.sendKey(ev.Key, ev.Pressed)
			d.press(ev.Key, ev.Pressed)
		case f := <-d.ui:
			f()
		case s := <-usbState:
			d.setUSB(s)
		case s := <-stop:
			d.run.stop(time.Second)
			d.light.flush()
			d.offset.flush()
			log.Printf("%v, exiting", s)
			os.Remove(startsFile) // stopped, not crashed
			return
		case <-d.frames.C:
			d.nextFrames()
		case i := <-d.redraw:
			state := render.Idle
			if d.held[i] != nil {
				state = render.Pressed
			}
			d.drawKey(i, state)
		}
	}
}

func savePNG(path string, c *gfx.Canvas) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, c.Image()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
