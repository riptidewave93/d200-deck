package deckd

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"golang.org/x/image/font/gofont/gomono"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/screen"
)

// solidPNG is a base64 PNG of w x h pixels in col.
func solidPNG(t *testing.T, w, h int, col color.NRGBA) string {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, col)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func count(c *gfx.Canvas, r gfx.Rect, col uint32) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if c.Pix[y*c.W+x] == col {
				n++
			}
		}
	}
	return n
}

func TestColours(t *testing.T) {
	plain := deck.Button{}
	custom := deck.Button{Color: "#ff0000", Background: "#000080"}
	cases := []struct {
		b      deck.Button
		state  keyState
		bg, fg uint32
	}{
		{plain, keyIdle, keyColours[keyIdle], fgColour},
		{plain, keyPressed, keyColours[keyPressed], fgColour},
		{custom, keyIdle, 0x000080, 0xff0000},
		{custom, keyPressed, pressed(0x000080), 0xff0000},
		{custom, keyError, keyColours[keyError], 0xff0000},
		{custom, keyIdentify, keyColours[keyIdentify], 0xff0000},
	}
	for i, tc := range cases {
		bg, fg := colours(tc.b, tc.state)
		if bg != tc.bg || fg != tc.fg {
			t.Errorf("case %d: %#x %#x, want %#x %#x", i, bg, fg, tc.bg, tc.fg)
		}
	}
}

func TestPressedShade(t *testing.T) {
	if got := pressed(0x000000); got != 0x555555 {
		t.Errorf("black pressed: %#x", got)
	}
	if got := pressed(0xffffff); got != 0xaaaaaa {
		t.Errorf("white pressed: %#x", got)
	}
}

func TestCustomColoursDrawn(t *testing.T) {
	c := gfx.NewCanvas(screen.Width, screen.Height)
	r := screen.Key(0)
	drawButton(c, r, deck.Button{Icon: "mute", Color: "#ff0000", Background: "#000080"}, true, keyIdle, &assetCache{})
	if count(c, r, 0x000080) < 1000 || count(c, r, 0xff0000) < 500 {
		t.Errorf("background %d px, icon %d px", count(c, r, 0x000080), count(c, r, 0xff0000))
	}
}

func TestMultiLineLabel(t *testing.T) {
	r := screen.Key(1)
	one, three := gfx.NewCanvas(screen.Width, screen.Height), gfx.NewCanvas(screen.Width, screen.Height)
	drawButton(one, r, deck.Button{Label: "Hello"}, true, keyIdle, &assetCache{})
	drawButton(three, r, deck.Button{Label: "Hello\nthere\nfriend"}, true, keyIdle, &assetCache{})
	rows := func(c *gfx.Canvas) int {
		n := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			if count(c, image.Rect(r.Min.X, y, r.Max.X, y+1), fgColour) > 0 {
				n++
			}
		}
		return n
	}
	if rows(three) <= rows(one) {
		t.Errorf("three lines use %d rows, one line %d", rows(three), rows(one))
	}
	// with an icon, the lines join into one
	c := gfx.NewCanvas(screen.Width, screen.Height)
	drawButton(c, r, deck.Button{Icon: "mute", Label: "a\nb"}, true, keyIdle, &assetCache{})
}

func TestImageButtons(t *testing.T) {
	cfg := deck.DefaultConfig()
	cfg.Images = map[string]string{
		"red":    solidPNG(t, 64, 64, color.NRGBA{0xff, 0, 0, 0xff}),
		"broken": "AAAA",
	}
	ic := &assetCache{}
	ic.reset(cfg)

	cases := []struct {
		name string
		key  int
		b    deck.Button
		min  int // red pixels at least
		max  int
	}{
		{"image fills the key", 0, deck.Button{Image: "red"}, 196 * 196, 196 * 196},
		{"image and label", 1, deck.Button{Image: "red", Label: "Red"}, 100 * 100, 100 * 100},
		{"wide key", 13, deck.Button{Image: "red", Label: "Red"}, 120 * 120, 120 * 120},
		{"wide key, image only", 13, deck.Button{Image: "red"}, 196 * 196, 196 * 196},
		{"broken image", 2, deck.Button{Image: "broken"}, 0, 0},
	}
	for _, tc := range cases {
		c := gfx.NewCanvas(screen.Width, screen.Height)
		drawButton(c, screen.Key(tc.key), tc.b, true, keyIdle, ic)
		if n := count(c, screen.Key(tc.key), 0xff0000); n < tc.min || n > tc.max {
			t.Errorf("%s: %d red pixels", tc.name, n)
		}
	}
	if len(ic.images) != 5 {
		t.Errorf("%d cached images", len(ic.images))
	}
	ic.reset(cfg)
	if len(ic.images) != 5 {
		t.Error("reset with the same config dropped images")
	}
	ic.reset(&deck.Config{})
	if len(ic.images) != 0 || ic.pixels != 0 {
		t.Error("new config kept images")
	}
}

