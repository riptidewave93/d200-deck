package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStartInScope(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	fake := filepath.Join(dir, "systemd-run")
	os.WriteFile(fake, []byte("#!/bin/sh\necho \"$@\" > "+out+"\n"), 0o755)
	old := systemdRun
	systemdRun = fake
	t.Cleanup(func() { systemdRun = old })

	if err := start("true", "x"); err != nil {
		t.Fatal(err)
	}
	truePath, _ := exec.LookPath("true")
	want := "--user --scope --quiet -- " + truePath + " x\n"
	for range 100 {
		if b, _ := os.ReadFile(out); string(b) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := os.ReadFile(out)
	t.Errorf("systemd-run got %q, want %q", b, want)
}
