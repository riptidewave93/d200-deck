package gfx

import (
	"image"
	"testing"

	"github.com/riptidewave93/d200-deck/deck"
)

func TestBuiltinFonts(t *testing.T) {
	names := FontNames()
	if len(names) != len(deck.BuiltinFonts) {
		t.Fatalf("gfx has %d fonts, deck lists %d", len(names), len(deck.BuiltinFonts))
	}
	for i, n := range names {
		if n != deck.BuiltinFonts[i] {
			t.Errorf("font %d: gfx %q, deck %q", i, n, deck.BuiltinFonts[i])
		}
		f, ok := BuiltinFont(n)
		if !ok || f.TextWidth("Hi", 20) == 0 {
			t.Errorf("%s doesn't measure text", n)
		}
	}
	if _, ok := BuiltinFont(DefaultFont); !ok {
		t.Error("no default font")
	}
	if _, ok := BuiltinFont("comic-sans"); ok {
		t.Error("found a missing font")
	}
}

func TestParseFont(t *testing.T) {
	if _, err := ParseFont([]byte("not a font")); err == nil {
		t.Error("parsed garbage")
	}
	mono, _ := BuiltinFont("go-mono")
	f, err := ParseFont(mono.data)
	if err != nil {
		t.Fatal(err)
	}
	// monospaced: every character is as wide
	if f.TextWidth("iiii", 30) != f.TextWidth("MMMM", 30) {
		t.Error("parsed font isn't the mono font")
	}
}

func TestFaceCacheBounded(t *testing.T) {
	f, err := ParseFont(builtinFonts["go-regular"].data)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCanvas(60, 60)
	for px := 8; px < 8+maxFaces+10; px++ {
		c.DrawText(f, "x", image.Rect(0, 0, 60, 60), px, 0xffffff)
	}
	if len(f.faces) > maxFaces {
		t.Errorf("%d faces kept", len(f.faces))
	}
}
