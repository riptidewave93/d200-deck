// Package debuginfo collects what helps diagnose a device for a bug report:
// versions, uptime, memory, storage, flash health, the update state, the
// config with its private parts hidden, and logs.
package debuginfo

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/ota"
)

// Collector gathers a device's state.
type Collector struct {
	Root    string // prefixed to every path; "" on the device
	Version string // deckd's
	Config  string // deckd's config file
	Run     func(name string, args ...string) ([]byte, error)
}

// Device collects from this device.
func Device(version, config string) *Collector {
	return &Collector{Version: version, Config: config, Run: run}
}

func run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// files are copied into the bundle as they are.
var files = []string{
	"/etc/os-release", "/proc/cmdline", "/proc/meminfo", "/proc/loadavg", "/proc/mounts",
	"/proc/mtd", "/proc/device-tree/model", "/data/deckd/update.json", "/var/log/messages.0", "/var/log/messages",
}

// commands' output goes into the bundle as <name>.txt.
var commands = [][]string{
	{"uname", "-a"}, {"uptime"}, {"free"}, {"df", "-h"}, {"ps"}, {"dmesg"}, {"fw_printenv"},
	{"ip", "addr"},
}

// maxEntry bounds one file in the bundle; logs keep their end.
const maxEntry = 2 << 20

func (c *Collector) path(p string) string { return filepath.Join(c.Root, p) }

// Bundle writes the state as a .tar.gz, everything under debug/.
func (c *Collector) Bundle(w io.Writer) error {
	zw := gzip.NewWriter(w)
	tw := tar.NewWriter(zw)
	now := time.Now()
	var problems []string
	add := func(name string, b []byte) error {
		if len(b) > maxEntry {
			b = b[len(b)-maxEntry:]
		}
		h := &tar.Header{Name: path.Join("debug", name), Mode: 0o644, Size: int64(len(b)), ModTime: now}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		_, err := tw.Write(b)
		return err
	}

	type entry struct {
		name string
		get  func() ([]byte, error)
	}
	text := func(s func() string) func() ([]byte, error) {
		return func() ([]byte, error) { return []byte(s()), nil }
	}
	entries := []entry{
		{"summary.txt", text(c.Summary)},
		{"flash.txt", text(c.flashReport)},
		{"usb.txt", text(c.usbReport)},
		{"thermal.txt", text(c.thermalReport)},
	}
	for _, f := range []string{c.Config, c.Config + ".bad"} {
		entries = append(entries, entry{strings.TrimPrefix(f, "/"), func() ([]byte, error) {
			b, err := os.ReadFile(c.path(f))
			if err != nil {
				return nil, err
			}
			return redactConfig(b)
		}})
	}
	for _, f := range files {
		entries = append(entries, entry{strings.TrimPrefix(f, "/"), func() ([]byte, error) { return os.ReadFile(c.path(f)) }})
	}
	for _, cmd := range commands {
		entries = append(entries, entry{"cmd/" + strings.Join(cmd, "_") + ".txt", func() ([]byte, error) { return c.Run(cmd[0], cmd[1:]...) }})
	}
	for _, e := range entries {
		b, err := e.get()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", e.name, err))
			if len(b) == 0 {
				continue
			}
		}
		if err := add(e.name, b); err != nil {
			return err
		}
	}
	if len(problems) > 0 {
		if err := add("problems.txt", []byte(strings.Join(problems, "\n")+"\n")); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

// Summary is a few lines to paste into a bug report.
func (c *Collector) Summary() string {
	var b strings.Builder
	line := func(k, v string) { fmt.Fprintf(&b, "%-8s %s\n", k+":", v) }
	info, err := osrelease.Read(c.path(osrelease.Path))
	model, _ := os.ReadFile(c.path("/proc/device-tree/model"))
	line("model", orNone(strings.TrimSpace(strings.TrimRight(string(model), "\x00"))))
	if err != nil {
		line("OS", "unknown")
	} else {
		line("OS", fmt.Sprintf("%s, build %s", info.Name, info.Build))
	}
	line("deckd", c.Version)
	cmdline, _ := os.ReadFile(c.path("/proc/cmdline"))
	env := map[string]string{}
	if out, err := c.Run("fw_printenv"); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if k, v, ok := strings.Cut(l, "="); ok {
				env[k] = v
			}
		}
	}
	line("update", fmt.Sprintf("slot %s, ota_result %s", orNone(ota.RunningSlot(string(cmdline))), orNone(env["ota_result"])))
	line("uptime", c.uptime())
	line("memory", c.memory())
	line("temp", c.temp())
	line("data", c.space("/data"))
	line("flash", c.flash())
	line("config", c.config())
	line("USB", c.usb())
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func (c *Collector) uptime() string {
	b, err := os.ReadFile(c.path("/proc/uptime"))
	f := strings.Fields(string(b))
	if err != nil || len(f) == 0 {
		return "unknown"
	}
	secs, _ := strconv.ParseFloat(f[0], 64)
	up := time.Duration(secs) * time.Second
	s := fmt.Sprintf("%dh %dm", int(up.Hours()), int(up.Minutes())%60)
	if l, err := os.ReadFile(c.path("/proc/loadavg")); err == nil {
		if lf := strings.Fields(string(l)); len(lf) >= 3 {
			s += ", load " + strings.Join(lf[:3], " ")
		}
	}
	return s
}

// zones are the thermal zones' sysfs directories.
func (c *Collector) zones() []string {
	z, _ := filepath.Glob(c.path("/sys/class/thermal/thermal_zone*"))
	return z
}

func celsius(milli string) string {
	n, err := strconv.Atoi(milli)
	if err != nil {
		return milli
	}
	return fmt.Sprintf("%.1f °C", float64(n)/1000)
}

