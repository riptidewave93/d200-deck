package deckd

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

func editingSettings(t *testing.T, d *deckd, payload string) bool {
	t.Helper()
	status, reply := d.settings(1, []byte(payload))
	var s proto.Settings
	if status != proto.StatusOK || json.Unmarshal(reply, &s) != nil || s.Editing == nil {
		t.Fatalf("%s: status %d %s", payload, status, reply)
	}
	return *s.Editing
}

// While editing, presses run nothing: no keys sent, no page change, no PC
// action. Answering the device's own questions still works.
func TestEditing(t *testing.T) {
	usbConf = filepath.Join(t.TempDir(), "usb-gadget.conf")
	t.Cleanup(func() { usbConf, editingTimeout = "/data/usb-gadget.conf", proto.EditingTimeout })
	d := newTestDeckd()
	d.run.stop(time.Second)
	run, _, reports := testRunner(t)
	d.run = run
	keys := d.cfg.Pages["main"].Keys
	keys[1] = deck.Button{Action: deck.Action{Type: deck.ActionKey, Keys: []string{"f13"}}}
	keys[2] = deck.Button{Action: deck.Action{Type: deck.ActionPage, Page: "browser"}}
	d.pc.fail = func(int) {}
	d.pc.heartbeat(proto.ActionHost{ID: "h"}) // so PC actions would be sent
	tap := func(i int) {
		d.press(i, true)
		expect(t, reports()) // nothing held down
		d.press(i, false)
	}

	if editingSettings(t, d, "") {
		t.Error("editing by default")
	}
	if !editingSettings(t, d, `{"editing": true}`) {
		t.Fatal("not editing")
	}
	tap(1)
	tap(2)
	d.press(13, true) // Configure Buttons: a PC action
	if h := d.held[13]; h == nil || h.pcID != 0 {
		t.Error("PC action sent while editing")
	}
	d.press(13, false)
	expect(t, reports())
	if d.page() != "main" {
		t.Errorf("page changed while editing: %q", d.page())
	}

	// a question on the device still takes its answer
	answered := false
	d.setOverlay(askScreen("t", "d", "OK", func(yes bool) { answered = yes }))
	d.press(13, true)
	d.press(13, false)
	if !answered {
		t.Error("question not answered while editing")
	}
	d.setOverlay(nil)

	if editingSettings(t, d, `{"editing": false}`) {
		t.Error("still editing")
	}
	d.press(1, true)
	expect(t, reports(), kbd(0, 0x68))
	d.press(1, false)
	expect(t, reports(), kbd(0))

	// it ends by itself
	editingTimeout = 20 * time.Millisecond
	editingSettings(t, d, `{"editing": true}`)
	for deadline := time.Now().Add(5 * time.Second); d.editing.on.Load(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("editing didn't time out")
		}
	}
	if editingSettings(t, d, "") {
		t.Error("reply says editing after the timeout")
	}
	tap(2)
	if d.page() != "browser" {
		t.Error("keys don't work after the timeout")
	}

	// and when the PC sleeps
	editingTimeout = time.Minute
	editingSettings(t, d, `{"editing": true}`)
	d.setAsleep(true)
	d.setAsleep(false)
	if d.editing.on.Load() {
		t.Error("still editing after the PC slept")
	}
}
