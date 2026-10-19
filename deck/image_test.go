package deck

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
)

// testImage is a w x h image encoded as format ("png", "jpeg" or "gif"),
// base64-encoded as in a config.
func testImage(t *testing.T, format string, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	img.Set(0, 0, color.NRGBA{0xff, 0, 0, 0xff})
	var b bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&b, img)
	case "jpeg":
		err = jpeg.Encode(&b, img, nil)
	case "gif":
		err = gif.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestParseColor(t *testing.T) {
	if v, err := ParseColor("#2f6FDe"); err != nil || v != 0x2f6fde {
		t.Errorf("%#x, %v", v, err)
	}
	for _, bad := range []string{"", "2f6fde", "#2f6fd", "#2f6fdex", "#gggggg", "#-1ffff"} {
		if _, err := ParseColor(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestImages(t *testing.T) {
	png1 := testImage(t, "png", 64, 32)
	jpg := testImage(t, "jpeg", 196, 196)
	if p, err := ImageSize(png1); err != nil || p != image.Pt(64, 32) {
		t.Errorf("png: %v, %v", p, err)
	}
	if img, err := DecodeImage(jpg); err != nil || img.Bounds().Dx() != 196 {
		t.Errorf("jpeg: %v", err)
	}
	bad := map[string]string{
		"not base64":   "%%%",
		"not an image": base64.StdEncoding.EncodeToString([]byte("hello")),
		"gif":          testImage(t, "gif", 8, 8),
		"too wide":     testImage(t, "png", MaxImageSide+1, 8),
		"too big":      base64.StdEncoding.EncodeToString(make([]byte, MaxImageBytes+1)),
		"too big b64":  strings.Repeat("A", 4*MaxImageBytes),
	}
	for name, data := range bad {
		if _, err := ImageSize(data); err == nil {
			t.Errorf("%s accepted", name)
		}
		if _, err := DecodeImage(data); err == nil {
			t.Errorf("%s decoded", name)
		}
	}
}

func TestValidateIcons(t *testing.T) {
	img := testImage(t, "png", 16, 16)
	good := func() *Config {
		c := DefaultConfig()
		c.Images = map[string]string{"logo.v2": img}
		c.Pages[c.Home].Keys[9] = Button{Image: "logo.v2", Label: "Two\nlines", Color: "#ffffff", Background: "#123abc"}
		return c
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Config){
		"missing image":  func(c *Config) { c.Pages[c.Home].Keys[9] = Button{Image: "nope"} },
		"icon and image": func(c *Config) { c.Pages[c.Home].Keys[9] = Button{Icon: "mute", Image: "logo.v2"} },
		"bad colour":     func(c *Config) { c.Pages[c.Home].Keys[9] = Button{Color: "red"} },
		"bad background": func(c *Config) { c.Pages[c.Home].Keys[9] = Button{Background: "#12"} },
		"four lines":     func(c *Config) { c.Pages[c.Home].Keys[9] = Button{Label: "a\nb\nc\nd"} },
		"bad image":      func(c *Config) { c.Images["broken"] = "AAAA" },
		"bad id":         func(c *Config) { c.Images["has space"] = img },
		"empty id":       func(c *Config) { c.Images[""] = img },
		"long id":        func(c *Config) { c.Images[strings.Repeat("x", 65)] = img },
	}
	for name, change := range bad {
		c := good()
		change(c)
		if c.Validate() == nil {
			t.Errorf("%s: valid", name)
		}
	}
}

func TestLoadTooBig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.json")
	os.WriteFile(p, bytes.Repeat([]byte(" "), MaxConfigBytes+1), 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("oversized config: %v", err)
	}
}

func TestFonts(t *testing.T) {
	ttf := base64.StdEncoding.EncodeToString(gomono.TTF)
	good := func() *Config {
		c := DefaultConfig()
		c.Fonts = map[string]string{"mono": ttf}
		c.Pages[c.Home].Keys[9] = Button{Label: "x", Font: "mono"}
		c.Pages[c.Home].Keys[11] = Button{Label: "y", Font: "go-bold"}
		return c
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Config){
		"missing font":    func(c *Config) { c.Pages[c.Home].Keys[9] = Button{Font: "nope"} },
		"not a font":      func(c *Config) { c.Fonts["junk"] = base64.StdEncoding.EncodeToString([]byte("junk")) },
		"not base64":      func(c *Config) { c.Fonts["junk"] = "%%%" },
		"too big":         func(c *Config) { c.Fonts["big"] = strings.Repeat("A", 4*MaxFontBytes) },
		"shadows builtin": func(c *Config) { c.Fonts["go-bold"] = ttf },
		"bad id":          func(c *Config) { c.Fonts["a b"] = ttf },
	}
	for name, change := range bad {
		c := good()
		change(c)
		if c.Validate() == nil {
			t.Errorf("%s: valid", name)
		}
	}
}
