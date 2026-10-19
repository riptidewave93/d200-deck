package gfx

import (
	"image"
	"image/draw"
	"image/gif"
	"time"
)

// Animation is an animated GIF's frames, composed and scaled to fit a box.
type Animation struct {
	Frames []*image.RGBA
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
// each to fit w x h.
func NewAnimation(g *gif.GIF, w, h int) *Animation {
	a := &Animation{}
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	for i, f := range g.Image {
		var disposal byte
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		var prev *image.RGBA
		if disposal == gif.DisposalPrevious {
			prev = image.NewRGBA(canvas.Bounds())
			copy(prev.Pix, canvas.Pix)
		}
		draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
		a.Frames = append(a.Frames, Fit(canvas, w, h))
		d := shortDelay
		if i < len(g.Delay) && g.Delay[i] > 1 {
			d = max(time.Duration(g.Delay[i])*10*time.Millisecond, minDelay)
		}
		a.Delays = append(a.Delays, d)
		a.total += d
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, f.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = prev
		}
	}
	return a
}

// At is the frame t into the animation, which loops, and the time until the
// next one.
func (a *Animation) At(t time.Duration) (*image.RGBA, time.Duration) {
	t = max(t, 0) % a.total
	for i, d := range a.Delays {
		if t < d {
			return a.Frames[i], d - t
		}
		t -= d
	}
	return a.Frames[0], a.Delays[0]
}

// Pixels is the frames' size in all.
func (a *Animation) Pixels() int {
	b := a.Frames[0].Bounds()
	return b.Dx() * b.Dy() * len(a.Frames)
}
