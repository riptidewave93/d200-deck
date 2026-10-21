package deckd

import (
	"image"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/screen"
	"github.com/riptidewave93/d200-deck/render"
)

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
