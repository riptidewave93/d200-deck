package gfx

import (
	"image"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

// Fit scales img to fit in a w x h box, keeping its aspect ratio.
func Fit(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	sw, sh := w, b.Dy()*w/b.Dx()
	if sh > h {
		sw, sh = b.Dx()*h/b.Dy(), h
	}
	sw, sh = max(sw, 1), max(sh, 1)
	if sw > b.Dx() || sh > b.Dy() {
		dst := image.NewRGBA(image.Rect(0, 0, sw, sh))
		xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		return dst
	}
	switch src := img.(type) {
	case *image.RGBA:
		return shrink(src.Pix, src.Stride, b, false, sw, sh)
	case *image.NRGBA:
		return shrink(src.Pix, src.Stride, b, true, sw, sh)
	}
	src := image.NewRGBA(b)
	draw.Draw(src, b, img, b.Min, draw.Src)
	return shrink(src.Pix, src.Stride, b, false, sw, sh)
}

// shrink scales an RGBA (or, if straight, NRGBA) image down to w x h,
// averaging the source pixels under each destination pixel: as good as a
// filter for shrinking, and much faster.
func shrink(pix []uint8, stride int, b image.Rectangle, straight bool, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sum := make([]uint64, 4*w) // per destination column: r, g, b, a, all x 255
	for y := range h {
		y0, y1 := y*b.Dy()/h, (y+1)*b.Dy()/h
		clear(sum)
		for sy := y0; sy < y1; sy++ {
			row := pix[sy*stride:]
			for x := range w {
				x0, x1 := x*b.Dx()/w, (x+1)*b.Dx()/w
				var r, g, bl, a uint64
				for i := 4 * x0; i < 4*x1; i += 4 {
					wt := uint64(255) // premultiplied already
					if straight {
						wt = uint64(row[i+3])
					}
					r += uint64(row[i]) * wt
					g += uint64(row[i+1]) * wt
					bl += uint64(row[i+2]) * wt
					a += uint64(row[i+3])
				}
				sum[4*x] += r
				sum[4*x+1] += g
				sum[4*x+2] += bl
				sum[4*x+3] += a * 255
			}
		}
		out := dst.Pix[y*dst.Stride:]
		for x := range w {
			n := 255 * uint64((x+1)*b.Dx()/w-x*b.Dx()/w) * uint64(y1-y0)
			for i := range 4 {
				out[4*x+i] = uint8((sum[4*x+i] + n/2) / n)
			}
		}
	}
	return dst
}

// DrawImage draws img centred in r, blended over the canvas by its alpha.
func (c *Canvas) DrawImage(img *image.RGBA, r Rect) {
	b := img.Bounds()
	at := image.Pt(r.Min.X+(r.Dx()-b.Dx())/2, r.Min.Y+(r.Dy()-b.Dy())/2)
	dst := b.Add(at).Intersect(image.Rect(0, 0, c.W, c.H))
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		for x := dst.Min.X; x < dst.Max.X; x++ {
			s := img.Pix[img.PixOffset(x-at.X, y-at.Y):]
			a := uint32(s[3])
			if a == 0 {
				continue
			}
			i := y*c.W + x
			d := c.Pix[i]
			// RGBA is premultiplied: out = src + dst * (1 - alpha)
			mix := func(src uint8, shift uint) uint32 {
				return (uint32(src) + (d>>shift&0xff)*(255-a)/255) << shift
			}
			c.Pix[i] = mix(s[0], 16) | mix(s[1], 8) | mix(s[2], 0)
		}
	}
}
