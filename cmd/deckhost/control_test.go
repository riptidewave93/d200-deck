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
