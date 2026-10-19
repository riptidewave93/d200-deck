package screen

import (
	"image"
	"testing"

	"github.com/riptidewave93/d200-deck/internal/gfx"
)

// the landscape canvas size
const Width, Height = 1280, 720

func TestCopyRotates(t *testing.T) {
	c := gfx.NewCanvas(Width, Height)
	c.Pix[5*Width+7] = 0xabcdef // canvas (7, 5)
	dst := Scanout{Width: Height, Height: Width, Stride: Height + 16}
	dst.Pixels = make([]uint32, dst.Stride*dst.Height)
	Copy(dst, c, image.Rect(0, 0, Width, Height))
	// canvas x runs bottom to top, canvas y left to right
	if got := dst.Pixels[(dst.Height-1-7)*dst.Stride+5]; got != 0xabcdef {
		t.Errorf("pixel not rotated into place: %#x", got)
	}
	n := 0
	for _, p := range dst.Pixels {
		if p != 0 {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d pixels set, want 1", n)
	}
}

func TestCopyClips(t *testing.T) {
	c := gfx.NewCanvas(Width, Height)
	for i := range c.Pix {
		c.Pix[i] = 1
	}
	dst := Scanout{Width: Height, Height: Width, Stride: Height}
	dst.Pixels = make([]uint32, dst.Stride*dst.Height)
	Copy(dst, c, image.Rect(-100, -100, 10, 10))
	n := 0
	for _, p := range dst.Pixels {
		n += int(p)
	}
	if n != 100 {
		t.Errorf("%d pixels copied, want 100", n)
	}
}
