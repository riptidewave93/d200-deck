package deckd

import (
	"fmt"
	"image"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/screen"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/render"
)

// patternBand is the alignment pattern's band width (see drawTestKey).
const patternBand = 4

// overscan is how far the outer keys may move off the screen: the drawn
// keys are a little larger than the windows they show through.
const overscan = 2

// offsetMin and offsetMax are the offset's range: the space the key windows
// leave at the screen's edges, plus overscan.
var offsetMin, offsetMax = func() (lo, hi image.Point) {
	all := render.Key(0)
	for i := range deck.KeyCount {
		all = all.Union(render.Key(i))
	}
	lo = image.Pt(-all.Min.X-overscan, -all.Min.Y-overscan)
	hi = image.Pt(render.Width-all.Max.X+overscan, render.Height-all.Max.Y+overscan)
	return lo, hi
}()

func clampOffset(p image.Point) image.Point {
	return image.Pt(min(max(p.X, offsetMin.X), offsetMax.X), min(max(p.Y, offsetMin.Y), offsetMax.Y))
}

// screenOffset is the offset setting, kept in file and saved saveDelay after
// the last change.
type screenOffset struct {
	mu      sync.Mutex
	file    string
	p       image.Point
	unsaved bool
	save    *time.Timer
}

func (o *screenOffset) get() image.Point {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.p
}

// load reads file, "x y"; anything else is no offset.
func (o *screenOffset) load(file string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.file = file
	b, _ := os.ReadFile(file)
	var p image.Point
	if _, err := fmt.Sscan(string(b), &p.X, &p.Y); err == nil {
		o.p = clampOffset(p)
	}
}

// set clamps p, takes it and saves it soon; false if it's unchanged.
func (o *screenOffset) set(p image.Point) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	p = clampOffset(p)
	if p == o.p {
		return false
	}
	o.p, o.unsaved = p, true
	if o.save == nil {
		o.save = time.AfterFunc(saveDelay, o.flush)
	} else {
		o.save.Reset(saveDelay)
	}
	return true
}

// flush saves the offset if it changed.
func (o *screenOffset) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.unsaved {
		return
	}
	o.unsaved = false
	if err := saveSetting(o.file, fmt.Sprintf("%d %d", o.p.X, o.p.Y)); err != nil {
		log.Printf("screen offset: %v", err)
	}
}

// alignPattern is whether the alignment pattern is wanted, and its timeout.
type alignPattern struct {
	lease
	shown bool // main loop only
}

// patternTimeout ends a pattern the site forgot.
var patternTimeout = proto.PatternTimeout

// keep turns the pattern on or off; end runs when it times out.
func (a *alignPattern) keep(on bool, end func()) { a.lease.keep(on, patternTimeout, end) }

// lease is a flag a client keeps on by setting it again within a timeout,
// so it ends if the client goes away.
type lease struct {
	on  atomic.Bool
	mu  sync.Mutex
	end *time.Timer
}

// keep turns l on or off, and restarts its timeout; ended, if set, runs
// when it times out.
func (l *lease) keep(on bool, timeout time.Duration, ended func()) {
	l.on.Store(on)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.end != nil {
		l.end.Stop()
	}
	if on {
		l.end = time.AfterFunc(timeout, func() {
			l.on.Store(false)
			if ended != nil {
				ended()
			}
		})
	}
}

// align takes the alignment settings in s. A new offset keeps the pattern
// on.
func (d *deckd) align(s proto.Settings) {
	if s.ScreenOffset != nil && d.offset.set(image.Pt(s.ScreenOffset.X, s.ScreenOffset.Y)) {
		d.ui <- d.showOffset
	}
	on := d.pattern.on.Load()
	if s.ScreenPattern != nil {
		on = *s.ScreenPattern
	} else if s.ScreenOffset == nil {
		return
	}
	d.pattern.keep(on, func() { d.ui <- d.showPattern })
	d.ui <- d.showPattern
}

// showOffset shows the picture at the offset now set.
func (d *deckd) showOffset() {
	p := d.offset.get()
	if p == d.shownOffset || d.scan == nil {
		d.shownOffset = p
		return
	}
	d.shownOffset = p
	screen.Show(*d.scan, d.canvas, p)
}

// showPattern shows or ends the alignment pattern, as wanted. The PC's
// sleep ends it.
func (d *deckd) showPattern() {
	on := d.pattern.on.Load()
	if on && d.asleep {
		d.pattern.keep(false, nil)
		on = false
	}
	if on == d.pattern.shown {
		return
	}
	d.pattern.shown = on
	for i := range deck.KeyCount {
		d.drawKey(i, d.state[i])
	}
}
