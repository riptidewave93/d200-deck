package deck

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"strings"
	"testing"
)

// testGIF is a base64 GIF of n w x h frames, red then green.
func testGIF(t *testing.T, w, h, n int) string {
	t.Helper()
	pal := color.Palette{color.RGBA{0xff, 0, 0, 0xff}, color.RGBA{0, 0xff, 0, 0xff}}
	g := &gif.GIF{}
	for i := range n {
		f := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for j := range f.Pix {
			f.Pix[j] = uint8(i % 2)
		}
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 10)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestGIFs(t *testing.T) {
	if _, err := ImageSize(testGIF(t, 64, 64, 3)); err != nil {
		t.Errorf("GIF refused: %v", err)
	}
	for name, data := range map[string]string{
		"too many frames": testGIF(t, 8, 8, MaxFrames+1),
		"too many pixels": testGIF(t, 512, 512, 17), // under 256 KiB, 4.4 M pixels
	} {
		if _, err := ImageSize(data); err == nil || !strings.Contains(err.Error(), "frames") {
			t.Errorf("%s: %v", name, err)
		}
	}
	g, err := DecodeAnimation(testGIF(t, 16, 16, 2))
	if err != nil || g == nil || len(g.Image) != 2 {
		t.Errorf("animation: %v", err)
	}
	if g, err := DecodeAnimation(testGIF(t, 16, 16, 1)); g != nil || err != nil {
		t.Error("a single frame is animated")
	}
	if _, err := gifFrames([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00\x2c")); err == nil {
		t.Error("a broken GIF counted")
	}
}
