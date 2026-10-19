package deckd

import (
	"image"
	"log"
	"runtime/debug"
	"strings"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
)

type keyState int

const (
	keyIdle keyState = iota
	keyPressed
	keyError
	keyIdentify
)

// Colours (0x00RRGGBB).
var keyColours = [...]uint32{keyIdle: 0x1c1c1c, keyPressed: 0x2f6fde, keyError: 0xc62828, keyIdentify: 0xf9a825}

const fgColour = 0xe8e8e8

// assets supplies a config's custom icons, scaled to fit w x h, and fonts.
type assets interface {
	image(id string, w, h int) *image.RGBA
	font(name string) *gfx.Font
}

// drawSafely draws b like drawButton. If that panics (say, on a bad custom
// font or image), it marks b's font and image broken and draws b without
// them, or leaves the key blank if that panics too.
func drawSafely(c *gfx.Canvas, r gfx.Rect, b deck.Button, ok bool, state keyState, ac *assetCache) {
	if try(func() { drawButton(c, r, b, ok, state, ac) }) == nil {
		return
	}
	ac.broken(b)
	b.Font, b.Image = "", ""
	if try(func() { drawButton(c, r, b, ok, state, ac) }) != nil {
		c.Fill(r, keyColours[keyIdle])
	}
}

// try runs f, returning (and logging) what it panicked with, if anything.
func try(f func()) (p any) {
	defer func() {
		if p = recover(); p != nil {
			log.Printf("drawing a key: %v\n%s", p, debug.Stack())
		}
	}()
	f()
	return nil
}

// drawButton draws button b (blank if !ok) into key area r.
func drawButton(c *gfx.Canvas, r gfx.Rect, b deck.Button, ok bool, state keyState, a assets) {
	if !ok {
		c.Fill(r, 0)
		return
	}
	bg, fg := colours(b, state)
	c.Fill(r, bg)
	f := a.font(b.Font)

	// icon draws the icon or custom image in a size x size square centred in ir
	icon := func(ir gfx.Rect, size int) {
		if b.Image == "" {
			c.DrawIcon(b.Icon, ir, size, fg)
		} else if img := a.image(b.Image, size, size); img != nil {
			c.DrawImage(img, ir)
		}
	}
	hasIcon := b.Image != "" || gfx.HasIcon(b.Icon)
	label := strings.ReplaceAll(b.Label, "\n", " ")
	switch {
	case hasIcon && label != "" && r.Dx() > r.Dy():
		// wide key: icon then label, centred together
		const iconSize, gap, margin = 120, 28, 24
		px := fitText(f, label, r.Dx()-2*margin-iconSize-gap, 44)
		w := iconSize + gap + f.TextWidth(label, px)
		x := r.Min.X + (r.Dx()-w)/2
		icon(image.Rect(x, r.Min.Y, x+iconSize, r.Max.Y), iconSize)
		c.DrawText(f, label, image.Rect(x+iconSize+gap, r.Min.Y, x+w, r.Max.Y), px, fg)
	case hasIcon && label != "":
		icon(image.Rect(r.Min.X, r.Min.Y+12, r.Max.X, r.Max.Y-44), 100)
		lr := image.Rect(r.Min.X+8, r.Max.Y-52, r.Max.X-8, r.Max.Y-8)
		c.DrawText(f, label, lr, fitText(f, label, lr.Dx(), 30), fg)
	case b.Image != "":
		// a custom image alone fills the key
		if img := a.image(b.Image, r.Dx(), r.Dy()); img != nil {
			c.DrawImage(img, r)
		}
	case hasIcon:
		icon(r, 120)
	case b.Label != "":
		drawLines(c, f, r, strings.Split(b.Label, "\n"), fg)
	}
}

// colours are the key's background and foreground in state.
func colours(b deck.Button, state keyState) (bg, fg uint32) {
	bg, fg = keyColours[keyIdle], fgColour
	custom, err := deck.ParseColor(b.Background)
	if err == nil {
		bg = custom
	}
	if v, err := deck.ParseColor(b.Color); err == nil {
		fg = v
	}
	switch {
	case state == keyPressed && b.Background != "":
		bg = pressed(bg)
	case state != keyIdle:
		bg = keyColours[state]
	}
	return bg, fg
}

// pressed is a custom background when pressed: a third of the way to white,
// or to black for light colours.
func pressed(c uint32) uint32 {
	r, g, b := c>>16&0xff, c>>8&0xff, c&0xff
	toward := uint32(255)
	if (r*299+g*587+b*114)/1000 > 160 {
		toward = 0
	}
	mix := func(v uint32) uint32 { return uint32(int(v) + (int(toward)-int(v))/3) }
	return mix(r)<<16 | mix(g)<<8 | mix(b)
}

