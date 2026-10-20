package gfx

import (
	"image"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

// Fit scales img to fit in a w x h box, keeping its aspect ratio.
func Fit(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	sw, sh := fitSize(b.Dx(), b.Dy(), w, h)
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

// fitSize is iw x ih scaled to fit w x h, keeping its aspect ratio.
func fitSize(iw, ih, w, h int) (int, int) {
	sw, sh := w, ih*w/iw
	if sh > h {
		sw, sh = iw*h/ih, h
	}
	return max(sw, 1), max(sh, 1)
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
	w := dst.Dx()
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		src := img.Pix[img.PixOffset(dst.Min.X-at.X, y-at.Y):][:4*w]
		out := c.Pix[y*c.W+dst.Min.X:][:w]
		for x := range out {
			s := src[4*x : 4*x+4 : 4*x+4]
			switch a := uint32(s[3]); a {
			case 0:
			case 255:
				out[x] = uint32(s[0])<<16 | uint32(s[1])<<8 | uint32(s[2])
			default:
				out[x] = over(uint32(s[0]), uint32(s[1]), uint32(s[2]), a, out[x])
			}
		}
	}
}

// over is premultiplied colour r, g, b with alpha a over canvas pixel d:
// d * (1 - alpha) + colour.
func over(r, g, b, a, d uint32) uint32 {
	na := 255 - a
	return (r+(d>>16&0xff)*na/255)<<16 | (g+(d>>8&0xff)*na/255)<<8 | (b + (d&0xff)*na/255)
}

// Picture is an *image.RGBA or an *Indexed.
type Picture interface{ Bounds() image.Rectangle }

// DrawPicture draws p centred in r, blended over the canvas by its alpha.
func (c *Canvas) DrawPicture(p Picture, r Rect) {
	switch p := p.(type) {
	case *image.RGBA:
		c.DrawImage(p, r)
	case *Indexed:
		c.DrawIndexed(p, r)
	}
}
