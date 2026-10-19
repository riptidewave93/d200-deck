package debuginfo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeDevice(t *testing.T) *Collector {
	root := t.TempDir()
	write := func(p, s string) {
		p = filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("etc/os-release", "PRETTY_NAME=\"Deck OS\"\nBUILD_ID=202610192020\n")
	write("proc/cmdline", "console=ttyS0 d200.slot=b\n")
	write("proc/uptime", "7384.5 100.0\n")
	write("proc/loadavg", "0.10 0.20 0.30 1/50 300\n")
	write("proc/meminfo", "MemTotal:  117704 kB\nMemFree:  77208 kB\nMemAvailable:   80544 kB\n")
	for _, m := range []string{"mtd0", "mtd0ro", "mtd3"} {
		write("sys/class/mtd/"+m+"/name", m)
		write("sys/class/mtd/"+m+"/bad_blocks", "1")
		write("sys/class/mtd/"+m+"/ecc_failures", "0")
		write("sys/class/mtd/"+m+"/corrected_bits", "4")
	}
	write("sys/class/ubi/ubi0/bad_peb_count", "0")
	write("sys/class/ubi/ubi0/reserved_for_bad", "40")
	write("sys/class/ubi/ubi0/max_ec", "4")
	write("sys/class/udc/ff400000.usb/state", "configured")
	write("sys/class/thermal/thermal_zone0/type", "soc-thermal")
	write("sys/class/thermal/thermal_zone0/temp", "33044")
	write("proc/device-tree/model", "Some Deck\x00")
	write("data/deckd/config.json", `{"version": 1, "home": "main", "pages": {"main": {"keys": {
		"0": {"label": "Lights", "action": {"type": "http", "url": "http://ha.local/SECRET", "headers": {"Authorization": "SECRET"}, "body": "SECRET"}},
		"1": {"label": "Hi", "action": {"type": "macro", "steps": [{"type": "type-text", "text": "SECRET"}]}},
		"2": {"action": {"type": "launch", "command": ["/home/SECRET/bin/x"], "fallback": {"type": "hid-key", "keys": ["ctrl", "m"]}}},
		"3": {"action": {"type": "app", "app": "discord", "op": "channel", "args": {"id": "SECRET"}}}}}},
		"images": {"logo": "AAAAAAAA"}}`)
	write("data/deckd/config.json.bad", "{not json SECRET")
	write("var/log/messages", strings.Repeat("x", maxEntry)+"the end\n")
	return &Collector{Root: root, Version: "v9", Config: "/data/deckd/config.json",
		Run: func(name string, args ...string) ([]byte, error) {
			if name == "fw_printenv" {
				return []byte("slot=b\nota_result=ok\n"), nil
			}
			if name == "dmesg" {
				return []byte("partial"), errors.New("exit status 1")
			}
			return []byte(name + " output"), nil
		}}
}

func TestSummary(t *testing.T) {
	s := fakeDevice(t).Summary()
	for _, want := range []string{
		"model:   Some Deck\n",
		"OS:      Deck OS, build 202610192020",
		"temp:    33.0 °C soc-thermal",
		"deckd:   v9",
		"update:  slot b, ota_result ok",
		"uptime:  2h 3m, load 0.10 0.20 0.30",
		"memory:  78 MiB available of 114 MiB",
		"flash:   2 bad blocks, 0 ECC failures, 8 corrected bits; UBI 0 bad (40 reserved), max erase count 4",
		"config:  broken",
		"USB:     configured",
		", not synced since boot",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
}

func TestBundle(t *testing.T) {
	var buf bytes.Buffer
	if err := fakeDevice(t).Bundle(&buf); err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	got := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		got[h.Name] = string(b)
	}
	for _, name := range []string{"debug/summary.txt", "debug/flash.txt", "debug/usb.txt",
		"debug/etc/os-release", "debug/thermal.txt", "debug/proc/device-tree/model", "debug/cmd/uname_-a.txt", "debug/cmd/dmesg.txt", "debug/var/log/messages"} {
		if _, ok := got[name]; !ok {
			t.Errorf("no %s", name)
		}
	}
	if _, ok := got["debug/data/deckd/update.json"]; ok {
		t.Error("a missing file was bundled")
	}
	if !strings.Contains(got["debug/problems.txt"], "dmesg.txt: exit status 1") {
		t.Errorf("problems: %q", got["debug/problems.txt"])
	}
	if log := got["debug/var/log/messages"]; len(log) != maxEntry || !strings.HasSuffix(log, "the end\n") {
		t.Errorf("log is %d bytes, ends %q", len(log), log[len(log)-10:])
	}
	for name, b := range got {
		if strings.Contains(b, "SECRET") {
			t.Errorf("%s has private config contents:\n%s", name, b)
		}
	}
	cfg := got["debug/data/deckd/config.json"]
	for _, want := range []string{`"label": "Lights"`, `"Authorization": "<redacted>"`, `"ctrl"`, `"logo": "<6 bytes>"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %s:\n%s", want, cfg)
		}
	}
	if !strings.Contains(got["debug/problems.txt"], "config.json.bad: not valid JSON") {
		t.Errorf("problems: %q", got["debug/problems.txt"])
	}
}
