package deckd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/proto/prototest"
)

func TestReset(t *testing.T) {
	marker := filepath.Join(t.TempDir(), ".reset")
	rebooted := make(chan struct{}, 1)
	oldMarker, oldReboot, oldDelay := resetMarker, reboot, resetDelay
	t.Cleanup(func() { resetMarker, reboot, resetDelay = oldMarker, oldReboot, oldDelay })
	resetMarker, resetDelay = marker, 0
	reboot = func() error { rebooted <- struct{}{}; return nil }

	d := newTestDeckd()
	dev := prototest.New()
	go serve(d, dev)
	c := proto.NewConn(dev)
	defer c.Close()
	reset := func(key int) error {
		t.Helper()
		done := make(chan error, 1)
		go func() { _, err := c.DoWait(proto.CmdReset, nil, time.Minute); done <- err }()
		runUI(t, d) // shows the question
		if d.overlay == nil || d.overlay.keys[2].Label != "Erase all?" {
			t.Fatal("not asked")
		}
		d.press(5, true) // not a choice: nothing
		d.press(5, false)
		if d.overlay == nil {
			t.Fatal("another key answered")
		}
		d.press(key, true)
		d.press(key, false)
		runUI(t, d) // clears it
		return <-done
	}

	if err := reset(10); err == nil || !strings.Contains(err.Error(), "cancelled on the device") {
		t.Errorf("cancel: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("marked when cancelled")
	}

	if err := reset(13); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("no marker: %v", err)
	}
	runUI(t, d)
	if d.overlay == nil || d.overlay.keys[7].Label != "Restarting" {
		t.Error("restart not shown")
	}
	select {
	case <-rebooted:
	case <-time.After(time.Second):
		t.Error("not rebooted")
	}
}

// A marker that can't be written fails the reset, without a reboot.
func TestResetMarkerFails(t *testing.T) {
	oldMarker, oldReboot := resetMarker, reboot
	t.Cleanup(func() { resetMarker, reboot = oldMarker, oldReboot })
	resetMarker = filepath.Join(t.TempDir(), "missing", ".reset")
	reboot = func() error { t.Error("rebooted"); return nil }
	d := newTestDeckd()
	dev := prototest.New()
	go serve(d, dev)
	c := proto.NewConn(dev)
	defer c.Close()
	done := make(chan error, 1)
	go func() { _, err := c.DoWait(proto.CmdReset, nil, time.Minute); done <- err }()
	runUI(t, d)
	d.press(13, true)
	runUI(t, d)
	if err := <-done; err == nil {
		t.Error("reset without a marker")
	}
}

func TestResetListed(t *testing.T) {
	if !strings.Contains(strings.Join(deck.Features().Commands, " "), "reset") {
		t.Error("reset not in features")
	}
}
