package main

import (
	"bytes"
	"os"
	"testing"
)

// The app icon's files are made from the drawing; UPDATE_ICONS=1 go test
// ./cmd/deckhost writes them again.
func TestAppIcons(t *testing.T) {
	var sizes [][]byte
	for _, s := range []int{16, 24, 32, 48, 64, 128, 256} {
		sizes = append(sizes, drawIcon(iconBlue, s))
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
