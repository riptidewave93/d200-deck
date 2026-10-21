package main

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

// The app icon's files are made from the drawing; UPDATE_ICONS=1 go test
// ./cmd/deckhost writes them again.
func TestAppIcons(t *testing.T) {
	var sizes [][]byte
	for _, s := range []int{16, 24, 32, 48, 64, 128, 256} {
		sizes = append(sizes, drawIcon(iconBlue, s, s >= detailedFrom))
	}
	for path, want := range map[string][]byte{
		"../../contrib/icons/deckhost.ico": icoFromPNG(sizes...),
		"../../contrib/icons/deckhost.svg": []byte(iconSVG(iconBlue)),
	} {
		if os.Getenv("UPDATE_ICONS") != "" {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, want) {
			t.Errorf("%s is out of date: UPDATE_ICONS=1 go test ./cmd/deckhost", path)
		}
	}
}

func TestIcon(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(drawIcon(iconBlue, trayIconSize, false)))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("icon is %v", b)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("corner isn't transparent")
	}
	if r, g, b, a := img.At(16, 24).RGBA(); a>>8 != 0xff || r>>8 != 0xff || g>>8 != 0xff || b>>8 != 0xff {
		t.Errorf("key pixel %v", img.At(16, 24))
	}
	if r, _, b, _ := img.At(4, 32).RGBA(); r>>8 != 0x2f || b>>8 != 0xde {
		t.Errorf("body pixel %v", img.At(4, 32))
	}
}
