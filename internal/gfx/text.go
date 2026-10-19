package gfx

import (
	"image"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	fontOnce sync.Once
	fontData *opentype.Font
	faces    = map[int]font.Face{}
)

func face(px int) font.Face {
	fontOnce.Do(func() {
		var err error
		if fontData, err = opentype.Parse(gomedium.TTF); err != nil {
			panic(err)
		}
	})
	if f, ok := faces[px]; ok {
		return f
	}
	f, err := opentype.NewFace(fontData, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	faces[px] = f
	return f
}

// TextWidth is the width of s in pixels at px pixels per em.
func TextWidth(s string, px int) int {
	return font.MeasureString(face(px), s).Ceil()
}

type textKey struct {
	s  string
	px int
}

var textMasks = map[textKey]*image.Alpha{}

// DrawText draws s centred in r at px pixels per em, in col.
func (c *Canvas) DrawText(s string, r Rect, px int, col uint32) {
	k := textKey{s, px}
	m, ok := textMasks[k]
	if !ok {
		f := face(px)
		fm := f.Metrics()
		w := font.MeasureString(f, s).Ceil()
		m = image.NewAlpha(image.Rect(0, 0, w, (fm.Ascent + fm.Descent).Ceil()))
		d := font.Drawer{Dst: m, Src: image.Opaque, Face: f, Dot: fixed.Point26_6{Y: fm.Ascent}}
		d.DrawString(s)
		textMasks[k] = m
	}
	b := m.Bounds()
	c.DrawMask(m, image.Pt(r.Min.X+(r.Dx()-b.Dx())/2, r.Min.Y+(r.Dy()-b.Dy())/2), col)
}
