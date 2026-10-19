package svgpath

import "testing"

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
		polys, err := Parse(tc.d)
		if err != nil {
			t.Errorf("%q: %v", tc.d, err)
			continue
		}
		if len(polys) != tc.polys {
			t.Errorf("%q: %d polygons, want %d", tc.d, len(polys), tc.polys)
		}
	}
	for _, d := range []string{"M0", "M0 0 X5", "M0 0L1", "M0 0A1 1 0 0 0 1 1", "M0 0L1e"} {
		if _, err := Parse(d); err == nil {
			t.Errorf("%q: parsed", d)
		}
	}
}

func TestParsePathCurve(t *testing.T) {
	polys, err := Parse("M0 0C0 10 10 10 10 0Z")
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

func TestViewBox(t *testing.T) {
	if x, y, w, h, err := ViewBox("0 -960 960,960"); err != nil || x != 0 || y != -960 || w != 960 || h != 960 {
		t.Errorf("%v %v %v %v %v", x, y, w, h, err)
	}
	for _, s := range []string{"", "0 0 1", "0 0 0 1", "0 0 1 -1", "a b c d", "0 0 NaN 1", "0 0 Inf 1"} {
		if _, _, _, _, err := ViewBox(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
}
