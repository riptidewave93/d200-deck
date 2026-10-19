package gfx

import (
	"image"
	"math"

	"golang.org/x/image/vector"
)

// Icon draws itself in a 100 x 100 unit box mapped onto a square area.
type Icon func(p *pen)

// Icons are the built-in icons, by name.
var Icons = map[string]Icon{
	"play-pause":  playPause,
	"next":        next,
	"prev":        mirror(next),
	"mute":        withSpeaker(cross),
	"volume-up":   withSpeaker(plus),
	"volume-down": withSpeaker(minus),
	"github":      svgIcon(githubMark, 0.5, 0.5, 24),
}

// githubMark is the GitHub mark, from Primer Octicons (mark-github-24, MIT).
const githubMark = "M12.5.75C6.146.75 1 5.896 1 12.25c0 5.089 3.292 9.387 7.863 10.91.575.101.79-.244.79-.546 0-.273-.014-1.178-.014-2.142-2.889.532-3.636-.704-3.866-1.35-.13-.331-.69-1.352-1.18-1.625-.402-.216-.977-.748-.014-.762.906-.014 1.553.834 1.769 1.179 1.035 1.74 2.688 1.25 3.349.948.1-.747.402-1.25.733-1.538-2.559-.287-5.232-1.279-5.232-5.678 0-1.25.445-2.285 1.178-3.09-.115-.288-.517-1.467.115-3.048 0 0 .963-.302 3.163 1.179.92-.259 1.897-.388 2.875-.388.977 0 1.955.13 2.875.388 2.2-1.495 3.162-1.179 3.162-1.179.633 1.581.23 2.76.115 3.048.733.805 1.179 1.825 1.179 3.09 0 4.413-2.688 5.39-5.247 5.678.417.36.776 1.05.776 2.128 0 1.538-.014 2.774-.014 3.162 0 .302.216.662.79.547C20.709 21.637 24 17.324 24 12.25 24 5.896 18.854.75 12.5.75Z"

// pen draws icon units into a coverage mask.
type pen struct {
	m    *image.Alpha
	z    vector.Rasterizer
	s    float64 // pixels per unit
	flip bool    // mirror horizontally
}

// poly adds a filled polygon, given as x, y pairs.
func (p *pen) poly(xy ...float64) {
	pt := func(i int) (float32, float32) {
		x := xy[i]
		if p.flip {
			x = 100 - x
		}
		return float32(x * p.s), float32(xy[i+1] * p.s)
	}
	b := p.m.Bounds()
	p.z.Reset(b.Dx(), b.Dy())
	p.z.MoveTo(pt(0))
	for i := 2; i+1 < len(xy); i += 2 {
		p.z.LineTo(pt(i))
	}
	p.z.ClosePath()
	p.z.Draw(p.m, b, image.Opaque, image.Point{}) // Over: overlapping polygons union
}

func (p *pen) rect(x0, y0, x1, y1 float64) { p.poly(x0, y0, x1, y0, x1, y1, x0, y1) }

// line is a thick line from (x0,y0) to (x1,y1), w units wide.
func (p *pen) line(x0, y0, x1, y1, w float64) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	nx, ny := -dy/l*w/2, dx/l*w/2
	p.poly(x0+nx, y0+ny, x1+nx, y1+ny, x1-nx, y1-ny, x0-nx, y0-ny)
}

type iconKey struct {
	name string
	size int
}

var iconMasks = map[iconKey]*image.Alpha{}

// DrawIcon draws the named icon centred in r, size pixels square, in col.
func (c *Canvas) DrawIcon(name string, r Rect, size int, col uint32) {
	k := iconKey{name, size}
	m, ok := iconMasks[k]
	if !ok {
		icon, ok := Icons[name]
		if !ok {
			return
		}
		m = image.NewAlpha(image.Rect(0, 0, size, size))
		icon(&pen{m: m, s: float64(size) / 100})
		iconMasks[k] = m
	}
	c.DrawMask(m, image.Pt(r.Min.X+(r.Dx()-size)/2, r.Min.Y+(r.Dy()-size)/2), col)
}

// svgIcon is an icon from SVG path data (nonzero fill) whose square viewport
// starts at (x0, y0) and is size units wide.
func svgIcon(d string, x0, y0, size float64) Icon {
	polys, err := parsePath(d)
	if err != nil {
		panic(err)
	}
	k := 100 / size
	return func(p *pen) {
		b := p.m.Bounds()
		p.z.Reset(b.Dx(), b.Dy())
		for _, poly := range polys {
			pt := func(i int) (float32, float32) {
				return float32((poly[i].X - x0) * k * p.s), float32((poly[i].Y - y0) * k * p.s)
			}
			p.z.MoveTo(pt(0))
			for i := 1; i < len(poly); i++ {
				p.z.LineTo(pt(i))
			}
			p.z.ClosePath()
		}
		p.z.Draw(p.m, b, image.Opaque, image.Point{})
	}
}

func mirror(icon Icon) Icon {
	return func(p *pen) { p.flip = !p.flip; icon(p); p.flip = !p.flip }
}

func playPause(p *pen) {
	p.poly(8, 22, 8, 78, 50, 50)
	p.rect(60, 22, 71, 78)
	p.rect(81, 22, 92, 78)
}

func next(p *pen) {
	p.poly(10, 22, 10, 78, 44, 50)
	p.poly(44, 22, 44, 78, 78, 50)
	p.rect(78, 22, 89, 78)
}

func withSpeaker(mark Icon) Icon {
	return func(p *pen) {
		p.rect(6, 37, 24, 63)
		p.poly(24, 37, 46, 18, 46, 82, 24, 63)
		mark(p)
	}
}

func cross(p *pen) {
	p.line(60, 36, 88, 64, 9)
	p.line(88, 36, 60, 64, 9)
}

func plus(p *pen) {
	p.rect(58, 45.5, 92, 54.5)
	p.rect(70.5, 33, 79.5, 67)
}

func minus(p *pen) { p.rect(58, 45.5, 92, 54.5) }
