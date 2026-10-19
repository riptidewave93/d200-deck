package deckd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClock(t *testing.T) {
	dir := t.TempDir()
	oldFile, oldSync, oldSet := clockFile, syncFile, setClock
	clockFile, syncFile = filepath.Join(dir, "clock"), filepath.Join(dir, "sync")
	var set []time.Time
	setClock = func(tm time.Time) error { set = append(set, tm); return nil }
	t.Cleanup(func() { clockFile, syncFile, setClock = oldFile, oldSync, oldSet })

	var k clock
	if err := k.set(time.Now()); err != nil || len(set) != 0 {
		t.Errorf("set a clock that was right: %v %v", set, err)
	}
	b, _ := os.ReadFile(clockFile)
	if n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); time.Since(time.Unix(n, 0)).Abs() > time.Minute {
		t.Errorf("saved %q", b)
	}
	if _, err := os.Stat(syncFile); err != nil {
		t.Error("sync not recorded")
	}
	later := time.Now().Add(time.Hour)
	if err := k.set(later); err != nil || len(set) != 1 || !set[0].Equal(later) {
		t.Errorf("clock not set: %v %v", set, err)
	}
	if err := k.set(time.Unix(1, 0)); err == nil {
		t.Error("took a time before the build")
	}
}
