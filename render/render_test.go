package render

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/gomono"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/icons"
	"github.com/riptidewave93/d200-deck/internal/gfx"
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
		state  State
		bg, fg uint32
	}{
		{plain, Idle, Colours[Idle], Foreground},
		{plain, Pressed, Colours[Pressed], Foreground},
		{custom, Idle, 0x000080, 0xff0000},
		{custom, Pressed, pressed(0x000080), 0xff0000},
		{custom, Error, Colours[Error], 0xff0000},
		{custom, Identify, Colours[Identify], 0xff0000},
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
	c := gfx.NewCanvas(Width, Height)
	r := Key(0)
	withIcons().draw(c, r, deck.Button{Icon: "mute", Color: "#ff0000", Background: "#000080"}, true, Idle)
	if count(c, r, 0x000080) < 1000 || count(c, r, 0xff0000) < 500 {
		t.Errorf("background %d px, icon %d px", count(c, r, 0x000080), count(c, r, 0xff0000))
	}
}

func TestMultiLineLabel(t *testing.T) {
	r := Key(1)
	one, three := gfx.NewCanvas(Width, Height), gfx.NewCanvas(Width, Height)
	withIcons().draw(one, r, deck.Button{Label: "Hello"}, true, Idle)
	withIcons().draw(three, r, deck.Button{Label: "Hello\nthere\nfriend"}, true, Idle)
	rows := func(c *gfx.Canvas) int {
		n := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			if count(c, image.Rect(r.Min.X, y, r.Max.X, y+1), Foreground) > 0 {
				n++
			}
		}
		return n
	}
	if rows(three) <= rows(one) {
		t.Errorf("three lines use %d rows, one line %d", rows(three), rows(one))
	}
	// with an icon, the lines join into one
	c := gfx.NewCanvas(Width, Height)
	withIcons().draw(c, r, deck.Button{Icon: "mute", Label: "a\nb"}, true, Idle)
}

func TestImageButtons(t *testing.T) {
	cfg := deck.DefaultConfig()
	cfg.Images = map[string]string{
		"red":    solidPNG(t, 64, 64, color.NRGBA{0xff, 0, 0, 0xff}),
		"broken": "AAAA",
	}
	ic := New(cfg)

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
		c := gfx.NewCanvas(Width, Height)
		ic.draw(c, Key(tc.key), tc.b, true, Idle)
		if n := count(c, Key(tc.key), 0xff0000); n < tc.min || n > tc.max {
			t.Errorf("%s: %d red pixels", tc.name, n)
		}
	}
	if len(ic.images) != 5 {
		t.Errorf("%d cached images", len(ic.images))
	}
	ic.SetConfig(cfg)
	if len(ic.images) != 5 {
		t.Error("reset with the same config dropped images")
	}
	ic.SetConfig(&deck.Config{})
	if len(ic.images) != 0 || ic.pixels != 0 {
		t.Error("new config kept images")
	}
}

