// Package screen shows the landscape canvas (see render) on the device's
// portrait panel, which is mounted rotated.
package screen

import (
	"image"

	"github.com/riptidewave93/d200-deck/internal/gfx"
)

// Scanout is a portrait buffer the canvas is shown on: 0x00RRGGBB pixels,
// Stride pixels per row.
type Scanout struct {
	Width, Height, Stride int
	Pixels                []uint32
}

// Copy copies r of the canvas into the scanout buffer, moved by off and
// rotated by 90 degrees (counter-clockwise panel mounting: canvas x runs
// bottom to top, canvas y left to right).
func Copy(dst Scanout, c *gfx.Canvas, r gfx.Rect, off image.Point) {
	r = r.Intersect(image.Rect(0, 0, c.W, c.H))
	r = r.Intersect(image.Rect(0, 0, dst.Height, dst.Width).Sub(off))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		line := c.Pix[y*c.W:]
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.Pixels[(dst.Height-1-x-off.X)*dst.Stride+y+off.Y] = line[x]
		}
	}
}

// Show shows the whole canvas, moved by off, black around it.
func Show(dst Scanout, c *gfx.Canvas, off image.Point) {
	clear(dst.Pixels)
	Copy(dst, c, image.Rect(0, 0, c.W, c.H), off)
}
