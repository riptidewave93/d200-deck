package gfx

import (
	"image"

	"golang.org/x/image/vector"

	"github.com/riptidewave93/d200-deck/internal/svgpath"
)

// Icon is a single-colour icon: polygons filled nonzero, in a square box.
type Icon struct {
	polys        [][]svgpath.Pt
	x0, y0, size float64 // the box
}

// ParseIcon reads SVG path data d in viewBox, fitted into a square and
// centred.
func ParseIcon(d, viewBox string) (*Icon, error) {
	x, y, w, h, err := svgpath.ViewBox(viewBox)
	if err != nil {
		return nil, err
	}
	polys, err := svgpath.Parse(d)
	if err != nil {
		return nil, err
	}
	size := max(w, h)
	return &Icon{polys, x - (size-w)/2, y - (size-h)/2, size}, nil
}

// Mask draws the icon size x size pixels, as coverage.
func (ic *Icon) Mask(size int) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, size, size))
	var z vector.Rasterizer
	z.Reset(size, size)
	k := float64(size) / ic.size
	for _, poly := range ic.polys {
		pt := func(i int) (float32, float32) {
			return float32((poly[i].X - ic.x0) * k), float32((poly[i].Y - ic.y0) * k)
		}
		z.MoveTo(pt(0))
		for i := 1; i < len(poly); i++ {
			z.LineTo(pt(i))
		}
		z.ClosePath()
	}
	z.Draw(m, m.Bounds(), image.Opaque, image.Point{})
	return m
}
