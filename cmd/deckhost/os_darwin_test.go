package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartAtLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := startAtLogin(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "Library", "LaunchAgents", launchAgent+".plist")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if err := xml.Unmarshal(b, new(struct{})); err != nil || !strings.Contains(string(b), "<string>"+exe+"</string>") {
		t.Errorf("%v\n%s", err, b)
	}
	// left alone when unchanged, so a job turned off stays off
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
	startAtLogin()
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(old) {
		t.Error("rewritten unchanged")
	}
	if b := launchAgentPlist("/Apps/A&B.app/Contents/MacOS/deckhost"); !strings.Contains(string(b), "A&amp;B.app") {
		t.Errorf("not escaped:\n%s", b)
	}
}
