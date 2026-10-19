package hidraw

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindAll(t *testing.T) {
	old := sysHidraw
	defer func() { sysHidraw = old }()
	sysHidraw = t.TempDir()
	for n, desc := range map[string][]byte{
		"hidraw0": {0x05, 0x01, 0x09, 0x06}, // a keyboard
		"hidraw1": {0x06, 0xd2, 0xff, 0x09, 0x01},
		"hidraw7": {0x06, 0xd2, 0xff, 0x09, 0x01},
	} {
		d := filepath.Join(sysHidraw, n, "device")
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "report_descriptor"), desc, 0o644)
	}
	got := FindAll([]byte{0x06, 0xd2, 0xff})
	if len(got) != 2 || got[0] != "/dev/hidraw1" || got[1] != "/dev/hidraw7" {
		t.Errorf("FindAll = %v", got)
	}
}

func TestDevice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hidraw")
	os.WriteFile(p, []byte("input report"), 0o644)
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := d.ReadReport(buf)
	if err != nil || string(buf[:n]) != "input report" {
		t.Errorf("read %q, %v", buf[:n], err)
	}
	if err := d.WriteReport([]byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	d.Close()
	b, _ := os.ReadFile(p)
	if string(b[len(b)-3:]) != "\x00\x01\x02" {
		t.Errorf("written report % x, want a leading 0", b[len(b)-3:])
	}
	if _, err := Open(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("opened a missing node")
	}
}
