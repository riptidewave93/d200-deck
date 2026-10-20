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
	if c := colourAt(a.Frames[1], 7, 7); c != 0xffff0000 {
		t.Errorf("not composed: %#x", c)
	}
	if c := colourAt(a.Frames[1], 0, 0); c != 0xff00ff00 {
		t.Errorf("corner %#x", c)
	}
	f, wait := a.At(150 * time.Millisecond)
	if f != a.Frames[1] || wait != 250*time.Millisecond {
		t.Errorf("at 150 ms: frame %v, wait %v", f == a.Frames[1], wait)
	}
	if f, _ := a.At(400 * time.Millisecond); f != a.Frames[0] {
		t.Error("doesn't loop")
	}
	if _, ok := a.Frames[0].(*Indexed); !ok || a.Bytes() != 2*64+1024 {
		t.Errorf("%T frames, %d bytes", a.Frames[0], a.Bytes())
	}
}

// colourAt is p's pixel at x, y: premultiplied alpha<<24 | r<<16 | g<<8 | b.
func colourAt(p Picture, x, y int) uint32 {
	switch p := p.(type) {
	case *Indexed:
		return p.Palette[p.Pix[y*p.W+x]]
	case *image.RGBA:
		c := p.RGBAAt(x, y)
		return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
	}
	return 0
}

// Scaled by nearest pixel, a GIF keeps its colours: no blends at the edges.
func TestAnimationNearest(t *testing.T) {
	pal := color.Palette{color.RGBA{0xff, 0, 0, 0xff}, color.RGBA{0, 0, 0xff, 0xff}}
	f := image.NewPaletted(image.Rect(0, 0, 2, 1), pal)
	f.Pix[1] = 1
	g := &gif.GIF{Image: []*image.Paletted{f, f}, Delay: []int{10, 10}, Config: image.Config{Width: 2, Height: 1}}
	p := NewAnimation(g, 7, 7).Frames[0].(*Indexed)
	if p.W != 7 || p.H != 3 {
		t.Fatalf("%dx%d", p.W, p.H)
	}
	for y := range p.H {
		for x := range p.W {
			want := uint32(0xffff0000)
			if x >= 4 { // x * 2 / 7 >= 1
				want = 0xff0000ff
			}
			if c := colourAt(p, x, y); c != want {
				t.Errorf("(%d,%d) = %#x, want %#x", x, y, c, want)
			}
		}
	}
}

// Frames with palettes of their own get palettes of their own.
func TestAnimationPalettes(t *testing.T) {
	var g gif.GIF
	for i := range 2 {
		var pal color.Palette
		for j := range 200 {
			pal = append(pal, color.RGBA{uint8(j), uint8(i), 0, 0xff})
		}
		f := image.NewPaletted(image.Rect(0, 0, 20, 10), pal)
		for j := range f.Pix {
			f.Pix[j] = uint8(j)
		}
		g.Image, g.Delay = append(g.Image, f), append(g.Delay, 10)
	}
	g.Config = image.Config{Width: 20, Height: 10}
	a := NewAnimation(&g, 20, 10)
	if _, ok := a.Frames[0].(*Indexed); !ok || len(a.Frames) != 2 || a.Bytes() != 2*200+2*1024 {
		t.Errorf("%d %T frames, %d bytes", len(a.Frames), a.Frames[0], a.Bytes())
	}
	if c := colourAt(a.Frames[0], 5, 0); c != 0xff050000 {
		t.Errorf("frame 1 pixel %#x", c)
	}
	if c := colourAt(a.Frames[1], 5, 0); c != 0xff050100 {
		t.Errorf("frame 2 pixel %#x", c)
	}
}

// A frame of more than 256 colours, here a part frame over another's
// leftovers, keeps the GIF in RGBA.
func TestAnimationManyColours(t *testing.T) {
	palette := func(g uint8) color.Palette {
		var p color.Palette
		for j := range 256 {
			p = append(p, color.RGBA{uint8(j), g, 0, 0xff})
		}
		return p
	}
	whole := image.NewPaletted(image.Rect(0, 0, 32, 16), palette(0))
	for j := range whole.Pix {
		whole.Pix[j] = uint8(j)
	}
	half := image.NewPaletted(image.Rect(0, 0, 16, 16), palette(1)) // new colours on the left
	for j := range half.Pix {
		half.Pix[j] = uint8(j)
	}
	g := &gif.GIF{Image: []*image.Paletted{whole, half}, Delay: []int{10, 10},
		Config: image.Config{Width: 32, Height: 16}}
	a := NewAnimation(g, 32, 16)
	if _, ok := a.Frames[0].(*image.RGBA); !ok || a.Bytes() != 4*2*32*16 {
		t.Errorf("%T frames, %d bytes", a.Frames[0], a.Bytes())
	}
	if c := colourAt(a.Frames[1], 20, 0); c != 0xff140000 { // left over from frame 1
		t.Errorf("pixel %#x", c)
	}
}
