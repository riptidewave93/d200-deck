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
	"runtime/debug"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/drm"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/internal/input"
	"github.com/riptidewave93/d200-deck/internal/screen"
)

// held is a pressed key: the page it was pressed on and its action, so the
// release goes to the same action even if the press changed the page.
type held struct {
	page   string
	action deck.Action
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
	ui chan func()
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
		drawButton(d.canvas, r, b, ok, state)
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
		d.setPages(append(d.pages, a.Page))
	case a.Type == deck.ActionBack:
		if len(d.pages) > 1 {
			d.setPages(d.pages[:len(d.pages)-1])
		}
	case a.Type == deck.ActionHome:
		d.setPages([]string{d.cfg.Home})
	case a.Type.OnPC():
		log.Printf("key %d: %s needs the PC daemon", i, a.Type)
		d.flashError(i)
	default:
		d.run.press(i, a)
	}
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
			if _, ok := gfx.Icons[b.Icon]; b.Icon != "" && !ok {
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
			os.Stdout.Write(hidg.RawDescriptor)
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

	d := &deckd{cfg: loadConfig(*config), canvas: gfx.NewCanvas(screen.Width, screen.Height),
		test: *test, redraw: make(chan int, deck.KeyCount), ui: make(chan func(), 4)}
	d.pages = []string{d.cfg.Home}
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

	var dev *hidg.Device
	if !d.test {
		if dev, err = hidg.Open(*hid); err != nil {
			log.Printf("no HID gadget, keys won't send anything: %v", err)
		}
	}
	d.run = newRunner(dev)
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
