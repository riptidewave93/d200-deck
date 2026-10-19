// Package render draws the deck's keys, as the device shows them. The
// configuration website runs it as WebAssembly for its previews.
package render

import (
	"image"
	"log"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
)

// Screen size: the panel is mounted rotated, and drawn as landscape.
const (
	Width  = 1280
	Height = 720
)

// keys are the key windows on the screen: a 5x3 grid of 196x196 windows, the
// wide key spanning the two bottom-right cells (458x196). Positions are
// centred on the windows by eye with deckd -test.
var keys = func() (r [deck.KeyCount]image.Rectangle) {
	colX := [5]int{16, 275, 534, 793, 1052}
	rowY := [3]int{1, 262, 524}
	const size = 196
	for i := range 13 {
		x, y := colX[i%5], rowY[i/5]
		r[i] = image.Rect(x, y, x+size, y+size)
	}
	r[13] = image.Rect(793, 523, 1251, 719)
	return r
}()

// Key is key i's window on the screen.
func Key(i int) image.Rectangle { return keys[i] }

// State is how a key is shown.
type State int

const (
	Idle State = iota
	Pressed
	Error    // an action failed
	Identify // flashing to be found
)

// Colours are the key backgrounds in each state (0x00RRGGBB).
var Colours = [...]uint32{Idle: 0x1c1c1c, Pressed: 0x2f6fde, Error: 0xc62828, Identify: 0xf9a825}

// Foreground is the default icon and label colour.
const Foreground = 0xe8e8e8

// Renderer draws a config's buttons. It decodes and scales the config's
// images and fonts on first use, and keeps them (images up to
// maxCachedPixels). It is not safe for concurrent use.
type Renderer struct {
	// Extra are icons for screens the device draws itself, not configs.
	Extra map[string]deck.Icon
	// Now is the time animated GIFs are drawn at; nil is time.Now. They
	// start when the config is set.
	Now func() time.Time

	start      time.Time
	next       time.Time // when the key drawn last changes, if it's animated
	anims      map[imageKey]*anim
	animPixels int
	page       int // counts NewPage calls

	cfg       *deck.Config
	masks     map[imageKey]*image.Alpha // path icons, drawn
	images    map[imageKey]*image.RGBA
	pixels    int // in images
	badImages map[string]bool
	fonts     map[string]*gfx.Font // nil: broken, use the default
}

// maxCachedPixels bounds the scaled images kept (4 bytes each): a few pages
// of full-key images.
const maxCachedPixels = 1 << 20

// MaxAnimPixels bounds the frames of the animations on a page, as drawn
// (frames x width x height, 4 bytes each); past it, a GIF shows its first
// frame. A full-key GIF (196 px) fits with 54 frames.
const MaxAnimPixels = 2 << 20

// anim is a GIF's frames as drawn; a is nil if it isn't animated, or was
// refused (no room on its page).
type anim struct {
	a       *gfx.Animation
	refused bool
	page    int // the page it was last drawn on
}

type imageKey struct {
	id   string
	w, h int
}

// New returns a renderer for cfg's buttons.
func New(cfg *deck.Config) *Renderer { return &Renderer{cfg: cfg, start: time.Now()} }

// SetConfig switches to cfg, dropping what's cached if it's a different one.
func (r *Renderer) SetConfig(cfg *deck.Config) {
	if cfg == r.cfg {
		return
	}
	r.cfg = cfg
	r.start = r.now()
	clear(r.anims)
	r.animPixels = 0
	clear(r.masks)
	clear(r.fonts)
	clear(r.images)
	clear(r.badImages)
	r.pixels = 0
}

// Key draws button b on key i, as shown when its action's state is on. Keys
// without a button are black.
func (r *Renderer) Key(i int, b deck.Button, on bool) *image.RGBA {
	if i < 0 || i >= len(keys) {
		return nil
	}
	c := gfx.NewCanvas(keys[i].Dx(), keys[i].Dy())
	r.Draw(c, image.Rect(0, 0, c.W, c.H), b.Shown(on), true, Idle)
	return c.Image()
}

// Draw draws button b (black if !ok) into area of c. If that panics, say on a
// bad custom font or image, it marks them broken and draws b without them,
// or leaves the key blank if that panics too.
func (r *Renderer) Draw(c *gfx.Canvas, area image.Rectangle, b deck.Button, ok bool, s State) {
	r.next = time.Time{}
	if try(func() { r.draw(c, area, b, ok, s) }) == nil {
		return
	}
	r.broken(b)
	b.Font, b.Image = "", ""
	if try(func() { r.draw(c, area, b, ok, s) }) != nil {
		c.Fill(area, Colours[Idle])
	}
}

// NewPage says the screen now shows another page: animations not drawn since
// may be dropped to make room for its own.
func (r *Renderer) NewPage() { r.page++ }

