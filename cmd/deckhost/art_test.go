package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadArtURL(t *testing.T) {
	dir := t.TempDir()
	defer func(d string) { flatpakDir = d }(flatpakDir)
	flatpakDir = filepath.Join(dir, ".flatpak")

	own := filepath.Join(dir, "art.png")
	os.WriteFile(own, []byte("own"), 0o600)
	app := filepath.Join(flatpakDir, "org.example.Player", "tmp")
	os.MkdirAll(app, 0o700)
	os.WriteFile(filepath.Join(app, ".art-d200-test"), []byte("flatpak"), 0o600)

	for url, want := range map[string]string{
		"file://" + own:              "own",
		"file:///tmp/.art-d200-test": "flatpak", // a Flatpak player's /tmp
		"":                           "",
	} {
		if b, err := readArtURL(url); err != nil || string(b) != want {
			t.Errorf("%q: %q %v", url, b, err)
		}
	}
	for _, url := range []string{"file:///tmp/.no-such-art", "file:///tmp/../etc/.art-d200-test", "ftp://x/y"} {
		if _, err := readArtURL(url); err == nil {
			t.Errorf("%q worked", url)
		}
	}
}
