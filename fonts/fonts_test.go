package fonts

import (
	"bytes"
	"encoding/base64"
	"errors"
	"slices"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
)

const text = "AVATAR Wave ÉéÅ çñ 0123 …"

// kerned is Go Regular with A, V kerned by -150 units, and V, A by -90.
func kerned(t *testing.T) []byte {
	tables, err := readTables(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := sfnt.Parse(goregular.TTF)
	var buf sfnt.Buffer
	a, _ := f.GlyphIndex(&buf, 'A')
	v, _ := f.GlyphIndex(&buf, 'V')
	k := []byte{0, 0, 0, 1, 0, 0, 0, 26, 0, 1, 0, 2, 0, 12, 0, 1, 0, 0}
	for _, p := range [][3]int{{int(a), int(v), -150}, {int(v), int(a), -90}} {
		k = append(k, byte(p[0]>>8), byte(p[0]), byte(p[1]>>8), byte(p[1]), byte(p[2]>>8), byte(p[2]))
	}
	tables["kern"] = k
	tables["head"] = slices.Clone(tables["head"])
	return write(tables)
}

func TestSubset(t *testing.T) {
	ttf := kerned(t)
	b, err := Subset(ttf, []rune(text+"\U0001F600"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > len(goregular.TTF)/4 {
		t.Errorf("%d bytes, from %d", len(b), len(goregular.TTF))
	}
	if err := deck.CheckFont(base64.StdEncoding.EncodeToString(b)); err != nil {
		t.Fatal(err)
	}
	orig, err := sfnt.Parse(ttf)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := sfnt.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if sub.NumGlyphs() > 30 {
		t.Errorf("%d glyphs", sub.NumGlyphs())
	}

	// each glyph: the same outline and advance
	var ob, sb sfnt.Buffer
	ppem := fixed.I(40)
	gid := func(f *sfnt.Font, buf *sfnt.Buffer, r rune) sfnt.GlyphIndex {
		g, err := f.GlyphIndex(buf, r)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	for _, r := range text {
		og, sg := gid(orig, &ob, r), gid(sub, &sb, r)
		if sg == 0 {
			t.Errorf("%q: no glyph", r)
			continue
		}
		os, err1 := orig.LoadGlyph(&ob, og, ppem, nil)
		ss, err2 := sub.LoadGlyph(&sb, sg, ppem, nil)
		if err1 != nil || err2 != nil || !slices.Equal(os, ss) {
			t.Errorf("%q: outline %v %v", r, err1, err2)
		}
		oa, _ := orig.GlyphAdvance(&ob, og, ppem, font.HintingNone)
		sa, _ := sub.GlyphAdvance(&sb, sg, ppem, font.HintingNone)
		if oa != sa {
			t.Errorf("%q: advance %v, want %v", r, sa, oa)
		}
	}
	if gid(sub, &sb, 'x') != 0 {
		t.Error("x wasn't asked for")
	}

	// kerning
	kerns := 0
	for _, p := range [][2]rune{{'A', 'V'}, {'V', 'A'}, {'T', 'A'}, {'W', 'a'}} {
		ok, err1 := orig.Kern(&ob, gid(orig, &ob, p[0]), gid(orig, &ob, p[1]), ppem, font.HintingNone)
		sk, err2 := sub.Kern(&sb, gid(sub, &sb, p[0]), gid(sub, &sb, p[1]), ppem, font.HintingNone)
		if (err1 == nil) != (err2 == nil) || ok != sk {
			t.Errorf("kern %q: %v %v, want %v %v", p, sk, err2, ok, err1)
		}
		if ok != 0 {
			kerns++
		}
	}
	if kerns != 2 {
		t.Error("no kerning to test")
	}

	// it draws the same
	of, _ := gfx.ParseFont(ttf)
	sf, err := gfx.ParseFont(b)
	if err != nil {
		t.Fatal(err)
	}
	draw := func(f *gfx.Font) []byte {
		c := gfx.NewCanvas(400, 40)
		c.DrawText(f, text, gfx.Rect{Max: c.Image().Rect.Max}, 24, 0xffffff)
		return c.Image().Pix
	}
	if !bytes.Equal(draw(of), draw(sf)) {
		t.Error("draws differently")
	}
	if m := sf.Missing("Wave x"); !slices.Equal(m, []rune{'x'}) {
		t.Errorf("missing %q", m)
	}
}

func TestSubsetEmpty(t *testing.T) {
	b, err := Subset(goregular.TTF, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := sfnt.Parse(b)
	if err != nil || f.NumGlyphs() != 1 {
		t.Errorf("%v", err)
	}
}

func TestSubsetBad(t *testing.T) {
	for name, b := range map[string][]byte{
		"cff":        []byte("OTTO\x00\x00\x00\x00\x00\x00\x00\x00"),
		"collection": []byte("ttcf\x00\x01\x00\x00\x00\x00\x00\x00"),
	} {
		if _, err := Subset(b, []rune("a")); !errors.Is(err, ErrNotTrueType) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for n := range len(goregular.TTF) {
		if n%97 != 0 {
			continue
		}
		Subset(goregular.TTF[:n], []rune(text)) // no panic
	}
	bad := slices.Clone(goregular.TTF)
	for i := 12; i < len(bad); i += 13 {
		bad[i] ^= 0x5a
	}
	Subset(bad, []rune(text)) // no panic
}
