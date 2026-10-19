package gfx

import (
	"image"
	"strconv"
)

// 3x5 digit glyphs, one row per entry, bit 2 = left column.
var digits = [10][5]uint8{
	{7, 5, 5, 5, 7}, {2, 6, 2, 2, 7}, {7, 1, 7, 4, 7}, {7, 1, 7, 1, 7}, {5, 5, 7, 1, 1},
	{7, 4, 7, 1, 7}, {7, 4, 7, 5, 7}, {7, 1, 1, 1, 1}, {7, 5, 7, 5, 7}, {7, 5, 7, 1, 7},
}

// DrawNumber draws n centred in r, each glyph pixel scale x scale.
func (c *Canvas) DrawNumber(n int, r Rect, scale int, col uint32) {
	s := strconv.Itoa(n)
	w := (len(s)*4 - 1) * scale
	x0 := r.Min.X + (r.Dx()-w)/2
	y0 := r.Min.Y + (r.Dy()-5*scale)/2
	for i, ch := range s {
		g := digits[ch-'0']
		for row := 0; row < 5; row++ {
			for b := 0; b < 3; b++ {
				if g[row]&(4>>b) != 0 {
					x := x0 + (i*4+b)*scale
					y := y0 + row*scale
					c.Fill(image.Rect(x, y, x+scale, y+scale), col)
				}
			}
		}
	}
}
