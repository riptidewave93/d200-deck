package gfx

import (
	"image"
	"image/draw"
	"image/gif"
	"time"
)

// Animation is an animated GIF's frames, composed and scaled to fit a box.
type Animation struct {
	Frames []Picture // *Indexed; *image.RGBA if a frame has more than 256 colours
	Delays []time.Duration
	total  time.Duration
}

// Frame delays: browsers show a GIF frame of 0 or 1 hundredths of a second
// for 100 ms, and none faster than minDelay.
const (
	minDelay   = 50 * time.Millisecond
	shortDelay = 100 * time.Millisecond
)

// NewAnimation composes g's frames, as its disposal methods say, and scales
// each to fit w x h. Frames keep the GIF's colours, a byte a pixel, scaled by
// nearest pixel; a GIF with a frame of more than 256 colours is kept in RGBA,
// scaled smoothly.
func NewAnimation(g *gif.GIF, w, h int) *Animation {
	a := &Animation{Frames: indexedFrames(g, w, h)}
	if a.Frames == nil {
		compose(g, func(c *image.RGBA) bool {
			a.Frames = append(a.Frames, Fit(c, w, h))
			return true
		})
	}
	for i := range g.Image {
		d := shortDelay
		if i < len(g.Delay) && g.Delay[i] > 1 {
			d = max(time.Duration(g.Delay[i])*10*time.Millisecond, minDelay)
		}
		a.Delays = append(a.Delays, d)
		a.total += d
	}
	return a
}

// compose calls f with each of g's frames drawn over what the frames before
// it left, as a GIF is shown, until f returns false.
func compose(g *gif.GIF, f func(*image.RGBA) bool) {
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	for i, fr := range g.Image {
		var disposal byte
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		var prev *image.RGBA
		if disposal == gif.DisposalPrevious {
			prev = image.NewRGBA(canvas.Bounds())
			copy(prev.Pix, canvas.Pix)
		}
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		if !f(canvas) {
			return
		}
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = prev
		}
	}
}

// indexedFrames is g's frames scaled by nearest pixel to fit w x h, as
// indexes into palettes: frames share one until it's full (a GIF's frames
// can have palettes of their own). nil if a frame has more than 256 colours.
func indexedFrames(g *gif.GIF, w, h int) []Picture {
	gw, gh := g.Config.Width, g.Config.Height
	sw, sh := fitSize(gw, gh, w, h)
	xs := make([]int, sw)
	for x := range xs {
		xs[x] = x * gw / sw
	}
	var pal *[256]uint32
	var index map[uint32]uint8
	// frame indexes c into pal; false if pal fills up
	frame := func(c *image.RGBA, p *Indexed) bool {
		for y := range sh {
			row := c.Pix[(y*gh/sh)*c.Stride:]
			out := p.Pix[y*sw:]
			for x, sx := range xs {
				s := row[4*sx : 4*sx+4 : 4*sx+4]
				v := uint32(s[3])<<24 | uint32(s[0])<<16 | uint32(s[1])<<8 | uint32(s[2])
				i, ok := index[v]
				if !ok {
					if len(index) == len(pal) {
						return false
					}
					i = uint8(len(index))
					index[v], pal[i] = i, v
				}
				out[x] = i
			}
		}
		return true
	}
	var frames []Picture
	compose(g, func(c *image.RGBA) bool {
		p := &Indexed{W: sw, H: sh, Pix: make([]uint8, sw*sh)}
		if pal == nil || !frame(c, p) {
			pal, index = new([256]uint32), map[uint32]uint8{}
			if !frame(c, p) {
				frames = nil
				return false
			}
		}
		p.Palette = pal
		frames = append(frames, p)
		return true
	})
	return frames
}

// At is the frame t into the animation, which loops, and the time until the
// next one.
func (a *Animation) At(t time.Duration) (Picture, time.Duration) {
	t = max(t, 0) % a.total
	for i, d := range a.Delays {
		if t < d {
			return a.Frames[i], d - t
		}
		t -= d
	}
	return a.Frames[0], a.Delays[0]
}

// Bytes is the memory the frames take.
func (a *Animation) Bytes() int {
	b := a.Frames[0].Bounds()
	if _, ok := a.Frames[0].(*Indexed); !ok {
		return 4 * b.Dx() * b.Dy() * len(a.Frames)
	}
	palettes := map[*[256]uint32]bool{}
	for _, f := range a.Frames {
		palettes[f.(*Indexed).Palette] = true
	}
	return b.Dx()*b.Dy()*len(a.Frames) + 4*256*len(palettes)
}
