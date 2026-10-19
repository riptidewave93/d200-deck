// Package gfx is a tiny software renderer: an XRGB8888 canvas, rectangles,
// and cached antialiased icon and text masks.
package gfx

import (
	"image"
	"image/color"
)

// Canvas is a W x H image, one 0x00RRGGBB value per pixel.
type Canvas struct {
	W, H int
	Pix  []uint32
}

// NewCanvas returns a black canvas.
func NewCanvas(w, h int) *Canvas {
	return &Canvas{W: w, H: h, Pix: make([]uint32, w*h)}
}

// Rect is a pixel rectangle, Max exclusive.
type Rect = image.Rectangle

// Fill paints r with col.
func (c *Canvas) Fill(r Rect, col uint32) {
	r = r.Intersect(image.Rect(0, 0, c.W, c.H))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := c.Pix[y*c.W+r.Min.X : y*c.W+r.Max.X]
		for i := range row {
			row[i] = col
		}
	}
}

// Pt is a 2D point.
type Pt struct{ X, Y float64 }

// DrawMask paints col through coverage mask m, placed with its origin at at.
func (c *Canvas) DrawMask(m *image.Alpha, at image.Point, col uint32) {
	dst := m.Bounds().Add(at).Intersect(image.Rect(0, 0, c.W, c.H))
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		row := m.Pix[(y-at.Y)*m.Stride:]
		for x := dst.Min.X; x < dst.Max.X; x++ {
			if a := int(row[x-at.X]); a != 0 {
				i := y*c.W + x
				c.Pix[i] = blend(c.Pix[i], col, a, 255)
			}
		}
	}
}

// blend mixes src over dst with coverage n/total.
func blend(dst, src uint32, n, total int) uint32 {
	if n == total {
		return src
	}
	mix := func(shift uint) uint32 {
		d, s := int(dst>>shift&0xff), int(src>>shift&0xff)
		return uint32(d+(s-d)*n/total) << shift
	}
	return mix(16) | mix(8) | mix(0)
}

// Image returns a copy of the canvas as an image, e.g. to save a PNG.
func (c *Canvas) Image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for i, p := range c.Pix {
		img.Set(i%c.W, i/c.W, color.RGBA{uint8(p >> 16), uint8(p >> 8), uint8(p), 0xff})
	}
	return img
}
