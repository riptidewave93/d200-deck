// Package screen knows the D200's screen: a 720x1280 panel mounted rotated,
// used as a 1280x720 landscape canvas, seen through 13 key windows.
package screen

import (
	"image"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
)

// Landscape canvas size.
const (
	Width  = 1280
	Height = 720
)

// keys are the visible key windows on the landscape canvas: a 5x3 grid of
// 196x196 windows, the wide key spanning the two bottom-right grid cells
// (458x196), so 14 keys. Positions are centred on the windows by eye with
// deckd -test.
var keys = func() (r [deck.KeyCount]gfx.Rect) {
	colX := [5]int{16, 275, 534, 793, 1052}
	rowY := [3]int{1, 262, 524}
	const size = 196
	for i := 0; i < 13; i++ {
		x, y := colX[i%5], rowY[i/5]
		r[i] = image.Rect(x, y, x+size, y+size)
	}
	r[13] = image.Rect(793, 523, 1251, 719)
	return r
}()

// Key returns the visible area of key i on the canvas.
func Key(i int) gfx.Rect { return keys[i] }

// Scanout is a portrait buffer the canvas is shown on: 0x00RRGGBB pixels,
// Stride pixels per row.
type Scanout struct {
	Width, Height, Stride int
	Pixels                []uint32
}

// Copy copies r of the canvas into the scanout buffer, rotated by 90 degrees
// (counter-clockwise panel mounting: canvas x runs bottom to top, canvas y
// left to right).
func Copy(dst Scanout, c *gfx.Canvas, r gfx.Rect) {
	r = r.Intersect(image.Rect(0, 0, c.W, c.H))
	r = r.Intersect(image.Rect(0, 0, dst.Height, dst.Width))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		line := c.Pix[y*c.W:]
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.Pixels[(dst.Height-1-x)*dst.Stride+y] = line[x]
		}
	}
}
