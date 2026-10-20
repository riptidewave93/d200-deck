package deckd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

// A key sends its mac_keys only while the PC is a Mac, and its keys
// otherwise; so do macro steps and built-in fallbacks.
func TestMacKeys(t *testing.T) {
	d := newTestDeckd()
	d.run.stop(time.Second)
	run, _, reports := testRunner(t)
	d.run = run
	keys := d.cfg.Pages["main"].Keys
	keys[1] = deck.Button{Action: deck.Action{Type: deck.ActionKey, Keys: []string{"ctrl", "c"}, MacKeys: []string{"gui", "c"}}}
	keys[2] = deck.Button{Action: deck.Action{Type: deck.ActionKey, Keys: []string{"ctrl", "c"}}}
	keys[3] = deck.Button{Action: deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{
		{Type: deck.ActionKey, Keys: []string{"ctrl", "a"}, MacKeys: []string{"gui", "a"}},
		{Type: deck.ActionKey, Keys: []string{"enter"}},
	}}}
	keys[4] = deck.Button{Action: deck.Action{Type: deck.ActionApp, App: deck.AppDiscord, Op: deck.DiscordMute}}
	tap := func(i int, want ...[]byte) {
		t.Helper()
		d.press(i, true)
		got := reports()
		d.press(i, false)
		expect(t, append(got, reports()...), want...)
	}
	for _, host := range []string{"", proto.HostOther, proto.HostMac} {
		d.host.set(host)
		ctrl := byte(0x01)
		if host == proto.HostMac {
			ctrl = 0x08
		}
		tap(1, kbd(ctrl, 0x06), kbd(0))
		tap(2, kbd(0x01, 0x06), kbd(0))
		tap(3, kbd(ctrl, 0x04), kbd(0), kbd(0, 0x28), kbd(0))
		tap(4, kbd(ctrl|0x02, 0x10), kbd(0))
	}
}

func TestHostOSSetting(t *testing.T) {
	usbConf = filepath.Join(t.TempDir(), "usb-gadget.conf")
	t.Cleanup(func() { usbConf = "/data/usb-gadget.conf" })
	d := newTestDeckd()
	file := filepath.Join(t.TempDir(), "host_os")
	d.host.load(file)
	get := func(payload string) (byte, string) {
		t.Helper()
		status, reply := d.settings(1, []byte(payload))
		var s proto.Settings
		json.Unmarshal(reply, &s)
		if status == proto.StatusOK && s.HostOS == nil {
			t.Fatalf("%q: no host_os in %s", payload, reply)
		}
		if s.HostOS == nil {
			return status, ""
		}
		return status, *s.HostOS
	}
	if _, v := get(""); v != "" {
		t.Errorf("default %q", v)
	}
	if status, v := get(`{"host_os": "mac"}`); status != proto.StatusOK || v != proto.HostMac || !d.host.mac() {
		t.Errorf("set mac: %d %q", status, v)
	}
	if b, _ := os.ReadFile(file); string(b) != "mac\n" {
		t.Errorf("saved %q", b)
	}
	// unchanged: not written again
	os.Remove(file)
	get(`{"host_os": "mac"}`)
	if _, err := os.Stat(file); err == nil {
		t.Error("rewritten without a change")
	}
	if status, _ := get(`{"host_os": "windows"}`); status != proto.StatusFailed || !d.host.mac() {
		t.Errorf("bad value: status %d", status)
	}
	get(`{"host_os": "other"}`)
	var again hostOS
	again.load(file)
	if again.get() != proto.HostOther {
		t.Errorf("not kept across a restart: %q", again.get())
	}
	// a value from a newer version reads as not known
	os.WriteFile(file, []byte("plan9\n"), 0o644)
	again.load(file)
	if again.get() != "" {
		t.Errorf("unknown value read as %q", again.get())
	}
}
