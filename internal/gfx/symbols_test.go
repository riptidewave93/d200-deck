package gfx

import (
	"image"
	"sort"
	"testing"
)

func TestSymbolsSortedAndUnique(t *testing.T) {
	if len(symbols) < 3000 {
		t.Fatalf("only %d symbols", len(symbols))
	}
	for i := 1; i < len(symbols); i++ {
		if symbols[i-1].name >= symbols[i].name {
			t.Fatalf("symbols not sorted at %q, %q", symbols[i-1].name, symbols[i].name)
		}
	}
}

// Every symbol parses and draws something, so no config can name a broken one.
func TestAllSymbolsDraw(t *testing.T) {
	r := image.Rect(0, 0, 32, 32)
	for _, s := range symbols {
		if _, err := parsePath(s.d); err != nil {
			t.Errorf("%s: %v", s.name, err)
			continue
		}
		icon, ok := lookup(s.name)
		if !ok {
			t.Fatalf("%s: lookup failed", s.name)
		}
		m := image.NewAlpha(r)
		icon(&pen{m: m, s: 32.0 / 100})
		lit := 0
		for _, a := range m.Pix {
			if a != 0 {
				lit++
			}
		}
		if lit == 0 {
			t.Errorf("%s draws nothing", s.name)
		}
	}
}

func TestIconNames(t *testing.T) {
	names := IconNames()
	if !sort.StringsAreSorted(names) {
		t.Error("names not sorted")
	}
	if len(names) != len(drawn)+len(aliases)+len(symbols) {
		t.Errorf("%d names", len(names))
	}
	for _, n := range []string{"play-pause", "github", "discord", "back", "sleep", "volume_up", "home", "10k"} {
		if !HasIcon(n) {
			t.Errorf("%s missing", n)
		}
	}
	for _, n := range []string{"", "nope", "volume up", "zzzz"} {
		if HasIcon(n) {
			t.Errorf("%q found", n)
		}
		if _, ok := lookup(n); ok {
			t.Errorf("%q looked up", n)
		}
	}
	for alias, target := range aliases {
		if !HasIcon(target) {
			t.Errorf("alias %s points at missing %s", alias, target)
		}
	}
}

func TestAliasDrawsTarget(t *testing.T) {
	r := image.Rect(0, 0, 64, 64)
	a, b := NewCanvas(64, 64), NewCanvas(64, 64)
	a.DrawIcon("sleep", r, 64, 0xffffff)
	b.DrawIcon("bedtime", r, 64, 0xffffff)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatal("alias draws a different icon")
		}
	}
}

func TestIconCacheBounded(t *testing.T) {
	c := NewCanvas(16, 16)
	for _, s := range symbols[:maxIconMasks+20] {
		c.DrawIcon(s.name, image.Rect(0, 0, 16, 16), 16, 0xffffff)
	}
	if len(iconMasks) > maxIconMasks {
		t.Errorf("icon cache holds %d masks", len(iconMasks))
	}
}

func TestDiscordSymbol(t *testing.T) {
	// drawn 147 px square, one pixel per SVG unit, offset by (10.18, 25.5)
	c := NewCanvas(147, 147)
	c.DrawIcon("discord", image.Rect(0, 0, 147, 147), 147, 0xffffff)
	at := func(x, y float64) uint32 { return c.Pix[int(y+25.5)*147+int(x+10.18)] }
	for _, p := range []Pt{{42, 53}, {84, 53}, {63, 98}, {5, 5}} { // eyes, under the chin, a corner
		if at(p.X, p.Y) != 0 {
			t.Errorf("%v filled", p)
		}
	}
	for _, p := range []Pt{{63, 30}, {63, 70}, {20, 60}} {
		if at(p.X, p.Y) != 0xffffff {
			t.Errorf("%v empty", p)
		}
	}
}
