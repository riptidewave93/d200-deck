package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riptidewave93/d200-deck/proto"
)

func TestDescribe(t *testing.T) {
	hello := func(serial, version string) *proto.Hello {
		h := &proto.Hello{Serial: serial}
		h.OS.Version, h.OS.Flavour = version, "debug"
		return h
	}
	cases := []struct {
		s       Status
		running bool
		header  string
		lines   []string
	}{
		{Status{}, false, "deckhost isn't running", nil},
		{Status{}, true, "D200 not connected", nil},
		{Status{Devices: []Device{{Path: "/dev/hidraw1", Hello: hello("00112233445566aa", "v1")}}}, true,
			"D200 connected", []string{"OS v1 (debug)"}},
		{Status{Devices: []Device{
			{Path: "/dev/hidraw1", Hello: hello("00112233445566aa", "v1")},
			{Path: "/dev/hidraw2", Hello: hello("0123456789abcdef", "v2")},
			{Path: "/dev/hidraw3", Error: "device speaks protocol 1"},
			{Path: "/dev/hidraw4"},
			{Path: "/dev/hidraw5", Hello: hello("00000000000000ff", "v0"), Error: "too old"},
		}}, true, "2 D200s connected", []string{
			"D200 …5566aa: OS v1 (debug)", "D200 …abcdef: OS v2 (debug)", "hidraw3: device speaks protocol 1",
			"D200 …0000ff: too old",
		}},
		{Status{Apps: []AppStatus{{"Discord", "not running"}}}, true, "D200 not connected", []string{"Discord: not running"}},
	}
	for _, tc := range cases {
		header, lines := describe(tc.s, tc.running)
		if header != tc.header || strings.Join(lines, "|") != strings.Join(tc.lines, "|") {
			t.Errorf("describe: %q %q, want %q %q", header, lines, tc.header, tc.lines)
		}
	}
	long := Status{Devices: []Device{{Path: "/dev/hidraw1", Error: strings.Repeat("x", 200)}}}
	if _, lines := describe(long, true); len(lines[0]) > 80 {
		t.Errorf("line not shortened: %d chars", len(lines[0]))
	}
	var many Status
	for range maxTrayDevices + 3 {
		many.Devices = append(many.Devices, Device{Path: "/dev/x", Error: "e"})
	}
	if _, lines := describe(many, true); len(lines) != maxTrayDevices {
		t.Errorf("%d lines", len(lines))
	}
	if shortSerial("abc") != "D200 …abc" {
		t.Error("short serial changed")
	}
}

func TestMenuEntries(t *testing.T) {
	var s Status
	for i := range maxTrayDevices + 2 {
		s.Devices = append(s.Devices, Device{Hello: &proto.Hello{Serial: fmt.Sprintf("%016x", i)}}, Device{Error: "e"})
	}
	got := fileEntries(s)
	if len(got) != maxTrayDevices || got[0].serial != "0000000000000000" || got[1].many != "D200 …000001" ||
		got[0].one != "Install an update file…" {
		t.Errorf("fileEntries = %+v", got)
	}
	if fileEntries(Status{}) != nil || installEntries(s) != nil {
		t.Error("entries with nothing to show")
	}

	up := &Release{Version: "v0.3.0"}
	s = Status{Devices: []Device{
		{Hello: &proto.Hello{Serial: "aa0001"}, OSUpdate: up},
		{Hello: &proto.Hello{Serial: "aa0002"}},                                 // up to date
		{Hello: &proto.Hello{Serial: "aa0003"}, OSUpdate: up, Error: "too old"}, // can still be updated
		{Hello: &proto.Hello{Serial: "aa0004"}, OSUpdate: up, Install: "Sending to device…"},
	}}
	want := []menuEntry{
		{"aa0001", "Update device to v0.3.0", "D200 …aa0001 to v0.3.0", false},
		{"aa0003", "Update device to v0.3.0", "D200 …aa0003 to v0.3.0", false},
		{"aa0004", "Sending to device…", "D200 …aa0004: Sending to device…", true},
	}
	if got := installEntries(s); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("installEntries = %+v", got)
	}
	if len(fileEntries(s)) != 4 || len(s.Connected()) != 3 {
		t.Error("a device too old to talk to")
	}
	if notRunning() == nil {
		t.Error("notRunning returned nil")
	}
}

func TestDaemonStatusWithoutDaemon(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, running := daemonStatus(); running {
		t.Error("daemon reported running")
	}
	fallback := false
	ask("configure", func() error { fallback = true; return nil })
	if !fallback {
		t.Error("no fallback without a daemon")
	}
}
