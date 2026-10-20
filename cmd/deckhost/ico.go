package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
)

// icoFromPNG wraps PNGs, each a size of one image, in an .ico file.
func icoFromPNG(pngs ...[]byte) []byte {
	side := func(n int) byte {
		if n >= 256 {
			return 0 // 0 means 256
		}
		return byte(n)
	}
	head := 6 + 16*len(pngs)
	b := make([]byte, head)
	binary.LittleEndian.PutUint16(b[2:], 1) // type: icon
	binary.LittleEndian.PutUint16(b[4:], uint16(len(pngs)))
	for i, p := range pngs {
		w, h := 0, 0
		if cfg, err := png.DecodeConfig(bytes.NewReader(p)); err == nil {
			w, h = cfg.Width, cfg.Height
		}
		e := b[6+16*i:]
		e[0], e[1] = side(w), side(h)
		binary.LittleEndian.PutUint16(e[4:], 1)  // planes
		binary.LittleEndian.PutUint16(e[6:], 32) // bits per pixel
		binary.LittleEndian.PutUint32(e[8:], uint32(len(p)))
		binary.LittleEndian.PutUint32(e[12:], uint32(len(b)))
		b = append(b, p...)
	}
	return b
}
