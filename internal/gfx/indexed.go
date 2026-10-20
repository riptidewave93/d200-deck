package gfx

import "image"

// Indexed is a picture as indexes into a palette, a byte a pixel: how
// animated GIFs' frames are kept.
type Indexed struct {
	W, H    int
	Pix     []uint8
	Palette *[256]uint32 // premultiplied: alpha<<24 | r<<16 | g<<8 | b
}

// Bounds is the picture's size, at (0, 0).
func (p *Indexed) Bounds() image.Rectangle { return image.Rect(0, 0, p.W, p.H) }

// DrawIndexed draws p centred in r, blended over the canvas by its alpha.
func (c *Canvas) DrawIndexed(p *Indexed, r Rect) {
	at := image.Pt(r.Min.X+(r.Dx()-p.W)/2, r.Min.Y+(r.Dy()-p.H)/2)
	dst := p.Bounds().Add(at).Intersect(image.Rect(0, 0, c.W, c.H))
	w := dst.Dx()
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		src := p.Pix[(y-at.Y)*p.W+dst.Min.X-at.X:][:w]
		out := c.Pix[y*c.W+dst.Min.X:][:w]
		for x, i := range src {
			v := p.Palette[i]
			switch a := v >> 24; a {
			case 0:
			case 255:
				out[x] = v & 0xffffff
			default:
				out[x] = over(v>>16&0xff, v>>8&0xff, v&0xff, a, out[x])
			}
		}
	}
}
