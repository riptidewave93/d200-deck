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

func TestIconsDraw(t *testing.T) {
	r := image.Rect(0, 0, 120, 120)
	for name := range Icons {
		c := NewCanvas(120, 120)
		c.DrawIcon(name, r, 100, 0xffffff)
		n := lit(c, r, 0)
		if n < 500 || n > 10000-500 {
			t.Errorf("%s: %d of 10000 pixels lit", name, n)
		}
		// cached: drawing again gives the same picture
		c2 := NewCanvas(120, 120)
		c2.DrawIcon(name, r, 100, 0xffffff)
		if lit(c2, r, 0) != n {
			t.Errorf("%s: cached draw differs", name)
		}
	}
}

func TestUnknownIcon(t *testing.T) {
	c := NewCanvas(50, 50)
	c.DrawIcon("no-such-icon", image.Rect(0, 0, 50, 50), 40, 0xffffff)
	if lit(c, image.Rect(0, 0, 50, 50), 0) != 0 {
		t.Error("drew an unknown icon")
	}
}

func TestMirror(t *testing.T) {
	r := image.Rect(0, 0, 100, 100)
	a, b := NewCanvas(100, 100), NewCanvas(100, 100)
	a.DrawIcon("next", r, 100, 0xffffff)
	b.DrawIcon("prev", r, 100, 0xffffff)
	// antialiased edges can differ by a pixel; compare the halves
	left, right := image.Rect(0, 0, 50, 100), image.Rect(50, 0, 100, 100)
	if lit(a, left, 0) != lit(b, right, 0) || lit(a, right, 0) != lit(b, left, 0) {
		t.Errorf("prev isn't next mirrored: halves %d/%d vs %d/%d",
			lit(a, left, 0), lit(a, right, 0), lit(b, left, 0), lit(b, right, 0))
	}
}

func TestText(t *testing.T) {
	if TextWidth("Wide label", 30) <= TextWidth("W", 30) {
		t.Error("longer text isn't wider")
	}
	if TextWidth("Label", 40) <= TextWidth("Label", 20) {
		t.Error("bigger text isn't wider")
	}
	r := image.Rect(0, 0, 200, 60)
	c := NewCanvas(200, 60)
	c.DrawText("Hello", r, 30, 0xffffff)
	if lit(c, r, 0) == 0 {
		t.Error("text drew nothing")
	}
}

func TestTextCacheBounded(t *testing.T) {
	c := NewCanvas(100, 40)
	for i := range maxTextMasks + 10 {
		c.DrawText(string(rune('A'+i%26))+string(rune('a'+i/26)), image.Rect(0, 0, 100, 40), 20, 0xffffff)
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

func TestParsePath(t *testing.T) {
	cases := []struct {
		d     string
		polys int
	}{
		{"M0 0L10 0L10 10Z", 1},
		{"m0 0h10v10h-10z m20 0l5 5h-5z", 2},
		{"M0 0H10V10Z", 1},
		{"M0 0C1 1 2 2 3 3S5 5 6 6Q7 7 8 8T10 10z", 1},
		{"M0 0c1 1 2 2 3 3s1 1 2 2q1 1 2 2t2 2z", 1},
		{"M1.5.5-2-3e1L4,4", 1},
		{"M0 0 1 1 2 0Z", 1},
	}
	for _, tc := range cases {
		polys, err := parsePath(tc.d)
		if err != nil {
			t.Errorf("%q: %v", tc.d, err)
			continue
		}
		if len(polys) != tc.polys {
			t.Errorf("%q: %d polygons, want %d", tc.d, len(polys), tc.polys)
		}
	}
	for _, d := range []string{"M0", "M0 0 X5", "M0 0L1", "M0 0A1 1 0 0 0 1 1", "M0 0L1e"} {
		if _, err := parsePath(d); err == nil {
			t.Errorf("%q: parsed", d)
		}
	}
}

func TestParsePathCurve(t *testing.T) {
	polys, err := parsePath("M0 0C0 10 10 10 10 0Z")
	if err != nil {
		t.Fatal(err)
	}
	p := polys[0]
	if len(p) < 4 {
		t.Fatalf("curve flattened to %d points", len(p))
	}
	last := p[len(p)-1]
	if last.X != 10 || last.Y != 0 {
		t.Errorf("curve ends at %v", last)
	}
	top := 0.0
	for _, pt := range p {
		top = max(top, pt.Y)
	}
	if top < 7 || top > 7.6 {
		t.Errorf("curve peaks at %v, want 7.5", top)
	}
}
