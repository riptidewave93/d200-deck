package deckd

import (
	"encoding/json"
	"fmt"
	"image"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/ota"
)

// overlay is a screen shown instead of the config's pages, such as the
// update screen.
type overlay struct {
	keys  map[int]deck.Button
	drawn map[int]func(*gfx.Canvas, gfx.Rect) // keys drawn by hand
	press func(key int)                       // nil: presses do nothing
}

func (d *deckd) setOverlay(o *overlay) {
	d.overlay = o
	d.drawPage()
}

// confirmTimeout is how long the confirm screen waits for a key.
var confirmTimeout = 2 * time.Minute

const (
	red   = "#c62828"
	white = "#ffffff"

	greenRGB = 0x2e7d32
	amberRGB = 0xf9a825
	trackRGB = 0x2a2a2a
)

// The update screens. Confirm asks; installing only reports, so its keys
// don't look like buttons, and the Install key becomes the warning.
//
//	confirm:    . . Update?   . .        installing: . . Updating . .
//	            . . version   . .                    [ progress bar  ]
//	            Cancel .  .  [Install ]              . . .  [warning ]

// confirmScreen asks whether to install c; answer gets the choice.
func confirmScreen(c ota.ConfirmRequest, answer func(bool)) *overlay {
	o := &overlay{
		keys: map[int]deck.Button{
			2:  {Icon: "system_update_alt", Label: "Update?"},
			7:  {Label: c.Version + "\n" + c.Flavour},
			10: {Icon: "close", Label: "Cancel", Color: white, Background: red},
		},
		drawn: map[int]func(*gfx.Canvas, gfx.Rect){
			13: text("go-bold", greenRGB, 0xffffff, line{"Install", 56}, line{"Keep plugged in until done", 26}),
		},
		press: func(key int) {
			switch key {
			case 10:
				answer(false)
			case 13:
				answer(true)
			}
		},
	}
	if c.Older {
		delete(o.keys, 7)
		o.drawn[7] = text("go-bold", amberRGB, 0, line{c.Version, 30}, line{c.Flavour, 30}, line{"NOT NEWER", 30})
	}
	return o
}

// progressScreen shows title, and step under the bar, done of total steps
// through.
func progressScreen(title, step string, done, total int) *overlay {
	if title == ota.StepFailed {
		return &overlay{keys: map[int]deck.Button{
			2: {Icon: "error", Label: "Update failed", Color: white, Background: red},
			7: {Label: "Your device\nis unchanged"},
		}}
	}
	o := &overlay{
		keys: map[int]deck.Button{2: {Icon: "system_update_alt", Label: title}},
		drawn: map[int]func(*gfx.Canvas, gfx.Rect){
			13: text("go-bold", amberRGB, 0,
				line{"DON'T unplug the device", 40}, line{"or let your PC", 40}, line{"shut down or sleep", 40}),
		},
	}
	frac := 0.0
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	var caption func(*gfx.Canvas, gfx.Rect)
	if step != "" {
		caption = text("go-medium", 0, fgColour, line{step, 38}, line{fmt.Sprintf("%d of %d", done+1, total), 28})
	}
	for k := range 5 {
		o.drawn[5+k] = barTile(k, frac, nil)
	}
	o.drawn[7] = barTile(2, frac, caption)
	return o
}

// barTile draws key k of a progress bar across the middle row's 5 keys,
// frac of the way along, with an optional caption under it.
func barTile(k int, frac float64, caption func(*gfx.Canvas, gfx.Rect)) func(*gfx.Canvas, gfx.Rect) {
	return func(c *gfx.Canvas, r gfx.Rect) {
		c.Fill(r, 0)
		band := image.Rect(r.Min.X, r.Min.Y+44, r.Max.X, r.Min.Y+80)
		c.Fill(band, trackRGB)
		if caption != nil {
			caption(c, image.Rect(r.Min.X, band.Max.Y+8, r.Max.X, r.Max.Y-4))
		}
		fill := min(max(frac*5-float64(k), 0), 1)
		band.Max.X = band.Min.X + int(fill*float64(band.Dx()))
		c.Fill(band, greenRGB)
	}
}

type line struct {
	text string
	px   int
}

// underlined are the words text underlines.
var underlined = []string{"DON'T", "NOT"}

// text draws lines centred on a key filled with bg, shrunk together to fit,
// with the underlined words underlined.
func text(font string, bg, fg uint32, lines ...line) func(*gfx.Canvas, gfx.Rect) {
	return func(c *gfx.Canvas, r gfx.Rect) {
		c.Fill(r, bg)
		f, _ := gfx.BuiltinFont(font)
		scale, h := 1.0, 0
		for _, l := range lines {
			scale = min(scale, float64(fitText(f, l.text, r.Dx()-24, l.px))/float64(l.px))
		}
		for i := range lines {
			lines[i].px = int(float64(lines[i].px) * scale)
			h += lines[i].px * 5 / 4
		}
		y := r.Min.Y + (r.Dy()-h)/2
		for _, l := range lines {
			lineH := l.px * 5 / 4
			c.DrawText(f, l.text, image.Rect(r.Min.X, y, r.Max.X, y+lineH), l.px, fg)
			ascent, descent := f.Metrics(l.px)
			base := y + (lineH-ascent-descent)/2 + ascent
			thick := max(3, l.px/10)
			x := r.Min.X + (r.Dx()-f.TextWidth(l.text, l.px))/2
			for _, w := range underlined {
				if i := strings.Index(" "+l.text+" ", " "+w+" "); i >= 0 {
					x0 := x + f.TextWidth(l.text[:i], l.px)
					c.Fill(image.Rect(x0, base+thick, x0+f.TextWidth(w, l.px), base+2*thick), fg)
				}
			}
			y += lineH
		}
	}
}

// serveUpdates shows deckd update's requests, one connection at a time.
func (d *deckd) serveUpdates(path string) {
	os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("update screen: %v", err)
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		log.Printf("update screen: %v", err)
		l.Close()
		return
	}
	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("update screen: %v", err)
			return
		}
		d.updateSession(c)
	}
}

func (d *deckd) updateSession(c net.Conn) {
	defer c.Close()
	defer func() { d.ui <- func() { d.setOverlay(nil) } }()
	dec, enc := json.NewDecoder(c), json.NewEncoder(c)
	for {
		var req ota.UIRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		switch {
		case req.Confirm != nil:
			answer := make(chan bool, 1)
			screen := confirmScreen(*req.Confirm, func(ok bool) {
				select {
				case answer <- ok:
				default:
				}
			})
			d.ui <- func() { d.setOverlay(screen) }
			ok := false
			select {
			case ok = <-answer:
			case <-time.After(confirmTimeout):
			}
			log.Printf("update %s: install %v", req.Confirm.Version, ok)
			if enc.Encode(ota.UIReply{OK: ok}) != nil || !ok {
				return
			}
		case req.Progress != "":
			screen := progressScreen(req.Progress, req.Step, req.Done, req.Total)
			d.ui <- func() { d.setOverlay(screen) }
		}
	}
}

// writeReady tells deckd update that the keys are up.
func writeReady(path string) {
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		log.Print(err)
	}
}
