package gfx

import (
	"fmt"
	"strconv"
)

// parsePath flattens SVG path data into polygons. It supports the commands
// M L H V C S Q T Z, absolute and relative.
func parsePath(d string) ([][]Pt, error) {
	toks, err := tokenize(d)
	if err != nil {
		return nil, err
	}
	var (
		polys     [][]Pt
		cur       []Pt
		x, y      float64 // current point
		sx, sy    float64 // subpath start
		cx, cy    float64 // last control point, for S and T
		cmd       byte
		lastCubic bool
		lastQuad  bool
	)
	num := func() (float64, error) {
		if len(toks) == 0 || toks[0].cmd != 0 {
			return 0, fmt.Errorf("path: missing number after %c", cmd)
		}
		v := toks[0].v
		toks = toks[1:]
		return v, nil
	}
	nums := func(n int) ([]float64, error) {
		v := make([]float64, n)
		for i := range v {
			var err error
			if v[i], err = num(); err != nil {
				return nil, err
			}
		}
		return v, nil
	}
	closeSub := func() {
		if len(cur) > 2 {
			polys = append(polys, cur)
		}
		cur = nil
	}
	cubic := func(x1, y1, x2, y2, x3, y3 float64) {
		const steps = 12
		for i := 1; i <= steps; i++ {
			t := float64(i) / steps
			u := 1 - t
			cur = append(cur, Pt{
				u*u*u*x + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x3,
				u*u*u*y + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y3,
			})
		}
		cx, cy, x, y = x2, y2, x3, y3
	}
	quad := func(x1, y1, x2, y2 float64) {
		const steps = 8
		for i := 1; i <= steps; i++ {
			t := float64(i) / steps
			u := 1 - t
			cur = append(cur, Pt{u*u*x + 2*u*t*x1 + t*t*x2, u*u*y + 2*u*t*y1 + t*t*y2})
		}
		cx, cy, x, y = x1, y1, x2, y2
	}
	for len(toks) > 0 {
		if toks[0].cmd != 0 {
			cmd = toks[0].cmd
			toks = toks[1:]
		} else if cmd == 0 {
			return nil, fmt.Errorf("path: number before any command")
		}
		rel := cmd >= 'a'
		ox, oy := 0.0, 0.0
		if rel {
			ox, oy = x, y
		}
		wasCubic, wasQuad := false, false
		switch cmd {
		case 'M', 'm':
			v, err := nums(2)
			if err != nil {
				return nil, err
			}
			closeSub()
			x, y = ox+v[0], oy+v[1]
			sx, sy = x, y
			cur = []Pt{{x, y}}
			// further pairs are line-tos
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'L', 'l':
			v, err := nums(2)
			if err != nil {
				return nil, err
			}
			x, y = ox+v[0], oy+v[1]
			cur = append(cur, Pt{x, y})
		case 'H', 'h':
			v, err := num()
			if err != nil {
				return nil, err
			}
			x = ox + v
			cur = append(cur, Pt{x, y})
		case 'V', 'v':
			v, err := num()
			if err != nil {
				return nil, err
			}
			y = oy + v
			cur = append(cur, Pt{x, y})
		case 'C', 'c':
			v, err := nums(6)
			if err != nil {
				return nil, err
			}
			cubic(ox+v[0], oy+v[1], ox+v[2], oy+v[3], ox+v[4], oy+v[5])
			wasCubic = true
		case 'S', 's':
			v, err := nums(4)
			if err != nil {
				return nil, err
			}
			x1, y1 := x, y
			if lastCubic {
				x1, y1 = 2*x-cx, 2*y-cy
			}
			cubic(x1, y1, ox+v[0], oy+v[1], ox+v[2], oy+v[3])
			wasCubic = true
		case 'Q', 'q':
			v, err := nums(4)
			if err != nil {
				return nil, err
			}
			quad(ox+v[0], oy+v[1], ox+v[2], oy+v[3])
			wasQuad = true
		case 'T', 't':
			v, err := nums(2)
			if err != nil {
				return nil, err
			}
			x1, y1 := x, y
			if lastQuad {
				x1, y1 = 2*x-cx, 2*y-cy
			}
			quad(x1, y1, ox+v[0], oy+v[1])
			wasQuad = true
		case 'Z', 'z':
			closeSub()
			x, y = sx, sy
		default:
			return nil, fmt.Errorf("path: unsupported command %c", cmd)
		}
		lastCubic, lastQuad = wasCubic, wasQuad
	}
	closeSub()
	return polys, nil
}

type token struct {
	cmd byte // 0 for a number
	v   float64
}

func tokenize(d string) ([]token, error) {
	var toks []token
	for i := 0; i < len(d); {
		c := d[i]
		switch {
		case c == ' ' || c == ',' || c == '\n' || c == '\t' || c == '\r':
			i++
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			if c == 'e' || c == 'E' {
				return nil, fmt.Errorf("path: stray exponent at %d", i)
			}
			toks = append(toks, token{cmd: c})
			i++
		default:
			// a number: sign, digits, one dot, optional exponent; a second
			// dot or a sign starts the next number (".5.5" is 0.5 0.5)
			j := i
			if d[j] == '-' || d[j] == '+' {
				j++
			}
			dot := false
			for j < len(d) && (d[j] >= '0' && d[j] <= '9' || d[j] == '.' && !dot) {
				dot = dot || d[j] == '.'
				j++
			}
			if j < len(d) && (d[j] == 'e' || d[j] == 'E') {
				j++
				if j < len(d) && (d[j] == '-' || d[j] == '+') {
					j++
				}
				for j < len(d) && d[j] >= '0' && d[j] <= '9' {
					j++
				}
			}
			v, err := strconv.ParseFloat(d[i:j], 64)
			if err != nil {
				return nil, fmt.Errorf("path: bad number %q", d[i:j])
			}
			toks = append(toks, token{v: v})
			i = j
		}
	}
	return toks, nil
}
