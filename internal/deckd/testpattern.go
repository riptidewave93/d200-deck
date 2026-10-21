package deckd

import (
	"image"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
)

// Test pattern key colours, one per key.
var testColours = [deck.KeyCount]uint32{
	0xe53935, 0xfb8c00, 0xfdd835, 0x43a047, 0x1e88e5,
	0x8e24aa, 0x00acc1, 0xd81b60, 0x7cb342, 0x5e35b1,
	0x6d4c41, 0x546e7a, 0xf4511e, 0x00897b,
}

// drawTestKey draws key i as a numbered box with 4 white/black edge bands,
// band px each, to check the key window positions: count the bands visible
// on each side.
func drawTestKey(c *gfx.Canvas, i int, r gfx.Rect, pressed bool, band int) {
	for k := 0; k < 4; k++ {
		c.Fill(r.Inset(k*band), []uint32{0xffffff, 0}[k%2])
	}
	bg := testColours[i]
	if pressed {
		bg = 0xffffff
	}
	c.Fill(r.Inset(4*band), bg)
	c.DrawNumber(i, r, 16, 0)
}

// drawGrid draws grey lines every 100 px, brighter every 500 px.
func drawGrid(c *gfx.Canvas) {
	for x := 0; x < c.W; x += 100 {
		col := uint32(0x404040)
		if x%500 == 0 {
			col = 0xa0a0a0
		}
		c.Fill(image.Rect(x, 0, x+2, c.H), col)
	}
	for y := 0; y < c.H; y += 100 {
		col := uint32(0x404040)
		if y%500 == 0 {
			col = 0xa0a0a0
		}
		c.Fill(image.Rect(0, y, c.W, y+2), col)
	}
}