func TestImageCacheBounded(t *testing.T) {
	cfg := &deck.Config{Images: map[string]string{"red": solidPNG(t, 8, 8, color.NRGBA{0xff, 0, 0, 0xff})}}
	ic := New(cfg)
	for size := 100; size < 400; size++ {
		if ic.image("red", size, size) == nil {
			t.Fatal("no image")
		}
		if ic.pixels > maxCachedPixels {
			t.Fatalf("%d pixels cached", ic.pixels)
		}
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
	ac := New(cfg)
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
	ac.SetConfig(cfg) // same config: fonts kept
	if ac.font("mine") != mine {
		t.Error("fonts dropped on a page change")
	}
	ac.SetConfig(deck.DefaultConfig())
	if len(ac.fonts) != 0 {
		t.Error("fonts kept across configs")
	}

	// a different font draws a different label
	r := Key(1)
	draw := func(font string) []uint32 {
		c := gfx.NewCanvas(Width, Height)
		ac := New(cfg)
		ac.draw(c, r, deck.Button{Label: "Wide label", Font: font}, true, Idle)
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
	ic := New(cfg)
	ic.fonts = map[string]*gfx.Font{"bad": {}} // an unparsable font panics when drawn
	b := deck.Button{Label: "Hi", Font: "bad", Image: "red"}
	r := Key(0)
	c := gfx.NewCanvas(Width, Height)
	ic.Draw(c, r, b, true, Idle)
	if f, ok := ic.fonts["bad"]; !ok || f != nil {
		t.Error("font not marked broken")
	}
	if !ic.badImages["red"] || ic.image("red", 10, 10) != nil {
		t.Error("image not marked broken")
	}
	if count(c, r, Foreground) == 0 {
		t.Error("label not drawn with the default font")
	}

	// drawn again, the broken font is not used
	c = gfx.NewCanvas(Width, Height)
	ic.Draw(c, r, b, true, Idle)
	if count(c, r, Foreground) == 0 {
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

func TestDrawButton(t *testing.T) {
	icon := deck.Button{Icon: "mute"}
	label := deck.Button{Label: "Label only"}
	both := deck.Button{Icon: "github", Label: "Source Repo"}
	cases := []struct {
		name string
		key  int
		b    deck.Button
	}{
		{"icon", 0, icon}, {"label", 1, label}, {"icon and label", 2, both}, {"wide", 13, both},
	}
	for _, tc := range cases {
		c := gfx.NewCanvas(Width, Height)
		r := Key(tc.key)
		withIcons().draw(c, r, tc.b, true, Idle)
		fg := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if c.Pix[y*c.W+x] == Foreground {
					fg++
				}
			}
		}
		if fg < 50 {
			t.Errorf("%s: %d foreground pixels", tc.name, fg)
		}
	}
	c := gfx.NewCanvas(Width, Height)
	c.Fill(image.Rect(0, 0, Width, Height), 0x123456)
	withIcons().draw(c, Key(5), deck.Button{}, false, Idle)
	if c.Pix[Key(5).Min.Y*c.W+Key(5).Min.X] != 0 {
		t.Error("blank key not cleared")
	}
}

func TestFitText(t *testing.T) {
	if px := FitText(defaultFont(), "Hi", 1000, 36); px != 36 {
		t.Errorf("short text shrunk to %d", px)
	}
	long := "A label far too long for one key"
	f, _ := gfx.BuiltinFont(gfx.DefaultFont)
	px := FitText(f, long, 180, 36)
	if px >= 36 || (px > 12 && f.TextWidth(long, px) > 180) {
		t.Errorf("long text at %d px is %d wide", px, f.TextWidth(long, px))
	}
}

func TestKey(t *testing.T) {
	r := New(deck.DefaultConfig())
	if img := r.Key(0, deck.Button{Label: "Hi"}, false); img.Bounds() != image.Rect(0, 0, 196, 196) {
		t.Errorf("key 0 is %v", img.Bounds())
	}
	mute := deck.Button{Action: deck.Action{Type: deck.ActionApp, App: deck.AppDiscord, Op: "mute"}}
	off, on := r.Key(13, mute, false), r.Key(13, mute, true)
	if off.Bounds() != image.Rect(0, 0, 458, 196) {
		t.Errorf("key 13 is %v", off.Bounds())
	}
	if bytes.Equal(off.Pix, on.Pix) {
		t.Error("on looks like off")
	}
	if r.Key(14, mute, false) != nil || r.Key(-1, mute, false) != nil {
		t.Error("drew a key that isn't there")
	}
}

func TestKeysFitAndDontOverlap(t *testing.T) {
	canvas := image.Rect(0, 0, Width, Height)
	for i := range deck.KeyCount {
		r := Key(i)
		if r.Empty() || !r.In(canvas) {
			t.Errorf("key %d at %v is off the canvas", i, r)
		}
		for j := range i {
			if r.Overlaps(Key(j)) {
				t.Errorf("keys %d and %d overlap", i, j)
			}
		}
	}
	if Key(13).Dx() <= 2*Key(0).Dx() {
		t.Error("the wide key isn't wide")
	}
}

// withIcons is a renderer for a config with a few catalog icons.
func withIcons() *Renderer {
	c := &deck.Config{Icons: map[string]deck.Icon{}}
	for _, n := range []string{"mute", "github"} {
		c.Icons[n], _ = icons.Get(n)
	}
	return New(c)
}

func TestConfigIcons(t *testing.T) {
	r := Key(0)
	draw := func(rn *Renderer, b deck.Button) *gfx.Canvas {
		c := gfx.NewCanvas(Width, Height)
		rn.Draw(c, r, b, true, Idle)
		return c
	}
	// a missing icon draws nothing; the label takes the key
	if c := draw(New(&deck.Config{}), deck.Button{Icon: "mute", Color: "#ff0000"}); count(c, r, 0xff0000) != 0 {
		t.Error("drew a missing icon")
	}
	// Extra icons, for the device's own screens
	extra := New(&deck.Config{})
	extra.Extra = map[string]deck.Icon{"mute": must(icons.Get("mute"))}
	if c := draw(extra, deck.Button{Icon: "mute", Color: "#ff0000"}); count(c, r, 0xff0000) < 500 {
		t.Error("Extra icon not drawn")
	}
	// a picture icon is drawn at icon size, untinted
	pic := New(&deck.Config{Icons: map[string]deck.Icon{"e": {PNG: solidPNG(t, 64, 64, color.NRGBA{0, 0xff, 0, 0xff})}}})
	c := draw(pic, deck.Button{Icon: "e", Label: "Hi", Color: "#ff0000"})
	if n := count(c, r, 0x00ff00); n != 100*100 {
		t.Errorf("%d green pixels", n)
	}
}

func must(ic deck.Icon, ok bool) deck.Icon {
	if !ok {
		panic("no icon")
	}
	return ic
}

// redGreenGIF is a base64 GIF of two 196 px frames, red then green, 100 ms
// each.
func redGreenGIF(t *testing.T) string { return testGIF(t, 2) }

// testGIF is a base64 GIF of n 196 px frames, red and green in turn, 100 ms
// each.
func testGIF(t *testing.T, n int) string {
	pal := color.Palette{color.RGBA{0xff, 0, 0, 0xff}, color.RGBA{0, 0xff, 0, 0xff}}
	g := &gif.GIF{}
	for i := range n {
		f := image.NewPaletted(image.Rect(0, 0, 196, 196), pal)
		for j := range f.Pix {
			f.Pix[j] = uint8(i % 2)
		}
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 10)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestAnimatedGIF(t *testing.T) {
	now := time.Now()
	r := New(&deck.Config{Images: map[string]string{"anim": redGreenGIF(t)}})
	r.Now = func() time.Time { return now }
	r.SetConfig(r.cfg) // same config: the start stays
	r.start = now
	draw := func() *gfx.Canvas {
		c := gfx.NewCanvas(Width, Height)
		r.Draw(c, Key(0), deck.Button{Image: "anim"}, true, Idle)
		return c
	}
	if c := draw(); count(c, Key(0), 0xff0000) != 196*196 || !r.Next().Equal(now.Add(100*time.Millisecond)) {
		t.Errorf("first frame: next %v", r.Next().Sub(now))
	}
	now = now.Add(150 * time.Millisecond)
	if c := draw(); count(c, Key(0), 0x00ff00) != 196*196 {
		t.Error("second frame not drawn")
	}
	r.Draw(gfx.NewCanvas(Width, Height), Key(1), deck.Button{Label: "x"}, true, Idle)
	if !r.Next().IsZero() {
		t.Error("a still key is animated")
	}
}

func TestAnimationsPerPage(t *testing.T) {
	// two 100-frame full-key GIFs fit a page's budget (3.8 MB each), three don't
	g := testGIF(t, deck.MaxFrames)
	r := New(&deck.Config{Images: map[string]string{"a": g, "b": g, "c": g}})
	animated := func(id string) bool {
		r.Draw(gfx.NewCanvas(Width, Height), Key(0), deck.Button{Image: id}, true, Idle)
		return !r.Next().IsZero()
	}
	if !animated("a") || !animated("b") || animated("c") {
		t.Fatal("budget: three GIFs animate on one page, or the first two don't")
	}
	r.NewPage()
	if !animated("c") {
		t.Error("the refused GIF stays still on a page of its own")
	}
	r.NewPage()
	if !animated("a") {
		t.Error("an earlier page's GIF doesn't animate again")
	}
}

func TestMissingGlyphs(t *testing.T) {
	r := New(&deck.Config{})
	b := deck.Button{Label: "Done ✓", On: &deck.Look{Label: "日本"}}
	if got := string(r.MissingGlyphs(b)); got != "✓日本" {
		t.Errorf("missing %q", got)
	}
	if got := r.MissingGlyphs(deck.Button{Label: "Mute\nMic"}); len(got) != 0 {
		t.Errorf("missing %q", string(got))
	}
}

func TestAnimBytes(t *testing.T) {
	data := testGIF(t, 3)
	g, err := deck.DecodeAnimation(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, box := range [][2]int{{196, 196}, {100, 100}, {458, 196}} {
		want := gfx.NewAnimation(g, box[0], box[1]).Bytes()
		if got := AnimBytes(data, box[0], box[1]); got != want || got == 0 {
			t.Errorf("in %v: %d bytes, the renderer keeps %d", box, got, want)
		}
	}
	if got := AnimBytes(data, 196, 196); got != 3*196*196+1024 {
		t.Errorf("%d bytes for 3 full-key frames", got)
	}
	for name, data := range map[string]string{
		"PNG": solidPNG(t, 8, 8, color.NRGBA{1, 2, 3, 255}), "one frame": testGIF(t, 1),
		"broken": "R0lGODlhAAA=", "not base64": "R0lGOD!",
	} {
		if got := AnimBytes(data, 196, 196); got != 0 {
			t.Errorf("%s: %d bytes", name, got)
		}
	}
}

// PictureBox and FitSize give the size draw draws a picture at.
func TestPictureBox(t *testing.T) {
	wide := -1
	for i := range deck.KeyCount {
		if Key(i).Dx() > Key(i).Dy() {
			wide = i
		}
	}
	col := color.NRGBA{0x12, 0x34, 0x56, 0xff}
	pic := solidPNG(t, 300, 150, col)
	cfg := &deck.Config{Images: map[string]string{"pic": pic}, Icons: map[string]deck.Icon{"pic": {PNG: pic}}}
	r := New(cfg)
	for _, i := range []int{0, wide} {
		for _, image := range []bool{true, false} {
			for _, label := range []bool{false, true} {
				b := deck.Button{Icon: "pic"}
				if image {
					b = deck.Button{Image: "pic"}
				}
				if label {
					b.Label = "Go"
				}
				c := gfx.NewCanvas(Width, Height)
				r.Draw(c, Key(i), b, true, Idle)
				bw, bh := PictureBox(i, image, label)
				w, h := FitSize(300, 150, bw, bh)
				if got := count(c, Key(i), 0x123456); got != w*h {
					t.Errorf("key %d, image %v, label %v: %d pixels drawn, PictureBox says %dx%d", i, image, label, got, w, h)
				}
			}
		}
	}
}
