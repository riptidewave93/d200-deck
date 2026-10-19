package gfx

import (
	"image"
	"image/color"
	"image/gif"
	"testing"
	"time"
)

func TestAnimation(t *testing.T) {
	pal := color.Palette{color.Transparent, color.RGBA{0xff, 0, 0, 0xff}, color.RGBA{0, 0xff, 0, 0xff}}
	full := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	for i := range full.Pix {
		full.Pix[i] = 1
	}
	corner := image.NewPaletted(image.Rect(0, 0, 2, 2), pal) // drawn over the red
	for i := range corner.Pix {
		corner.Pix[i] = 2
	}
	g := &gif.GIF{Image: []*image.Paletted{full, corner}, Delay: []int{0, 30},
		Config: image.Config{Width: 4, Height: 4}}
	a := NewAnimation(g, 8, 8)
	if len(a.Frames) != 2 || a.Frames[0].Bounds().Dx() != 8 {
		t.Fatalf("%d frames", len(a.Frames))
	}
	if a.Delays[0] != shortDelay || a.Delays[1] != 300*time.Millisecond {
		t.Errorf("delays %v", a.Delays)
	}
	// frame 2 keeps frame 1's red outside the corner
	if c := a.Frames[1].RGBAAt(7, 7); c.R != 0xff {
		t.Errorf("not composed: %v", c)
	}
	if c := a.Frames[1].RGBAAt(0, 0); c.G != 0xff {
		t.Errorf("corner %v", c)
	}
	f, wait := a.At(150 * time.Millisecond)
	if f != a.Frames[1] || wait != 250*time.Millisecond {
		t.Errorf("at 150 ms: frame %v, wait %v", f == a.Frames[1], wait)
	}
	if f, _ := a.At(400 * time.Millisecond); f != a.Frames[0] {
		t.Error("doesn't loop")
	}
	if a.Pixels() != 2*64 {
		t.Errorf("%d pixels", a.Pixels())
	}
}
