package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

func TestControl(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	urls := fakeBrowser(t)
	d := newDaemon()
	d.devices["/dev/hidraw9"] = &Device{Path: "/dev/hidraw9", Error: "not yet"}
	d.devices["/dev/hidraw1"] = &Device{Path: "/dev/hidraw1", Hello: &proto.Hello{Serial: "00112233445566aa"}}
	l, err := listenControl(d)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	reply, err := control("status")
	var s Status
	if err != nil || json.Unmarshal([]byte(reply), &s) != nil || len(s.Devices) != 2 || s.Devices[1].Error != "not yet" {
		t.Errorf("status %q, %v", reply, err)
	}
	for _, cmd := range []string{"configure", "update", "update 5566aa"} {
		if reply, err := control(cmd); err != nil || reply != "ok" {
			t.Errorf("%s: %q, %v", cmd, reply, err)
		}
	}
	const page = "http://198.19.23.5/update"
	if u := urls(); len(u) != 3 || u[0] != deck.ConfigureURL || u[1] != page || u[2] != page {
		t.Errorf("opened %v", u)
	}
	if _, err := control("update ffff"); err == nil || err.Error() != "no D200 ffff connected" {
		t.Errorf("update of a missing deck: %v", err)
	}
	for _, cmd := range []string{"dance", "status now"} {
		if _, err := control(cmd); err == nil || err.Error() != "unknown command" {
			t.Errorf("%s: %v", cmd, err)
		}
	}
	if _, err := listenControl(d); err == nil {
		t.Error("a second daemon started")
	}
	info, _ := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "deckhost.sock"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("socket mode %v", info.Mode().Perm())
	}
}

func TestControlStaleSocket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	os.WriteFile(filepath.Join(dir, "deckhost.sock"), nil, 0o600) // left by a crash
	l, err := listenControl(newDaemon())
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	if _, err := control("status"); err == nil {
		t.Error("talked to a stopped daemon")
	}
}

func TestControlNoRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := listenControl(newDaemon()); err == nil {
		t.Error("listened without XDG_RUNTIME_DIR")
	}
	if _, err := control("status"); err == nil {
		t.Error("connected without XDG_RUNTIME_DIR")
	}
}

func TestControlGarbage(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	l, err := listenControl(newDaemon())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	path, _ := socketPath()
	c, _ := net.Dial("unix", path)
	c.Write([]byte("no newline"))
	c.(*net.UnixConn).CloseWrite()
	b, _ := io.ReadAll(c)
	c.Close()
	if len(b) != 0 {
		t.Errorf("answered a partial command: %q", b)
	}
}

func TestControlChecks(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	if !checkUpdates() {
		t.Error("checks off without settings")
	}
	d := newDaemon()
	d.os.latest = func(string) (Release, bool, error) { return Release{}, false, nil }
	d.setCheckUpdates(checkUpdates())
	l, err := listenControl(d)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, on := range []bool{false, true} {
		arg := map[bool]string{true: "on", false: "off"}[on]
		if _, err := control("checks " + arg); err != nil {
			t.Fatal(err)
		}
		d.mu.Lock()
		state := d.state[stateUpdateDevice]
		d.mu.Unlock()
		if checkUpdates() != on || d.getStatus().CheckUpdates != on || state != on {
			t.Errorf("checks %s: saved %v, status %v, state %v", arg, checkUpdates(), d.getStatus().CheckUpdates, state)
		}
	}
	if _, err := control("checks maybe"); err == nil {
		t.Error("checks maybe")
	}
	if _, err := control("install"); err == nil || err.Error() != "no D200 connected" {
		t.Errorf("install with nothing connected: %v", err)
	}
}
