package deckd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/proto"
)

func TestUSBDriveFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usb-gadget.conf")
	for _, c := range []struct {
		file string
		on   bool
	}{
		{"", true},
		{"drive=0\n", false},
		{"drive=1\n", true},
		{"drive=0\ndrive=1\n", true}, // the last one wins
		{"drive=off\n", true},        // only an exact 0 turns it off
		{"other=x\n  drive=0\n", false},
	} {
		os.WriteFile(p, []byte(c.file), 0o644)
		if got := usbDrive(p); got != c.on {
			t.Errorf("%q: %v", c.file, got)
		}
	}
	if !usbDrive(filepath.Join(t.TempDir(), "none")) {
		t.Error("no file: off")
	}

	os.WriteFile(p, []byte("addr=198.18.0.0/30\ndrive=1\n# note\n"), 0o644)
	if err := setConf(p, "drive", "0"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "addr=198.18.0.0/30\n# note\ndrive=0\n" {
		t.Errorf("rewrote %q", b)
	}
}

func TestSettings(t *testing.T) {
	usbConf = filepath.Join(t.TempDir(), "usb-gadget.conf")
	applied := make(chan struct{}, 2)
	oldApply, oldDelay := applyUSB, rebindDelay
	applyUSB, rebindDelay = func() error { applied <- struct{}{}; return nil }, 0
	t.Cleanup(func() { applyUSB, rebindDelay, usbConf = oldApply, oldDelay, "/data/usb-gadget.conf" })
	d := newTestDeckd()

	get := func(payload string) bool {
		t.Helper()
		status, reply := d.settings(1, []byte(payload))
		var s proto.Settings
		if status != proto.StatusOK || json.Unmarshal(reply, &s) != nil || s.USBDrive == nil {
			t.Fatalf("%q: status %d, reply %q", payload, status, reply)
		}
		return *s.USBDrive
	}
	if !get("") {
		t.Error("drive off by default")
	}
	if get(`{"usb_drive": false}`) {
		t.Error("drive still on")
	}
	select {
	case <-applied:
	case <-time.After(time.Second):
		t.Fatal("not applied")
	}
	get(`{"usb_drive": false}`) // no change, no re-plug
	get(`{}`)
	select {
	case <-applied:
		t.Error("applied without a change")
	case <-time.After(50 * time.Millisecond):
	}
	if status, _ := d.settings(1, []byte("{")); status != proto.StatusBadRequest {
		t.Errorf("bad JSON: %d", status)
	}
}

func TestUSBNet4(t *testing.T) {
	for s, ok := range map[string]bool{
		"10.77.0.4": true, "192.168.7.1": true, "223.1.1.1": true,
		"": false, "127.0.0.1": false, "0.1.2.3": false, "224.0.0.1": false, "10.077.0.4": false,
		"10.77.0": false, "10.77.0.256": false, "10.77.0.x": false, "+10.77.0.4": false,
	} {
		if validNet4(s) != ok {
			t.Errorf("%q: %v", s, !ok)
		}
	}

	usbConf = filepath.Join(t.TempDir(), "usb-gadget.conf")
	applied := make(chan struct{}, 4)
	oldApply, oldDelay := applyUSB, rebindDelay
	applyUSB, rebindDelay = func() error { applied <- struct{}{}; return nil }, 0
	t.Cleanup(func() {
		for range 2 { // set and cleared: wait for both re-plugs
			<-applied
		}
		applyUSB, rebindDelay, usbConf = oldApply, oldDelay, "/data/usb-gadget.conf"
	})
	os.WriteFile(usbConf, []byte("drive=0\nnet4=bad\n"), 0o644)
	d := newTestDeckd()
	type answer struct {
		status byte
		b      []byte
	}
	replies := make(chan answer, 1)
	d.reply = func(_ uint16, status byte, b []byte) { replies <- answer{status, b} }

	// set changes settings, pressing Change if the device asks
	set := func(payload string) (byte, proto.Settings) {
		status, reply := d.settings(1, []byte(payload))
		if status == statusLater {
			runUI(t, d)
			d.press(13, true)
			runUI(t, d)
			a := <-replies
			status, reply = a.status, a.b
		}
		var s proto.Settings
		json.Unmarshal(reply, &s)
		return status, s
	}
	if _, s := set(""); *s.USBNet4 != "" || *s.USBDrive {
		t.Errorf("an invalid net4 isn't the default: %+v", s)
	}
	if _, s := set(`{"usb_net4": "10.77.0.4"}`); *s.USBNet4 != "10.77.0.4" {
		t.Errorf("not set: %q", *s.USBNet4)
	}
	if b, _ := os.ReadFile(usbConf); string(b) != "drive=0\nnet4=10.77.0.4\n" {
		t.Errorf("file %q", b)
	}
	if status, _ := set(`{"usb_net4": "127.0.0.1"}`); status != proto.StatusFailed {
		t.Errorf("127.0.0.1 taken: %d", status)
	}
	if _, s := set(`{"usb_net4": ""}`); *s.USBNet4 != "" {
		t.Errorf("not cleared: %q", *s.USBNet4)
	}
	if b, _ := os.ReadFile(usbConf); string(b) != "drive=0\n" {
		t.Errorf("file %q", b)
	}
}

func TestUSBNet(t *testing.T) {
	dir := t.TempDir()
	ser, conf := filepath.Join(dir, "serial-number"), filepath.Join(dir, "usb-gadget.conf")
	os.WriteFile(ser, []byte("00112233445566AA\x00"), 0o644)
	want := "DEV_IP4=198.19.23.5\nPEER_IP4=198.19.23.6\nDEV_IP6=fdc1:8f70:cc5e::1\nNET6=fdc1:8f70:cc5e::/64\n"
	if got, err := usbNet(ser, conf); err != nil || got != want {
		t.Errorf("%q %v", got, err)
	}
	// IPv6 in its canonical form (RFC 5952): no leading zeros
	os.WriteFile(ser, []byte("c0ffee0003"), 0o644)
	if got, _ := usbNet(ser, conf); !strings.Contains(got, "DEV_IP6=fd9b:b65:8c1b::1\n") {
		t.Errorf("%q", got)
	}
	os.WriteFile(ser, []byte("00112233445566AA\x00"), 0o644)
	os.WriteFile(conf, []byte("drive=1\nnet4=10.77.0.6\n"), 0o644)
	want = "DEV_IP4=10.77.0.5\nPEER_IP4=10.77.0.6\nDEV_IP6=fdc1:8f70:cc5e::1\nNET6=fdc1:8f70:cc5e::/64\n"
	if got, err := usbNet(ser, conf); err != nil || got != want {
		t.Errorf("net4: %q %v", got, err)
	}
	os.WriteFile(conf, []byte("net4=127.0.0.1\n"), 0o644)
	os.Remove(ser)
	want = "DEV_IP4=172.31.254.1\nPEER_IP4=172.31.254.2\nDEV_IP6=\nNET6=\n"
	if got, err := usbNet(ser, conf); err != nil || got != want {
		t.Errorf("no serial: %q %v", got, err)
	}
	os.WriteFile(ser, []byte("not hex"), 0o644)
	if _, err := usbNet(ser, conf); err == nil {
		t.Error("a bad serial passed")
	}
}