// Next is when the key Draw drew last changes, if it shows an animated GIF;
// zero if it doesn't.
func (r *Renderer) Next() time.Time { return r.next }

func (r *Renderer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
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

func (r *Renderer) draw(c *gfx.Canvas, area image.Rectangle, b deck.Button, ok bool, s State) {
	if !ok {
		c.Fill(area, 0)
		return
	}
	bg, fg := colours(b, s)
	c.Fill(area, bg)
	f := r.font(b.Font)

	// icon draws the icon or custom image in a size x size square centred in ir
	icon := func(ir image.Rectangle, size int) {
		if b.Image == "" {
			r.drawIcon(c, b.Icon, ir, size, fg)
		} else if img := r.image(b.Image, size, size); img != nil {
			c.DrawImage(img, ir)
		}
	}
	_, known := r.icon(b.Icon)
	hasIcon := b.Image != "" || known
	label := strings.ReplaceAll(b.Label, "\n", " ")
	switch {
	case hasIcon && label != "" && area.Dx() > area.Dy():
		// wide key: icon then label, centred together
		const iconSize, gap, margin = 120, 28, 24
		px := FitText(f, label, area.Dx()-2*margin-iconSize-gap, 44)
		w := iconSize + gap + f.TextWidth(label, px)
		x := area.Min.X + (area.Dx()-w)/2
		icon(image.Rect(x, area.Min.Y, x+iconSize, area.Max.Y), iconSize)
		c.DrawText(f, label, image.Rect(x+iconSize+gap, area.Min.Y, x+w, area.Max.Y), px, fg)
	case hasIcon && label != "":
		icon(image.Rect(area.Min.X, area.Min.Y+12, area.Max.X, area.Max.Y-44), 100)
		lr := image.Rect(area.Min.X+8, area.Max.Y-52, area.Max.X-8, area.Max.Y-8)
		c.DrawText(f, label, lr, FitText(f, label, lr.Dx(), 30), fg)
	case b.Image != "":
		// a custom image alone fills the key
		if img := r.image(b.Image, area.Dx(), area.Dy()); img != nil {
			c.DrawImage(img, area)
		}
	case hasIcon:
		icon(area, 120)
	case b.Label != "":
		drawLines(c, f, area, strings.Split(b.Label, "\n"), fg)
	}
}

// colours are the key's background and foreground in state s.
func colours(b deck.Button, s State) (bg, fg uint32) {
	bg, fg = Colours[Idle], Foreground
	if v, err := deck.ParseColor(b.Background); err == nil {
		bg = v
	}
	if v, err := deck.ParseColor(b.Color); err == nil {
		fg = v
	}
	switch {
	case s == Pressed && b.Background != "":
		bg = pressed(bg)
	case s != Idle:
		bg = Colours[s]
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

// drawLines draws a label of one or more lines centred in area.
func drawLines(c *gfx.Canvas, f *gfx.Font, area image.Rectangle, lines []string, fg uint32) {
	px := min(36, (area.Dy()-16)*4/5/len(lines))
	for _, l := range lines {
		px = FitText(f, l, area.Dx()-16, px)
	}
	lineH := px * 5 / 4
	y := area.Min.Y + (area.Dy()-lineH*len(lines))/2
	for _, l := range lines {
		c.DrawText(f, l, image.Rect(area.Min.X, y, area.Max.X, y+lineH), px, fg)
		y += lineH
	}
}

// FitText is the largest text size up to px at which s fits in width.
func FitText(f *gfx.Font, s string, width, px int) int {
	for px > 12 && f.TextWidth(s, px) > width {
		px -= 2
	}
	return px
}

// icon finds an icon: the config's, then Extra. A key naming one that's
// in neither shows no icon.
func (r *Renderer) icon(name string) (deck.Icon, bool) {
	if name == "" {
		return deck.Icon{}, false
	}
	if r.cfg != nil {
		if ic, ok := r.cfg.Icons[name]; ok {
			return ic, true
		}
	}
	ic, ok := r.Extra[name]
	return ic, ok
}

// maxMasks bounds the path icons kept drawn; a page shows at most a few.
const maxMasks = 128

// drawIcon draws the named icon centred in ir, size pixels square: a path
// icon in fg, a picture as it is.
func (r *Renderer) drawIcon(c *gfx.Canvas, name string, ir image.Rectangle, size int, fg uint32) {
	ic, ok := r.icon(name)
	if !ok {
		return
	}
	if ic.PNG != "" {
		if img := r.picture("\x00"+name, ic.PNG, size, size); img != nil {
			c.DrawImage(img, ir)
		}
		return
	}
	k := imageKey{name, size, size}
	m, ok := r.masks[k]
	if !ok {
		if p, err := gfx.ParseIcon(ic.D, ic.ViewBox); err != nil {
			log.Printf("icon %q: %v", name, err)
		} else {
			m = p.Mask(size)
		}
		if r.masks == nil || len(r.masks) >= maxMasks {
			r.masks = map[imageKey]*image.Alpha{}
		}
		r.masks[k] = m
	}
	if m != nil {
		c.DrawMask(m, image.Pt(ir.Min.X+(ir.Dx()-size)/2, ir.Min.Y+(ir.Dy()-size)/2), fg)
	}
}

// image is custom image id scaled to fit w x h, or nil if it's broken.
func (r *Renderer) image(id string, w, h int) *image.RGBA {
	if r.badImages[id] || r.cfg == nil {
		return nil
	}
	return r.picture(id, r.cfg.Images[id], w, h)
}

// picture is base64 image data, cached as id, scaled to fit w x h; nil if
// it's broken. An animated GIF gives the frame for now.
func (r *Renderer) picture(id, data string, w, h int) *image.RGBA {
	k := imageKey{id, w, h}
	if a := r.animation(k, data); a != nil {
		now := r.now()
		f, wait := a.At(now.Sub(r.start))
		if next := now.Add(wait); r.next.IsZero() || next.Before(r.next) {
			r.next = next
		}
		return f
	}
	if img, ok := r.images[k]; ok {
		return img
	}
	if r.images == nil {
		r.images = map[imageKey]*image.RGBA{}
	}
	var scaled *image.RGBA
	if img, err := deck.DecodeImage(data); err != nil {
		log.Printf("image %q: %v", id, err)
	} else {
		scaled = gfx.Fit(img, w, h)
		n := scaled.Bounds().Dx() * scaled.Bounds().Dy()
		if r.pixels+n > maxCachedPixels {
			clear(r.images)
			r.pixels = 0
		}
		r.pixels += n
	}
	r.images[k] = scaled
	return scaled
}

// animation is data's frames scaled to fit k's size if it's an animated GIF
// and they fit in MaxAnimPixels; nil otherwise.
func (r *Renderer) animation(k imageKey, data string) *gfx.Animation {
	if !strings.HasPrefix(data, "R0lGOD") { // "GIF8" in base64
		return nil
	}
	if e, ok := r.anims[k]; ok && (!e.refused || e.page == r.page) {
		e.page = r.page
		return e.a
	}
	if r.anims == nil {
		r.anims = map[imageKey]*anim{}
	}
	e := &anim{page: r.page}
	if g, err := deck.DecodeAnimation(data); err != nil {
		log.Printf("image %q: %v", k.id, err)
	} else if g != nil {
		e.a = gfx.NewAnimation(g, k.w, k.h)
		if r.animPixels+e.a.Pixels() > MaxAnimPixels {
			r.dropOtherPages()
		}
		if r.animPixels+e.a.Pixels() > MaxAnimPixels {
			log.Printf("image %q: too many animated frames on this page; showing the first", k.id)
			e.a, e.refused = nil, true
		} else {
			r.animPixels += e.a.Pixels()
		}
	}
	r.anims[k] = e
	return e.a
}

// dropOtherPages drops the animations not drawn on this page.
func (r *Renderer) dropOtherPages() {
	for k, e := range r.anims {
		if e.page != r.page {
			if e.a != nil {
				r.animPixels -= e.a.Pixels()
			}
			delete(r.anims, k)
		}
	}
}

// MissingGlyphs are the characters in b's labels (its own and its on
// look's) that their font lacks; the device draws them as boxes.
func (r *Renderer) MissingGlyphs(b deck.Button) []rune {
	var missing []rune
	for _, on := range []bool{false, true} {
		s := b.Shown(on)
		for _, c := range r.font(s.Font).Missing(s.Label) {
			if !slices.Contains(missing, c) {
				missing = append(missing, c)
			}
		}
	}
	return missing
}

// font returns the named font, or the default if it's empty or broken.
func (r *Renderer) font(name string) *gfx.Font {
	if f, ok := gfx.BuiltinFont(name); ok {
		return f
	}
	f, ok := r.fonts[name]
	if !ok && name != "" {
		b, err := deck.FontBytes(r.cfg.Fonts[name])
		if err == nil {
			f, err = gfx.ParseFont(b)
		}
		if err != nil {
			log.Printf("font %q: %v", name, err)
			f = nil
		}
		if r.fonts == nil {
			r.fonts = map[string]*gfx.Font{}
		}
		r.fonts[name] = f
	}
	if f == nil {
		f, _ = gfx.BuiltinFont(gfx.DefaultFont)
	}
	return f
}

// broken stops b's custom font and image being used again.
func (r *Renderer) broken(b deck.Button) {
	if _, builtin := gfx.BuiltinFont(b.Font); !builtin && b.Font != "" {
		if r.fonts == nil {
			r.fonts = map[string]*gfx.Font{}
		}
		r.fonts[b.Font] = nil
	}
	if b.Image != "" {
		if r.badImages == nil {
			r.badImages = map[string]bool{}
		}
		r.badImages[b.Image] = true
	}
}
