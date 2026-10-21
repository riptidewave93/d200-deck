package deckd

import (
	"testing"

	"github.com/riptidewave93/d200-deck/render"
)

func TestTestPattern(t *testing.T) {
	d := newTestDeckd()
	d.test = true
	drawGrid(d.canvas)
	d.drawPage()
	d.press(3, true)
	r := render.Key(3)
	if got := d.canvas.Pix[(r.Min.Y+40)*d.canvas.W+r.Min.X+40]; got != 0xffffff {
		t.Errorf("pressed test key colour %#x", got)
	}
}
