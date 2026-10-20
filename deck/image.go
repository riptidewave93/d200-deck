package deck

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg" // custom icons may be JPEG
	_ "image/png"
	"strconv"

	"golang.org/x/image/font/sfnt"
)

// ParseColor parses "#rrggbb" into 0x00RRGGBB.
func ParseColor(s string) (uint32, error) {
	if len(s) != 7 || s[0] != '#' {
		return 0, fmt.Errorf("colour %q is not #rrggbb", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("colour %q is not #rrggbb", s)
	}
	return uint32(v), nil
}

// decode decodes base64 data of at most limit bytes.
func decode(data string, limit int) ([]byte, error) {
	if base64.StdEncoding.DecodedLen(len(data)) > limit+2 {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, errors.New("not base64")
	}
	if len(b) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return b, nil
}

// FontBytes decodes a custom font (base64 TrueType or OpenType).
func FontBytes(data string) ([]byte, error) { return decode(data, MaxFontBytes) }

// CheckFont checks that a custom font decodes and parses.
func CheckFont(data string) error {
	b, err := FontBytes(data)
	if err != nil {
		return err
	}
	if _, err := sfnt.Parse(b); err != nil {
		return errors.New("not a TrueType or OpenType font")
	}
	return nil
}

// Animated GIF limits: frames, and their pixels in all, before decoding.
const (
	MaxFrames    = 100
	MaxGIFPixels = 4 << 20
)

// MaxAnimBytes bounds the frames of the animations on a page, as the device
// keeps them: a byte a pixel at the size drawn, 4 for a GIF of more than 256
// colours. Past it, a GIF shows its first frame.
const MaxAnimBytes = 8 << 20

// ImageSize checks a custom image (base64 PNG, JPEG or GIF) from its header
// (a GIF's frames too) and returns its size.
func ImageSize(data string) (image.Point, error) {
	b, err := decode(data, MaxImageBytes)
	if err != nil {
		return image.Point{}, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return image.Point{}, errors.New("not a PNG, JPEG or GIF image")
	}
	if format != "png" && format != "jpeg" && format != "gif" {
		return image.Point{}, fmt.Errorf("%s images aren't supported", format)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageSide || cfg.Height > MaxImageSide {
		return image.Point{}, fmt.Errorf("%dx%d pixels; at most %dx%d", cfg.Width, cfg.Height, MaxImageSide, MaxImageSide)
	}
	if format == "gif" {
		// a small file can hold many frames: count them before decoding
		n, err := gifFrames(b)
		if err != nil {
			return image.Point{}, err
		}
		if n > MaxFrames || cfg.Width*cfg.Height*n > MaxGIFPixels {
			return image.Point{}, fmt.Errorf("%d frames of %dx%d; at most %d frames, %d pixels in all", n, cfg.Width, cfg.Height, MaxFrames, MaxGIFPixels)
		}
	}
	return image.Pt(cfg.Width, cfg.Height), nil
}

// gifFrames counts a GIF's frames from its block structure.
func gifFrames(b []byte) (int, error) {
	bad := errors.New("broken GIF")
	if len(b) < 13 {
		return 0, bad
	}
	i := 13
	if b[10]&0x80 != 0 { // global colour table
		i += 3 << (b[10]&7 + 1)
	}
	// subBlocks skips data sub-blocks up to their terminator
	subBlocks := func() bool {
		for i < len(b) {
			n := int(b[i])
			i += 1 + n
			if n == 0 {
				return true
			}
		}
		return false
	}
	n := 0
	for i < len(b) {
		switch b[i] {
		case 0x21: // extension: label, then sub-blocks
			i += 2
			if !subBlocks() {
				return 0, bad
			}
		case 0x2c: // image: descriptor, colour table, LZW code size, sub-blocks
			if i+10 > len(b) {
				return 0, bad
			}
			flags := b[i+9]
			i += 10
			if flags&0x80 != 0 {
				i += 3 << (flags&7 + 1)
			}
			i++
			if !subBlocks() {
				return 0, bad
			}
			n++
		case 0x3b: // trailer
			return n, nil
		default:
			return 0, bad
		}
	}
	return n, nil // no trailer: decoders take what's there
}

// DecodeAnimation decodes data if it's an animated GIF; nil if it's another
// image or a single frame.
func DecodeAnimation(data string) (*gif.GIF, error) {
	if _, err := ImageSize(data); err != nil {
		return nil, err
	}
	b, _ := decode(data, MaxImageBytes)
	if !bytes.HasPrefix(b, []byte("GIF")) {
		return nil, nil
	}
	g, err := gif.DecodeAll(bytes.NewReader(b))
	if err != nil || len(g.Image) < 2 {
		return nil, err
	}
	return g, nil
}

// DecodeImage decodes a custom image (base64 PNG, JPEG or GIF; a GIF's first
// frame).
func DecodeImage(data string) (image.Image, error) {
	if _, err := ImageSize(data); err != nil {
		return nil, err
	}
	b, _ := decode(data, MaxImageBytes)
	img, _, err := image.Decode(bytes.NewReader(b))
	return img, err
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}
