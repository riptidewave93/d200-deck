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
		}}, true, "2 D200s connected", []string{
			"D200 …5566aa: OS v1 (debug)", "D200 …abcdef: OS v2 (debug)", "hidraw3: device speaks protocol 1",
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

func TestUpdateSerials(t *testing.T) {
	var s Status
	for i := range maxTrayDevices + 2 {
		s.Devices = append(s.Devices, Device{Hello: &proto.Hello{Serial: fmt.Sprintf("%016x", i)}}, Device{Error: "e"})
	}
	got := updateSerials(s)
	if len(got) != maxTrayDevices || got[0] != "0000000000000000" || got[1] != "0000000000000001" {
		t.Errorf("updateSerials = %q", got)
	}
	if updateSerials(Status{}) != nil {
		t.Error("serials with nothing connected")
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
