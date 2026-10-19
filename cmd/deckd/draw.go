package main

import (
	"image"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
)

type keyState int

const (
	keyIdle keyState = iota
	keyPressed
	keyError
	keyIdentify
)

// Colours (0x00RRGGBB).
var keyColours = [...]uint32{keyIdle: 0x1c1c1c, keyPressed: 0x2f6fde, keyError: 0xc62828, keyIdentify: 0xf9a825}

const fgColour = 0xe8e8e8

// drawButton draws button b (blank if !ok) into key area r.
func drawButton(c *gfx.Canvas, r gfx.Rect, b deck.Button, ok bool, state keyState) {
	if !ok {
		c.Fill(r, 0)
		return
	}
	c.Fill(r, keyColours[state])

	_, hasIcon := gfx.Icons[b.Icon]
	switch {
	case hasIcon && b.Label != "" && r.Dx() > r.Dy():
		// wide key: icon then label, centred together
		const iconSize, gap, margin = 120, 28, 24
		px := fitText(b.Label, r.Dx()-2*margin-iconSize-gap, 44)
		w := iconSize + gap + gfx.TextWidth(b.Label, px)
		x := r.Min.X + (r.Dx()-w)/2
		c.DrawIcon(b.Icon, image.Rect(x, r.Min.Y, x+iconSize, r.Max.Y), iconSize, fgColour)
		c.DrawText(b.Label, image.Rect(x+iconSize+gap, r.Min.Y, x+w, r.Max.Y), px, fgColour)
	case hasIcon && b.Label != "":
		ir := image.Rect(r.Min.X, r.Min.Y+12, r.Max.X, r.Max.Y-44)
		c.DrawIcon(b.Icon, ir, 100, fgColour)
		lr := image.Rect(r.Min.X+8, r.Max.Y-52, r.Max.X-8, r.Max.Y-8)
		c.DrawText(b.Label, lr, fitText(b.Label, lr.Dx(), 30), fgColour)
	case hasIcon:
		c.DrawIcon(b.Icon, r, 120, fgColour)
	case b.Label != "":
		c.DrawText(b.Label, r, fitText(b.Label, r.Dx()-16, 36), fgColour)
	}
}

// fitText is the largest text size up to px at which s fits in width.
func fitText(s string, width, px int) int {
	for px > 12 && gfx.TextWidth(s, px) > width {
		px -= 2
	}
	return px
}

// Test pattern key colours, one per key.
var testColours = [deck.KeyCount]uint32{
	0xe53935, 0xfb8c00, 0xfdd835, 0x43a047, 0x1e88e5,
	0x8e24aa, 0x00acc1, 0xd81b60, 0x7cb342, 0x5e35b1,
	0x6d4c41, 0x546e7a, 0xf4511e, 0x00897b,
}

// drawTestKey draws key i as a numbered box with 8 px white/black edge bands,
// to check the key window positions: count the bands visible on each side.
func drawTestKey(c *gfx.Canvas, i int, r gfx.Rect, pressed bool) {
	for k := 0; k < 4; k++ {
		c.Fill(r.Inset(k*8), []uint32{0xffffff, 0}[k%2])
	}
	bg := testColours[i]
	if pressed {
		bg = 0xffffff
	}
	c.Fill(r.Inset(32), bg)
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
