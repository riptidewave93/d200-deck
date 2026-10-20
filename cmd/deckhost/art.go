package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/riptidewave93/d200-deck/proto"
)

// artSize is the album art's size: a key's.
const artSize = 196

// maxArtFile bounds an album art file read.
const maxArtFile = 8 << 20

// keyJPEG scales album art to fill a key, cropping it square, as a JPEG.
func keyJPEG(raw []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > 16<<20 {
		return nil, fmt.Errorf("%dx%d is too big", cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	sb := src.Bounds()
	side := min(sb.Dx(), sb.Dy())
	crop := image.Rect(0, 0, side, side).Add(sb.Min).Add(image.Pt((sb.Dx()-side)/2, (sb.Dy()-side)/2))
	dst := image.NewRGBA(image.Rect(0, 0, artSize, artSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	for q := 85; ; q -= 15 {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, dst, &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		if b.Len() <= proto.MaxHostImage || q <= 40 {
			return b.Bytes(), nil
		}
	}
}

var artClient = &http.Client{Timeout: 5 * time.Second}

// readArtURL reads album art from a player's URL: a file, or http(s).
func readArtURL(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	var r io.ReadCloser
	switch u.Scheme {
	case "file":
		if r, err = os.Open(u.Path); err != nil {
			return nil, err
		}
	case "http", "https":
		resp, err := artClient.Get(s)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("%s: %s", u.Host, resp.Status)
		}
		r = resp.Body
	default:
		return nil, errors.New("art URL isn't a file or http(s)")
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, maxArtFile+1))
	if err == nil && len(b) > maxArtFile {
		err = errors.New("art file too big")
	}
	return b, err
}
