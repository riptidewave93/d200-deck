package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// deckhost's icon: white keys on a rounded body, drawn on a 64 unit grid.
// Small, it's 3x2 big keys; from detailedFrom pixels up, the device's 5x3
// with its wide bottom-right key. The tray shows the small one blue while a
// device is connected and grey while not; the app icon is blue.
const (
	iconBlue     = 0x2f6fde
	trayIconSize = 64
	detailedFrom = 48
)

type iconRect struct{ x0, y0, x1, y1, r float64 }

// iconShapes are the body, then the keys.
func iconShapes(detailed bool) []iconRect {
	if !detailed {
		shapes := []iconRect{{2, 10, 62, 54, 9}}
		for row := range 2 {
			for col := range 3 {
				x, y := 9+float64(col)*16, 17+float64(row)*16
				shapes = append(shapes, iconRect{x, y, x + 14, y + 14, 3})
			}
		}
		return shapes
	}
	const margin, gap, key = 4.5, 2.0, 8.6
	top := 32 - (3*key+2*gap+2*margin)/2
	shapes := []iconRect{{2, top, 62, 64 - top, 7}}
	for row := range 3 {
		for col := range 5 {
			if row == 2 && col == 4 {
				continue // the wide key covers it
			}
			x, y := 2+margin+float64(col)*(key+gap), top+margin+float64(row)*(key+gap)
			w := key
			if row == 2 && col == 3 {
				w = 2*key + gap
			}
			shapes = append(shapes, iconRect{x, y, x + w, y + key, 2})
		}
	}
	return shapes
}

// drawIcon draws the icon size pixels square with a body of colour body, as
// a PNG: the detailed one or the small one.
func drawIcon(body uint32, size int, detailed bool) []byte {
	const ss = 4 // samples per pixel per axis
	scale := float64(size) / 64
	bodyRGBA := color.RGBA{uint8(body >> 16), uint8(body >> 8), uint8(body), 0xff}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	shapes := iconShapes(detailed)
	inside := func(s iconRect, x, y float64) bool {
		cx := min(max(x, s.x0+s.r), s.x1-s.r)
		cy := min(max(y, s.y0+s.r), s.y1-s.r)
		return x >= s.x0 && x < s.x1 && y >= s.y0 && y < s.y1 && (x-cx)*(x-cx)+(y-cy)*(y-cy) <= s.r*s.r
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for py := range size {
		for px := range size {
			var nBody, nKey int
			for sy := range ss {
				for sx := range ss {
					x := (float64(px) + (float64(sx)+0.5)/ss) / scale
					y := (float64(py) + (float64(sy)+0.5)/ss) / scale
					switch {
					case !inside(shapes[0], x, y):
					case func() bool {
						for _, k := range shapes[1:] {
							if inside(k, x, y) {
								return true
							}
						}
						return false
					}():
						nKey++
					default:
						nBody++
					}
				}
			}
			n := nBody + nKey
			if n == 0 {
				continue
			}
			// colour: mix of body and key samples; alpha: coverage
			mix := func(a, b uint8) uint8 { return uint8((int(a)*nBody + int(b)*nKey) / n) }
			a := n * 255 / (ss * ss)
			img.SetRGBA(px, py, color.RGBA{
				uint8(int(mix(bodyRGBA.R, white.R)) * a / 255),
				uint8(int(mix(bodyRGBA.G, white.G)) * a / 255),
				uint8(int(mix(bodyRGBA.B, white.B)) * a / 255),
				uint8(a),
			})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// iconSVG is the icon as SVG, for desktops that scale it.
func iconSVG(body uint32) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">` + "\n")
	for i, s := range iconShapes(true) {
		fill := "#ffffff"
		if i == 0 {
			fill = fmt.Sprintf("#%06x", body)
		}
		fmt.Fprintf(&b, `<rect x="%.4g" y="%.4g" width="%.4g" height="%.4g" rx="%.4g" fill="%s"/>`+"\n",
			s.x0, s.y0, s.x1-s.x0, s.y1-s.y0, s.r, fill)
	}
	b.WriteString("</svg>\n")
	return b.String()
}
