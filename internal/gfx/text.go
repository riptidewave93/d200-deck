package gfx

import (
	"image"
	"slices"
	"sort"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomediumitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/gofont/gosmallcaps"
	"golang.org/x/image/font/gofont/gosmallcapsitalic"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Font is a typeface. It is parsed on first use, and its faces are made per
// pixel size and kept.
type Font struct {
	data  []byte
	once  sync.Once
	f     *opentype.Font
	err   error
	faces map[int]font.Face
}

// DefaultFont is the built-in font labels use unless a button picks another.
const DefaultFont = "go-medium"

var builtinFonts = map[string]*Font{
	"go-regular":          {data: goregular.TTF},
	"go-medium":           {data: gomedium.TTF},
	"go-bold":             {data: gobold.TTF},
	"go-italic":           {data: goitalic.TTF},
	"go-medium-italic":    {data: gomediumitalic.TTF},
	"go-bold-italic":      {data: gobolditalic.TTF},
	"go-mono":             {data: gomono.TTF},
	"go-mono-bold":        {data: gomonobold.TTF},
	"go-mono-italic":      {data: gomonoitalic.TTF},
	"go-mono-bold-italic": {data: gomonobolditalic.TTF},
	"go-smallcaps":        {data: gosmallcaps.TTF},
	"go-smallcaps-italic": {data: gosmallcapsitalic.TTF},
}

// BuiltinFont returns the built-in font called name.
func BuiltinFont(name string) (*Font, bool) {
	f, ok := builtinFonts[name]
	return f, ok
}

// FontNames lists the built-in fonts, sorted.
func FontNames() []string {
	names := make([]string, 0, len(builtinFonts))
	for n := range builtinFonts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ParseFont reads a TrueType or OpenType font.
func ParseFont(data []byte) (*Font, error) {
	f := &Font{data: data}
	return f, f.load()
}

func (f *Font) load() error {
	f.once.Do(func() { f.f, f.err = opentype.Parse(f.data) })
	return f.err
}

// maxFaces bounds the faces kept per font; labels use a few sizes.
const maxFaces = 32

func (f *Font) face(px int) font.Face {
	if err := f.load(); err != nil {
		panic(err) // built-in fonts parse; ParseFont checked the others
	}
	if fc, ok := f.faces[px]; ok {
		return fc
	}
	fc, err := opentype.NewFace(f.f, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	if f.faces == nil || len(f.faces) >= maxFaces {
		f.faces = map[int]font.Face{}
	}
	f.faces[px] = fc
	return fc
}

// Metrics are the font's ascent and descent in pixels at px pixels per em.
func (f *Font) Metrics(px int) (ascent, descent int) {
	m := f.face(px).Metrics()
	return m.Ascent.Ceil(), m.Descent.Ceil()
}

// Missing are the characters of s, other than spaces and line breaks, that
// f has no glyph for.
func (f *Font) Missing(s string) []rune {
	if f.load() != nil {
		return nil
	}
	var buf sfnt.Buffer
	var missing []rune
	for _, c := range s {
		if c == ' ' || c == '\n' || slices.Contains(missing, c) {
			continue
		}
		if i, err := f.f.GlyphIndex(&buf, c); err != nil || i == 0 {
			missing = append(missing, c)
		}
	}
	return missing
}

// TextWidth is the width of s in pixels at px pixels per em.
func (f *Font) TextWidth(s string, px int) int {
	return font.MeasureString(f.face(px), s).Ceil()
}

type textKey struct {
	f  *Font
	s  string
	px int
}

var textMasks = map[textKey]*image.Alpha{}

// maxTextMasks bounds the text cache; labels are short and few per page.
const maxTextMasks = 256

// DrawText draws s in font f centred in r at px pixels per em, in col.
func (c *Canvas) DrawText(f *Font, s string, r Rect, px int, col uint32) {
	k := textKey{f, s, px}
	m, ok := textMasks[k]
	if !ok {
		fc := f.face(px)
		fm := fc.Metrics()
		w := font.MeasureString(fc, s).Ceil()
		m = image.NewAlpha(image.Rect(0, 0, w, (fm.Ascent + fm.Descent).Ceil()))
		d := font.Drawer{Dst: m, Src: image.Opaque, Face: fc, Dot: fixed.Point26_6{Y: fm.Ascent}}
		d.DrawString(s)
		if len(textMasks) >= maxTextMasks {
			clear(textMasks)
		}
		textMasks[k] = m
	}
	b := m.Bounds()
	c.DrawMask(m, image.Pt(r.Min.X+(r.Dx()-b.Dx())/2, r.Min.Y+(r.Dy()-b.Dy())/2), col)
}
