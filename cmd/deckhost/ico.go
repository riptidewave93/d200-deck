package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
)

// icoFromPNG wraps a PNG in an .ico file (one image, stored as PNG).
func icoFromPNG(p []byte) []byte {
	w, h := 0, 0
	if cfg, err := png.DecodeConfig(bytes.NewReader(p)); err == nil {
		w, h = cfg.Width, cfg.Height
	}
	side := func(n int) byte {
		if n >= 256 {
			return 0 // 0 means 256
		}
		return byte(n)
	}
	b := make([]byte, 22, 22+len(p))
	binary.LittleEndian.PutUint16(b[2:], 1) // type: icon
	binary.LittleEndian.PutUint16(b[4:], 1) // one image
	b[6], b[7] = side(w), side(h)
	binary.LittleEndian.PutUint16(b[10:], 1)  // planes
	binary.LittleEndian.PutUint16(b[12:], 32) // bits per pixel
	binary.LittleEndian.PutUint32(b[14:], uint32(len(p)))
	binary.LittleEndian.PutUint32(b[18:], 22) // offset of the image
	return append(b, p...)
}