func (c *Collector) temp() string {
	var t []string
	for _, z := range c.zones() {
		t = append(t, fmt.Sprintf("%s %s", celsius(readSys(z+"/temp")), readSys(z+"/type")))
	}
	if len(t) == 0 {
		return "unknown"
	}
	return strings.Join(t, ", ")
}

func (c *Collector) thermalReport() string {
	var b strings.Builder
	for _, z := range c.zones() {
		fmt.Fprintf(&b, "%s %q: %s\n", filepath.Base(z), readSys(z+"/type"), celsius(readSys(z+"/temp")))
	}
	return b.String()
}

func (c *Collector) memory() string {
	b, err := os.ReadFile(c.path("/proc/meminfo"))
	if err != nil {
		return "unknown"
	}
	kb := map[string]int64{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok {
			n, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
			kb[k] = n
		}
	}
	return fmt.Sprintf("%d MiB available of %d MiB", kb["MemAvailable"]>>10, kb["MemTotal"]>>10)
}

func (c *Collector) space(dir string) string {
	var st unix.Statfs_t
	if err := unix.Statfs(c.path(dir), &st); err != nil {
		return "unknown"
	}
	mib := func(blocks uint64) uint64 { return blocks * uint64(st.Bsize) >> 20 }
	return fmt.Sprintf("%d MiB free of %d MiB", mib(st.Bavail), mib(st.Blocks))
}

// mtds are the MTD partitions' sysfs directories.
func (c *Collector) mtds() []string {
	dirs, _ := filepath.Glob(c.path("/sys/class/mtd/mtd*"))
	var out []string
	for _, d := range dirs {
		if !strings.HasSuffix(d, "ro") {
			out = append(out, d)
		}
	}
	return out
}

func readSys(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(b))
}

func (c *Collector) flash() string {
	var bad, failed, corrected int64
	for _, d := range c.mtds() {
		n := func(f string) int64 { v, _ := strconv.ParseInt(readSys(filepath.Join(d, f)), 10, 64); return v }
		bad += n("bad_blocks")
		failed += n("ecc_failures")
		corrected += n("corrected_bits")
	}
	ubi := c.path("/sys/class/ubi/ubi0")
	return fmt.Sprintf("%d bad blocks, %d ECC failures, %d corrected bits; UBI %s bad (%s reserved), max erase count %s",
		bad, failed, corrected, readSys(ubi+"/bad_peb_count"), readSys(ubi+"/reserved_for_bad"), readSys(ubi+"/max_ec"))
}

func (c *Collector) flashReport() string {
	var b strings.Builder
	for _, d := range c.mtds() {
		fmt.Fprintf(&b, "%s %q: size %s, bad_blocks %s, bbt_blocks %s, ecc_failures %s, corrected_bits %s\n",
			filepath.Base(d), readSys(d+"/name"), readSys(d+"/size"), readSys(d+"/bad_blocks"),
			readSys(d+"/bbt_blocks"), readSys(d+"/ecc_failures"), readSys(d+"/corrected_bits"))
	}
	ubi := c.path("/sys/class/ubi/ubi0")
	b.WriteString("\nubi0:")
	for _, f := range []string{"total_eraseblocks", "avail_eraseblocks", "bad_peb_count", "reserved_for_bad", "max_ec", "ro_mode"} {
		fmt.Fprintf(&b, " %s %s,", f, readSys(filepath.Join(ubi, f)))
	}
	b.WriteString("\n")
	vols, _ := filepath.Glob(c.path("/sys/class/ubi/ubi0_*"))
	for _, v := range vols {
		fmt.Fprintf(&b, "%s %q: type %s, reserved_ebs %s, data_bytes %s, corrupted %s, upd_marker %s\n",
			filepath.Base(v), readSys(v+"/name"), readSys(v+"/type"), readSys(v+"/reserved_ebs"),
			readSys(v+"/data_bytes"), readSys(v+"/corrupted"), readSys(v+"/upd_marker"))
	}
	return b.String()
}

func (c *Collector) usb() string {
	states, _ := filepath.Glob(c.path("/sys/class/udc/*/state"))
	if len(states) == 0 {
		return "no USB device controller"
	}
	return readSys(states[0])
}

func (c *Collector) usbReport() string {
	var b strings.Builder
	states, _ := filepath.Glob(c.path("/sys/class/udc/*/state"))
	for _, s := range states {
		fmt.Fprintf(&b, "%s: %s\n", filepath.Base(filepath.Dir(s)), readSys(s))
	}
	gadgets, _ := filepath.Glob(c.path("/sys/kernel/config/usb_gadget/*"))
	for _, g := range gadgets {
		fmt.Fprintf(&b, "gadget %s: UDC %s, %s:%s, bcdDevice %s\n", filepath.Base(g), readSys(g+"/UDC"),
			readSys(g+"/idVendor"), readSys(g+"/idProduct"), readSys(g+"/bcdDevice"))
		funcs, _ := filepath.Glob(g + "/configs/*/*.*")
		for _, f := range funcs {
			fmt.Fprintf(&b, "  %s\n", filepath.Base(f))
		}
	}
	return b.String()
}

func (c *Collector) config() string {
	p := c.path(c.Config)
	var s string
	cfg, err := deck.Load(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s = "none (the default config)"
	case err != nil:
		s = "broken (the default config is used): " + err.Error()
	default:
		s = fmt.Sprintf("ok, %d pages, %d images, %d fonts", len(cfg.Pages), len(cfg.Images), len(cfg.Fonts))
	}
	if _, err := os.Stat(p + ".bad"); err == nil {
		s += "; a config that kept crashing deckd was set aside as " + filepath.Base(p) + ".bad"
	}
	return s
}
