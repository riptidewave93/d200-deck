// deckd runs the D200's keys: it shows the current page of buttons on the
// screen and carries out a button's action when its key is pressed.
package main

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
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/drm"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/internal/input"
	"github.com/riptidewave93/d200-deck/internal/screen"
	"github.com/riptidewave93/d200-deck/proto"
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
	disp   *drm.Display
	run    *runner
	// redraw asks the main loop to redraw a key, e.g. after an error flash
	redraw chan int
	// ui runs functions on the main loop, for the config channel
	ui     chan func()
	ch     channel
	pc     pcActions
	assets assetCache
}

func (d *deckd) page() string { return d.pages[len(d.pages)-1] }

func (d *deckd) show(r gfx.Rect) {
	if d.disp != nil {
		screen.Copy(screen.Scanout{Width: d.disp.Width, Height: d.disp.Height,
			Stride: d.disp.Stride, Pixels: d.disp.Pixels}, d.canvas, r)
	}
}

func (d *deckd) drawKey(i int, state keyState) {
	r := screen.Key(i)
	if d.test {
		drawTestKey(d.canvas, i, r, state != keyIdle)
	} else {
		b, ok := d.cfg.Pages[d.page()].Keys[i]
		drawSafely(d.canvas, r, b, ok, state, &d.assets)
	}
	d.show(r)
}

// flashError shows key i in the error colour for a moment.
func (d *deckd) flashError(i int) {
	d.drawKey(i, keyError)
	time.AfterFunc(400*time.Millisecond, func() { d.redraw <- i })
}

// flash highlights key i for a moment, to identify it.
func (d *deckd) flash(i int) {
	d.drawKey(i, keyIdentify)
	time.AfterFunc(time.Second, func() { d.redraw <- i })
}

func (d *deckd) drawPage() {
	for i := range deck.KeyCount {
		d.drawKey(i, keyIdle)
	}
}

func (d *deckd) setPages(pages []string) {
	d.pages = pages
	d.assets.reset(d.cfg)
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
			d.drawKey(i, keyIdle)
		}
		return
	}

	b, ok := d.cfg.Pages[d.page()].Keys[i]
	if d.test || !ok {
		d.held[i] = &held{page: d.page()}
		d.drawKey(i, keyPressed)
		return
	}
	a := b.Action
	d.held[i] = &held{page: d.page(), action: a}
	d.drawKey(i, keyPressed)

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

// guardConfig counts this start. If deckd keeps dying soon after starting,
// the config may be what kills it: it's moved to path.bad, so deckd starts
// with the default config and can still be reached to fix it.
func guardConfig(path string) {
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
// it is missing or broken.
func loadConfig(path string) *deck.Config {
	c, err := deck.Load(path)
	if err == nil {
		err = checkIcons(c)
	}
	switch {
	case err == nil:
		return c
	case errors.Is(err, fs.ErrNotExist):
		log.Printf("no %s, using the default config", path)
	default:
		log.Printf("bad config, using the default: %v", err)
	}
	return deck.DefaultConfig()
}

func checkIcons(c *deck.Config) error {
	for name, p := range c.Pages {
		for k, b := range p.Keys {
			if b.Icon != "" && !gfx.HasIcon(b.Icon) {
				return fmt.Errorf("page %q key %d: unknown icon %q", name, k, b.Icon)
			}
		}
	}
	return nil
}

func main() {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "hid-descriptor": // for the USB gadget setup
			os.Stdout.Write(hidg.ReportDescriptor)
			return
		case "raw-descriptor":
			os.Stdout.Write(proto.RawDescriptor)
			return
		case "icons":
			for _, n := range gfx.IconNames() {
				fmt.Println(n)
			}
			return
		case "fonts":
			for _, n := range gfx.FontNames() {
				fmt.Println(n)
			}
			return
		case "default-config":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(deck.DefaultConfig()); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	config := flag.String("config", "/etc/deckd/config.json", "config file; the default config is used if it is missing")
	preview := flag.String("preview", "", "render the screen to this PNG file and exit")
	card := flag.String("card", "/dev/dri/card0", "DRM device")
	keys := flag.String("keys", "", "key event device (default: find the keypad)")
	hid := flag.String("hid", "/dev/hidg0", "USB HID gadget device")
	channel := flag.String("channel", "/dev/hidg1", "config channel gadget device")
	test := flag.Bool("test", false, "show numbered key windows on a 100 px grid; keys send nothing")
	flag.Parse()

	// The D200 has little RAM: keep the Go heap small.
	debug.SetMemoryLimit(16 << 20)

	if !*test && *preview == "" {
		guardConfig(*config)
	}
	d := &deckd{cfg: loadConfig(*config), canvas: gfx.NewCanvas(screen.Width, screen.Height),
		test: *test, redraw: make(chan int, deck.KeyCount), ui: make(chan func(), 4)}
	d.pages = []string{d.cfg.Home}
	d.assets.reset(d.cfg)
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
	d.disp = disp
	d.show(gfx.Rect{Max: image.Pt(screen.Width, screen.Height)})
	d.drawPage()

	if d.test {
		*hid = ""
	}
	d.run = newRunner(*hid)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	if !d.test {
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
	for {
		select {
		case ev := <-events:
			d.press(ev.Key, ev.Pressed)
		case f := <-d.ui:
			f()
		case s := <-stop:
			d.run.stop(time.Second)
			log.Printf("%v, exiting", s)
			os.Remove(startsFile) // stopped, not crashed
			return
		case i := <-d.redraw:
			state := keyIdle
			if d.held[i] != nil {
				state = keyPressed
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