// drawLines draws a label of one or more lines centred in r.
func drawLines(c *gfx.Canvas, f *gfx.Font, r gfx.Rect, lines []string, fg uint32) {
	px := min(36, (r.Dy()-16)*4/5/len(lines))
	for _, l := range lines {
		px = fitText(f, l, r.Dx()-16, px)
	}
	lineH := px * 5 / 4
	y := r.Min.Y + (r.Dy()-lineH*len(lines))/2
	for _, l := range lines {
		c.DrawText(f, l, image.Rect(r.Min.X, y, r.Max.X, y+lineH), px, fg)
		y += lineH
	}
}

// assetCache decodes and scales a config's custom icons and fonts on first
// use, and keeps them for as long as the config (icons up to maxCachedPixels).
type assetCache struct {
	cfg       *deck.Config
	images    map[imageKey]*image.RGBA
	pixels    int // in images
	badImages map[string]bool
	fonts     map[string]*gfx.Font // nil: broken, use the default
}

// maxCachedPixels bounds the scaled icons kept (4 bytes each): a few pages of
// full-key images.
const maxCachedPixels = 1 << 20

type imageKey struct {
	id   string
	w, h int
}

func (ac *assetCache) image(id string, w, h int) *image.RGBA {
	if ac.badImages[id] {
		return nil
	}
	k := imageKey{id, w, h}
	if img, ok := ac.images[k]; ok {
		return img
	}
	if ac.images == nil {
		ac.images = map[imageKey]*image.RGBA{}
	}
	var scaled *image.RGBA
	if img, err := deck.DecodeImage(ac.cfg.Images[id]); err != nil {
		log.Printf("image %q: %v", id, err)
	} else {
		scaled = gfx.Fit(img, w, h)
		n := scaled.Bounds().Dx() * scaled.Bounds().Dy()
		if ac.pixels+n > maxCachedPixels {
			clear(ac.images)
			ac.pixels = 0
		}
		ac.pixels += n
	}
	ac.images[k] = scaled
	return scaled
}

// font returns the named font, or the default if it's empty or broken.
func (ac *assetCache) font(name string) *gfx.Font {
	if f, ok := gfx.BuiltinFont(name); ok {
		return f
	}
	f, ok := ac.fonts[name]
	if !ok && name != "" {
		b, err := deck.FontBytes(ac.cfg.Fonts[name])
		if err == nil {
			f, err = gfx.ParseFont(b)
		}
		if err != nil {
			log.Printf("font %q: %v", name, err)
			f = nil
		}
		if ac.fonts == nil {
			ac.fonts = map[string]*gfx.Font{}
		}
		ac.fonts[name] = f
	}
	if f == nil {
		f, _ = gfx.BuiltinFont(gfx.DefaultFont)
	}
	return f
}

// reset drops everything cached if the config changed.
func (ac *assetCache) reset(cfg *deck.Config) {
	if cfg == ac.cfg {
		return
	}
	ac.cfg = cfg
	clear(ac.fonts)
	clear(ac.images)
	clear(ac.badImages)
	ac.pixels = 0
}

// broken stops b's custom font and image being used again.
func (ac *assetCache) broken(b deck.Button) {
	if _, builtin := gfx.BuiltinFont(b.Font); !builtin && b.Font != "" {
		if ac.fonts == nil {
			ac.fonts = map[string]*gfx.Font{}
		}
		ac.fonts[b.Font] = nil
	}
	if b.Image != "" {
		if ac.badImages == nil {
			ac.badImages = map[string]bool{}
		}
		ac.badImages[b.Image] = true
	}
}

// fitText is the largest text size up to px at which s fits in width.
func fitText(f *gfx.Font, s string, width, px int) int {
	for px > 12 && f.TextWidth(s, px) > width {
		px -= 2
	}
	return px
}

// Test pattern key colours, one per key.
var testColours = [deck.KeyCount]uint32{
	0xe53935, 0xfb8c00, 0xfdd835, 0x43a047, 0x1e88e5,
	0x8e24aa, 0x00acc1, 0xd81b60, 0x7cb342, 0x5e35b1,
	0x6d4c41, 0x546e7a, 0xf4511e, 0x00897b,
}

// drawTestKey draws key i as a numbered box with 8 px white/black edge bands,
// to check the key window positions: count the bands visible on each side.
func drawTestKey(c *gfx.Canvas, i int, r gfx.Rect, pressed bool) {
	for k := 0; k < 4; k++ {
		c.Fill(r.Inset(k*8), []uint32{0xffffff, 0}[k%2])
	}
	bg := testColours[i]
	if pressed {
		bg = 0xffffff
	}
	c.Fill(r.Inset(32), bg)
	c.DrawNumber(i, r, 16, 0)
}

// drawGrid draws grey lines every 100 px, brighter every 500 px.
func drawGrid(c *gfx.Canvas) {
	for x := 0; x < c.W; x += 100 {
		col := uint32(0x404040)
		if x%500 == 0 {
			col = 0xa0a0a0
		}
		c.Fill(image.Rect(x, 0, x+2, c.H), col)
	}
	for y := 0; y < c.H; y += 100 {
		col := uint32(0x404040)
		if y%500 == 0 {
			col = 0xa0a0a0
		}
		c.Fill(image.Rect(0, y, c.W, y+2), col)
	}
}
