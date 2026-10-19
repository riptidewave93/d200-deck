package deck

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
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

// ImageSize checks a custom icon (base64 PNG or JPEG) from its header and
// returns its size.
func ImageSize(data string) (image.Point, error) {
	b, err := decode(data, MaxImageBytes)
	if err != nil {
		return image.Point{}, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return image.Point{}, errors.New("not a PNG or JPEG image")
	}
	if format != "png" && format != "jpeg" {
		return image.Point{}, fmt.Errorf("%s images aren't supported", format)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageSide || cfg.Height > MaxImageSide {
		return image.Point{}, fmt.Errorf("%dx%d pixels; at most %dx%d", cfg.Width, cfg.Height, MaxImageSide, MaxImageSide)
	}
	return image.Pt(cfg.Width, cfg.Height), nil
}

// DecodeImage decodes a custom icon (base64 PNG or JPEG).
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
