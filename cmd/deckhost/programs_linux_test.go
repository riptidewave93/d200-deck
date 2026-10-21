package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindDesktop(t *testing.T) {
	home, sys := t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)
	t.Setenv("XDG_DATA_DIRS", sys)
	write := func(dir, id, body string) {
		os.MkdirAll(filepath.Join(dir, "applications"), 0o755)
		if err := os.WriteFile(filepath.Join(dir, "applications", id+".desktop"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(sys, "org.example.App", "[Desktop Entry]\nType=Application\nExec=sh -c true %U\n")
	write(sys, "org.example.Gone", "[Desktop Entry]\nType=Application\nExec=no-such-program-here\n")
	write(sys, "org.example.Try", "[Desktop Entry]\nType=Application\nExec=sh\nTryExec=no-such-program-here\n")
	write(sys, "org.example.Hidden", "[Desktop Entry]\nType=Application\nExec=sh\n")
	write(home, "org.example.Hidden", "[Desktop Entry]\nType=Application\nExec=sh\nHidden=true\n")

	if findDesktop("org.example.App") == nil {
		t.Error("not found")
	}
	for _, id := range []string{"org.example.Gone", "org.example.Try", "org.example.Hidden", "org.example.None"} {
		if findDesktop(id) != nil {
			t.Errorf("%s: found", id)
		}
	}
	if findProgram([]candidate{{kind: "desktop", value: "org.example.None"}, {kind: "run", value: "sh", args: []string{"~"}}}) == nil {
		t.Error("sh not found")
	}
	dirs := applicationDirs()
	if dirs[0] != filepath.Join(home, "applications") || dirs[1] != filepath.Join(sys, "applications") {
		t.Errorf("%q", dirs)
	}
}
