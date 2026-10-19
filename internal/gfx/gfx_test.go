package gfx

import (
	"image"
	"testing"
)

// lit counts pixels in r that aren't bg.
func lit(c *Canvas, r Rect, bg uint32) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if c.Pix[y*c.W+x] != bg {
				n++
			}
		}
	}
	return n
}

func TestFillClips(t *testing.T) {
	c := NewCanvas(10, 10)
	c.Fill(image.Rect(-5, -5, 3, 20), 0xff0000)
	if got := lit(c, image.Rect(0, 0, 10, 10), 0); got != 30 {
		t.Errorf("%d pixels filled, want 30", got)
	}
}

func TestBlend(t *testing.T) {
	cases := []struct {
		dst, src uint32
		n, total int
		want     uint32
	}{
		{0x000000, 0xffffff, 255, 255, 0xffffff},
		{0x000000, 0xffffff, 0, 255, 0x000000},
		{0x000000, 0xc864ff, 1, 2, 0x64327f},
		{0xffffff, 0x000000, 1, 2, 0x808080},
	}
	for _, tc := range cases {
		if got := blend(tc.dst, tc.src, tc.n, tc.total); got != tc.want {
			t.Errorf("blend(%#x, %#x, %d/%d) = %#x, want %#x", tc.dst, tc.src, tc.n, tc.total, got, tc.want)
		}
	}
}

func TestDrawMaskClips(t *testing.T) {
	c := NewCanvas(10, 10)
	m := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for i := range m.Pix {
		m.Pix[i] = 0xff
	}
	c.DrawMask(m, image.Pt(8, -2), 0x00ff00)
	if got := lit(c, image.Rect(0, 0, 10, 10), 0); got != 4 {
		t.Errorf("%d pixels drawn, want 4", got)
	}
}

func TestImage(t *testing.T) {
	c := NewCanvas(3, 2)
	c.Pix[4] = 0x112233
	img := c.Image()
	if r, g, b, a := img.At(1, 1).RGBA(); r>>8 != 0x11 || g>>8 != 0x22 || b>>8 != 0x33 || a>>8 != 0xff {
		t.Errorf("pixel (1,1) = %v", img.At(1, 1))
	}
}

func TestIconMask(t *testing.T) {
	// a square over the left half of a 2:1 box, centred: the middle quarter
	ic, err := ParseIcon("M0 0H10V10H0Z", "0 0 20 10")
	if err != nil {
		t.Fatal(err)
	}
	m := ic.Mask(100)
	at := func(x, y int) uint8 { return m.AlphaAt(x, y).A }
	if at(25, 50) != 0xff || at(75, 50) != 0 || at(25, 10) != 0 || at(25, 90) != 0 {
		t.Errorf("coverage %d %d %d %d", at(25, 50), at(75, 50), at(25, 10), at(25, 90))
	}
	for _, bad := range [][2]string{{"M0", "0 0 1 1"}, {"M0 0H1V1Z", "0 0 0 1"}, {"M0 0H1V1Z", "x"}} {
		if _, err := ParseIcon(bad[0], bad[1]); err == nil {
			t.Errorf("%q in %q parsed", bad[0], bad[1])
		}
	}
}

func TestText(t *testing.T) {
	f, _ := BuiltinFont(DefaultFont)
	if f.TextWidth("Wide label", 30) <= f.TextWidth("W", 30) {
		t.Error("longer text isn't wider")
	}
	if f.TextWidth("Label", 40) <= f.TextWidth("Label", 20) {
		t.Error("bigger text isn't wider")
	}
	r := image.Rect(0, 0, 200, 60)
	c := NewCanvas(200, 60)
	c.DrawText(f, "Hello", r, 30, 0xffffff)
	if lit(c, r, 0) == 0 {
		t.Error("text drew nothing")
	}
}

func TestTextCacheBounded(t *testing.T) {
	f, _ := BuiltinFont(DefaultFont)
	c := NewCanvas(100, 40)
	for i := range maxTextMasks + 10 {
		c.DrawText(f, string(rune('A'+i%26))+string(rune('a'+i/26)), image.Rect(0, 0, 100, 40), 20, 0xffffff)
	}
	if len(textMasks) > maxTextMasks {
		t.Errorf("text cache holds %d masks", len(textMasks))
	}
}

func TestDrawNumber(t *testing.T) {
	c := NewCanvas(40, 20)
	c.DrawNumber(18, image.Rect(0, 0, 40, 20), 2, 0xffffff)
	// "1" has 8 lit glyph pixels and "8" has 13, each 2x2
	if got := lit(c, image.Rect(0, 0, 40, 20), 0); got != (8+13)*4 {
		t.Errorf("%d pixels lit", got)
	}
}
