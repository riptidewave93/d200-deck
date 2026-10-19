package gfx

import (
	"fmt"
	"image"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// Icon draws itself in a 100 x 100 unit box mapped onto a square area.
type Icon func(p *pen)

// drawn are the icons drawn here rather than taken from Material Symbols.
var drawn = map[string]Icon{
	"play-pause":  playPause,
	"next":        next,
	"prev":        mirror(next),
	"mute":        withSpeaker(cross),
	"volume-up":   withSpeaker(plus),
	"volume-down": withSpeaker(minus),
	"github":      svgIcon(githubMark, 0.5, 0.5, 24),
	"discord":     svgIcon(discordSymbol, -10.18, -25.5, 147), // 86% wide, centred
}

// aliases are older names for Material Symbols, kept so configs using them
// still work.
var aliases = map[string]string{
	"back":            "undo",
	"browser-back":    "arrow_back",
	"browser-forward": "arrow_forward",
	"globe":           "public",
	"hang-up":         "call_end",
	"headphones-off":  "headset_off",
	"launch":          "rocket_launch",
	"mic-off":         "mic_off",
	"open-external":   "open_in_new",
	"reload":          "refresh",
	"sleep":           "bedtime",
}

type symbol struct{ name, d string }

// lookup finds an icon by name: drawn icons, aliases, then Material Symbols
// by their own names (e.g. "volume_up").
func lookup(name string) (Icon, bool) {
	if icon, ok := drawn[name]; ok {
		return icon, true
	}
	if a, ok := aliases[name]; ok {
		name = a
	}
	i := sort.Search(len(symbols), func(i int) bool { return symbols[i].name >= name })
	if i == len(symbols) || symbols[i].name != name {
		return nil, false
	}
	return svgIcon(symbols[i].d, 0, -960, 960), true
}

// HasIcon reports whether name is a built-in icon.
func HasIcon(name string) bool {
	if _, ok := drawn[name]; ok {
		return true
	}
	if _, ok := aliases[name]; ok {
		return true
	}
	i := sort.Search(len(symbols), func(i int) bool { return symbols[i].name >= name })
	return i < len(symbols) && symbols[i].name == name
}

// IconNames lists every built-in icon name, sorted.
func IconNames() []string {
	names := make([]string, 0, len(drawn)+len(aliases)+len(symbols))
	for n := range drawn {
		names = append(names, n)
	}
	for n := range aliases {
		names = append(names, n)
	}
	for _, s := range symbols {
		names = append(names, s.name)
	}
	sort.Strings(names)
	return slices.Compact(names) // an alias can share a symbol's name
}

// IconSVG is an icon as SVG: path data, filled nonzero, in a square viewBox.
type IconSVG struct {
	D       string `json:"d"`
	ViewBox string `json:"viewBox"`
}

// IconsSVG gives every built-in icon as SVG, by name, for tools that show
// the same icons as the device.
func IconsSVG() map[string]IconSVG {
	out := map[string]IconSVG{}
	for _, name := range IconNames() {
		out[name], _ = IconSVGOf(name)
	}
	return out
}

// IconSVGOf is the named icon as SVG, as the device finds it (see lookup).
func IconSVGOf(name string) (IconSVG, bool) {
	switch name {
	case "github":
		return IconSVG{githubMark, "0.5 0.5 24 24"}, true
	case "discord":
		return IconSVG{discordSymbol, "-10.18 -25.5 147 147"}, true
	}
	if icon, ok := drawn[name]; ok {
		var b strings.Builder
		icon(&pen{s: 1, rec: &b})
		return IconSVG{b.String(), "0 0 100 100"}, true
	}
	if a, ok := aliases[name]; ok {
		name = a
	}
	i := sort.Search(len(symbols), func(i int) bool { return symbols[i].name >= name })
	if i == len(symbols) || symbols[i].name != name {
		return IconSVG{}, false
	}
	return IconSVG{symbols[i].d, "0 -960 960 960"}, true
}

func num(f float32) string { return strconv.FormatFloat(float64(f), 'f', -1, 32) }

// githubMark is the GitHub mark, from Primer Octicons (mark-github-24, MIT).
const githubMark = "M12.5.75C6.146.75 1 5.896 1 12.25c0 5.089 3.292 9.387 7.863 10.91.575.101.79-.244.79-.546 0-.273-.014-1.178-.014-2.142-2.889.532-3.636-.704-3.866-1.35-.13-.331-.69-1.352-1.18-1.625-.402-.216-.977-.748-.014-.762.906-.014 1.553.834 1.769 1.179 1.035 1.74 2.688 1.25 3.349.948.1-.747.402-1.25.733-1.538-2.559-.287-5.232-1.279-5.232-5.678 0-1.25.445-2.285 1.178-3.09-.115-.288-.517-1.467.115-3.048 0 0 .963-.302 3.163 1.179.92-.259 1.897-.388 2.875-.388.977 0 1.955.13 2.875.388 2.2-1.495 3.162-1.179 3.162-1.179.633 1.581.23 2.76.115 3.048.733.805 1.179 1.825 1.179 3.09 0 4.413-2.688 5.39-5.247 5.678.417.36.776 1.05.776 2.128 0 1.538-.014 2.774-.014 3.162 0 .302.216.662.79.547C20.709 21.637 24 17.324 24 12.25 24 5.896 18.854.75 12.5.75Z"

// discordSymbol is the Discord symbol, from Discord's brand assets
// (discord.com/branding; 126.644 x 96).
const discordSymbol = "M81.15,0c-1.2376,2.1973-2.3489,4.4704-3.3591,6.794-9.5975-1.4396-19.3718-1.4396-28.9945,0-.985-2.3236-2.1216-4.5967-3.3591-6.794-9.0166,1.5407-17.8059,4.2431-26.1405,8.0568C2.779,32.5304-1.6914,56.3725.5312,79.8863c9.6732,7.1476,20.5083,12.603,32.0505,16.0884,2.6014-3.4854,4.8998-7.1981,6.8698-11.0623-3.738-1.3891-7.3497-3.1318-10.8098-5.1523.9092-.6567,1.7932-1.3386,2.6519-1.9953,20.281,9.547,43.7696,9.547,64.0758,0,.8587.7072,1.7427,1.3891,2.6519,1.9953-3.4601,2.0457-7.0718,3.7632-10.835,5.1776,1.97,3.8642,4.2683,7.5769,6.8698,11.0623,11.5419-3.4854,22.3769-8.9156,32.0509-16.0631,2.626-27.2771-4.496-50.9172-18.817-71.8548C98.9811,4.2684,90.1918,1.5659,81.1752.0505l-.0252-.0505ZM42.2802,65.4144c-6.2383,0-11.4159-5.6575-11.4159-12.6535s4.9755-12.6788,11.3907-12.6788,11.5169,5.708,11.4159,12.6788c-.101,6.9708-5.026,12.6535-11.3907,12.6535ZM84.3576,65.4144c-6.2637,0-11.3907-5.6575-11.3907-12.6535s4.9755-12.6788,11.3907-12.6788,11.4917,5.708,11.3906,12.6788c-.101,6.9708-5.026,12.6535-11.3906,12.6535Z"

// pen draws icon units into a coverage mask, or records them as SVG path
// data in rec.
type pen struct {
	m    *image.Alpha
	z    vector.Rasterizer
	s    float64 // pixels per unit
	flip bool    // mirror horizontally
	rec  *strings.Builder
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
	if p.rec != nil {
		for i := 0; i+1 < len(xy); i += 2 {
			x, y := pt(i)
			fmt.Fprintf(p.rec, "%s%s %s", map[bool]string{true: "M", false: "L"}[i == 0], num(x), num(y))
		}
		p.rec.WriteString("Z")
		return
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

// maxIconMasks bounds the icon cache; a page shows at most a few dozen.
const maxIconMasks = 128

// DrawIcon draws the named icon centred in r, size pixels square, in col.
func (c *Canvas) DrawIcon(name string, r Rect, size int, col uint32) {
	k := iconKey{name, size}
	m, ok := iconMasks[k]
	if !ok {
		icon, ok := lookup(name)
		if !ok {
			return
		}
		m = image.NewAlpha(image.Rect(0, 0, size, size))
		icon(&pen{m: m, s: float64(size) / 100})
		if len(iconMasks) >= maxIconMasks {
			clear(iconMasks)
		}
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
