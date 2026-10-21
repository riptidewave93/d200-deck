package deckd

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/internal/ota"
	"github.com/riptidewave93/d200-deck/render"
)

func TestUpdateScreens(t *testing.T) {
	for name, o := range map[string]*overlay{
		"confirm":  confirmScreen(ota.ConfirmRequest{Version: "v", Flavour: "debug"}, func(bool) {}),
		"older":    confirmScreen(ota.ConfirmRequest{Version: "v", Flavour: "debug", Older: true}, func(bool) {}),
		"progress": progressScreen("Updating", "Kernel", 2, 4),
		"failed":   progressScreen(ota.StepFailed, "", 0, 0),
	} {
		for k, b := range o.keys {
			if _, ok := o.drawn[k]; ok {
				t.Errorf("%s: key %d drawn twice", name, k)
			}
			if _, ok := systemIcons[b.Icon]; b.Icon != "" && !ok {
				t.Errorf("%s key %d: no icon %q", name, k, b.Icon)
			}
		}
	}
	if _, ok := progressScreen(ota.StepFailed, "", 0, 0).drawn[13]; ok {
		t.Error("warning shown after a failed install")
	}
}

func TestUpdateScreen(t *testing.T) {
	d := newTestDeckd()
	d.drawPage()
	sock := filepath.Join(t.TempDir(), "update.sock")
	go d.serveUpdates(sock)
	var c net.Conn
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	waitFor("socket", func() bool {
		var err error
		c, err = net.Dial("unix", sock)
		return err == nil
	})
	if fi, _ := os.Stat(sock); fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode %v", fi.Mode())
	}
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)

	enc.Encode(ota.UIRequest{Confirm: &ota.ConfirmRequest{Version: "abc1234", Flavour: "debug", Older: true}})
	runUI(t, d)
	if got := keyColour(d, 13); got != 0x2e7d32 {
		t.Errorf("install key %#x", got)
	}
	if got := keyColour(d, 7); got != 0xf9a825 {
		t.Errorf("older warning %#x", got)
	}
	d.press(0, true) // other keys do nothing
	d.press(0, false)
	if d.pages[len(d.pages)-1] != d.cfg.Home {
		t.Error("a key ran its action under the update screen")
	}
	d.press(13, true)
	var r ota.UIReply
	if err := dec.Decode(&r); err != nil || !r.OK {
		t.Fatalf("reply %+v %v", r, err)
	}

	enc.Encode(ota.UIRequest{Progress: "System 4/4"})
	runUI(t, d)
	if got := keyColour(d, 13); got != 0xf9a825 {
		t.Errorf("progress screen %#x", got)
	}
	c.Close()
	runUI(t, d)
	if d.overlay != nil || keyColour(d, 13) != render.Colours[render.Idle] {
		t.Error("update screen still shown")
	}

	// Cancel
	c, _ = net.Dial("unix", sock)
	json.NewEncoder(c).Encode(ota.UIRequest{Confirm: &ota.ConfirmRequest{Version: "x"}})
	runUI(t, d)
	d.press(10, true)
	if err := json.NewDecoder(c).Decode(&r); err != nil || r.OK {
		t.Errorf("cancel: %+v %v", r, err)
	}
	runUI(t, d)
	if d.overlay != nil {
		t.Error("still shown after cancel")
	}
	c.Close()

	// nobody answers
	old := confirmTimeout
	confirmTimeout = 10 * time.Millisecond
	t.Cleanup(func() { confirmTimeout = old })
	c, _ = net.Dial("unix", sock)
	defer c.Close()
	json.NewEncoder(c).Encode(ota.UIRequest{Confirm: &ota.ConfirmRequest{Version: "x"}})
	runUI(t, d)
	if err := json.NewDecoder(c).Decode(&r); err != nil || r.OK {
		t.Errorf("timeout: %+v %v", r, err)
	}
	runUI(t, d)

	ready := filepath.Join(t.TempDir(), "ready")
	writeReady(ready)
	if b, _ := os.ReadFile(ready); string(b) != strconv.Itoa(os.Getpid()) {
		t.Errorf("ready file %q", b)
	}
}