func TestImageCacheBounded(t *testing.T) {
	cfg := &deck.Config{Images: map[string]string{"red": solidPNG(t, 8, 8, color.NRGBA{0xff, 0, 0, 0xff})}}
	var ic assetCache
	ic.reset(cfg)
	for size := 100; size < 400; size++ {
		if ic.image("red", size, size) == nil {
			t.Fatal("no image")
		}
		if ic.pixels > maxCachedPixels {
			t.Fatalf("%d pixels cached", ic.pixels)
		}
	}
}

func TestPageChangeKeepsImages(t *testing.T) {
	d := newTestDeckd()
	d.cfg.Images = map[string]string{"red": solidPNG(t, 8, 8, color.NRGBA{0xff, 0, 0, 0xff})}
	d.cfg.Pages["main"].Keys[11] = deck.Button{Image: "red"}
	d.drawPage()
	if len(d.assets.images) == 0 {
		t.Fatal("image not cached")
	}
	d.press(0, true) // to the browser page
	if len(d.assets.images) == 0 {
		t.Error("page change dropped the cached images")
	}
}

func defaultFont() *gfx.Font {
	f, _ := gfx.BuiltinFont(gfx.DefaultFont)
	return f
}

func TestFonts(t *testing.T) {
	cfg := deck.DefaultConfig()
	cfg.Fonts = map[string]string{
		"mine":   base64.StdEncoding.EncodeToString(gomono.TTF),
		"broken": "AAAA",
	}
	ac := &assetCache{}
	ac.reset(cfg)
	if ac.font("") != defaultFont() || ac.font("go-medium") != defaultFont() {
		t.Error("default font not used")
	}
	if bold, _ := gfx.BuiltinFont("go-bold"); ac.font("go-bold") != bold {
		t.Error("built-in font not used")
	}
	mine := ac.font("mine")
	if mine == defaultFont() || ac.font("mine") != mine {
		t.Error("custom font not loaded once")
	}
	if ac.font("broken") != defaultFont() || ac.font("missing") != defaultFont() {
		t.Error("bad font didn't fall back to the default")
	}
	ac.reset(cfg) // same config: fonts kept
	if ac.font("mine") != mine {
		t.Error("fonts dropped on a page change")
	}
	ac.reset(deck.DefaultConfig())
	if len(ac.fonts) != 0 {
		t.Error("fonts kept across configs")
	}

	// a different font draws a different label
	r := screen.Key(1)
	draw := func(font string) []uint32 {
		c := gfx.NewCanvas(screen.Width, screen.Height)
		ac := &assetCache{}
		ac.reset(cfg)
		drawButton(c, r, deck.Button{Label: "Wide label", Font: font}, true, keyIdle, ac)
		return c.Pix
	}
	if bytes.Equal(u32bytes(draw("")), u32bytes(draw("go-mono"))) {
		t.Error("go-mono draws like the default")
	}
	if bytes.Equal(u32bytes(draw("")), u32bytes(draw("mine"))) {
		t.Error("the custom font draws like the default")
	}
}

func u32bytes(p []uint32) []byte {
	b := make([]byte, 0, len(p)*4)
	for _, v := range p {
		b = append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	return b
}

func TestDrawSafely(t *testing.T) {
	cfg := &deck.Config{Images: map[string]string{"red": solidPNG(t, 8, 8, color.NRGBA{0xff, 0, 0, 0xff})}}
	var ic assetCache
	ic.reset(cfg)
	ic.fonts = map[string]*gfx.Font{"bad": {}} // an unparsable font panics when drawn
	b := deck.Button{Label: "Hi", Font: "bad", Image: "red"}
	r := screen.Key(0)
	c := gfx.NewCanvas(screen.Width, screen.Height)
	drawSafely(c, r, b, true, keyIdle, &ic)
	if f, ok := ic.fonts["bad"]; !ok || f != nil {
		t.Error("font not marked broken")
	}
	if !ic.badImages["red"] || ic.image("red", 10, 10) != nil {
		t.Error("image not marked broken")
	}
	if count(c, r, fgColour) == 0 {
		t.Error("label not drawn with the default font")
	}

	// drawn again, the broken font is not used
	c = gfx.NewCanvas(screen.Width, screen.Height)
	drawSafely(c, r, b, true, keyIdle, &ic)
	if count(c, r, fgColour) == 0 {
		t.Error("label not drawn the second time")
	}

	ic.broken(deck.Button{Font: gfx.DefaultFont}) // built-in fonts are never marked
	if _, ok := ic.fonts[gfx.DefaultFont]; ok {
		t.Error("built-in font marked broken")
	}
	if try(func() { panic("boom") }) != "boom" || try(func() {}) != nil {
		t.Error("try")
	}
}
