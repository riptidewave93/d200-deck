package gfx

import (
	"image"
	"image/color"
	"testing"
)

func TestFit(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	cases := []struct{ w, h, wantW, wantH int }{
		{100, 100, 100, 50},
		{196, 196, 196, 98},
		{458, 196, 392, 196},
		{1, 1, 1, 1},
	}
	for _, tc := range cases {
		b := Fit(src, tc.w, tc.h).Bounds()
		if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
			t.Errorf("Fit into %dx%d: %v", tc.w, tc.h, b)
		}
	}
}

func TestShrink(t *testing.T) {
	// 4x2 to 2x1: each output pixel averages a 2x2 box
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 8)
	}
	got := Fit(src, 2, 2)
	if got.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Fatalf("bounds %v", got.Bounds())
	}
	want := []uint8{80, 88, 96, 104, 144, 152, 160, 168}
	if string(got.Pix) != string(want) {
		t.Errorf("pixels %v, want %v", got.Pix, want)
	}

	// uneven boxes, an offset source and a non-RGBA image
	n := image.NewNRGBA(image.Rect(3, 3, 8, 5))
	for i := 0; i < len(n.Pix); i += 4 {
		copy(n.Pix[i:], []uint8{0xff, 0, 0, 0x80}) // half-transparent red
	}
	got = Fit(n, 2, 2)
	if got.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Fatalf("bounds %v", got.Bounds())
	}
	for i := 0; i < len(got.Pix); i += 4 {
		if p := got.Pix[i : i+4]; p[0] != 0x80 || p[1] != 0 || p[3] != 0x80 {
			t.Errorf("pixel % x, want premultiplied 80 00 00 80", p)
		}
	}

	// growing uses a filter instead
	if b := Fit(image.NewRGBA(image.Rect(0, 0, 10, 10)), 40, 40).Bounds(); b.Dx() != 40 {
		t.Errorf("grown to %v", b)
	}
}

func BenchmarkFit(b *testing.B) {
	src := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for i := range src.Pix {
		src.Pix[i] = uint8(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		Fit(src, 196, 196)
	}
}

func TestDrawImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{0xff, 0, 0, 0xff}) // opaque red
	img.Set(1, 0, color.NRGBA{0, 0, 0xff, 0x80}) // half blue
	c := NewCanvas(4, 3)
	c.Fill(image.Rect(0, 0, 4, 3), 0x00ff00)
	c.DrawImage(img, image.Rect(0, 0, 4, 3)) // centred: (1,1) and (2,1)
	if got := c.Pix[1*4+1]; got != 0xff0000 {
		t.Errorf("opaque pixel %#x", got)
	}
	if got := c.Pix[1*4+2]; got != 0x007f80 {
		t.Errorf("blended pixel %#x, want 0x007f80", got)
	}
	if got := c.Pix[0]; got != 0x00ff00 {
		t.Errorf("pixel outside the image changed: %#x", got)
	}
	c.DrawImage(img, image.Rect(-5, -5, -3, -4)) // off the canvas: no panic
}
