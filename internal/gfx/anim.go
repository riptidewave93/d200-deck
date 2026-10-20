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
// for 100 ms. Frames change at most every minDelay: faster frames are
// skipped, keeping the GIF's timing.
const (
	minDelay   = 50 * time.Millisecond
	shortDelay = 100 * time.Millisecond
)

// NewAnimation composes g's frames, as its disposal methods say, and scales
// each to fit w x h. Frames keep the GIF's colours, a byte a pixel, scaled by
// nearest pixel; a GIF with a frame of more than 256 colours is kept in RGBA,
// scaled smoothly.
func NewAnimation(g *gif.GIF, w, h int) *Animation {
	var delays []time.Duration
	for i := range g.Image {
		d := shortDelay
		if i < len(g.Delay) && g.Delay[i] > 1 {
			d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		delays = append(delays, d)
	}
	keep, kept := shown(delays)
	a := &Animation{Frames: indexedFrames(g, w, h, keep), Delays: kept}
	if a.Frames == nil {
		compose(g, func(i int, c *image.RGBA) bool {
			if keep[i] {
				a.Frames = append(a.Frames, Fit(c, w, h))
			}
			return true
		})
	}
	for _, d := range kept {
		a.total += d
	}
	return a
}

// twoFrames picks two frames to show of a loop too short for shown's: the
// first, and the one showing minDelay in (half way, in a shorter loop). The
// loop keeps its length if it's at least 2 x minDelay; a shorter one can't be
// shown that fast, so it takes 2 x minDelay.
func twoFrames(delays []time.Duration, total time.Duration) ([]bool, []time.Duration) {
	split := min(minDelay, total/2)
	second, at := 1, time.Duration(0)
	for i, d := range delays {
		if at <= split && split < at+d {
			second = max(i, 1)
			break
		}
		at += d
	}
	keep := make([]bool, len(delays))
	keep[0], keep[second] = true, true
	if total < 2*minDelay {
		return keep, []time.Duration{minDelay, minDelay}
	}
	return keep, []time.Duration{split, total - split}
}

// shown picks the frames to show, so that none shows for less than minDelay:
// each starts on time and lasts until the first frame start at least
// minDelay later. keep marks them, and kept is how long each shows.
func shown(delays []time.Duration) (keep []bool, kept []time.Duration) {
	keep = make([]bool, len(delays))
	last := 0                   // the frame shown last
	var start, at time.Duration // when it starts, and frame i does
	for i, d := range delays {
		if i == 0 || at-start >= minDelay {
			if i > 0 {
				kept = append(kept, at-start)
			}
			keep[i], last, start = true, i, at
		}
		at += d
	}
	if rest := at - start; rest < minDelay && len(kept) > 0 {
		// too short to show: the frame before shows on instead
		keep[last] = false
		kept[len(kept)-1] += rest
	} else {
		kept = append(kept, rest)
	}
	if len(kept) == 1 && len(delays) > 1 {
		keep, kept = twoFrames(delays, at)
	}
	return keep, kept
}

// compose calls f with each of g's frames, by index, drawn over what the frames before
// it left, as a GIF is shown, until f returns false.
func compose(g *gif.GIF, f func(int, *image.RGBA) bool) {
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
		if !f(i, canvas) {
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

// indexedFrames is g's frames that keep marks, scaled by nearest pixel to
// fit w x h, as indexes into palettes: frames share one until it's full (a
// GIF's frames can have palettes of their own). nil if a frame has more than
// 256 colours.
func indexedFrames(g *gif.GIF, w, h int, keep []bool) []Picture {
	gw, gh := g.Config.Width, g.Config.Height
	sw, sh := FitSize(gw, gh, w, h)
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
	compose(g, func(i int, c *image.RGBA) bool {
		if !keep[i] {
			return true
		}
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
