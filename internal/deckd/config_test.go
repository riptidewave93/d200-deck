package deckd

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/proto/prototest"
)

// noisePNG is an image that doesn't compress, so the config spans many parts.
func noisePNG(t *testing.T) string {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = byte(rand.N(256))
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// useConfig has d show the config at path, as deckd does at start.
func useConfig(t *testing.T, d *deckd, path string) {
	t.Helper()
	cfg, mem := loadConfig(path)
	d.cfg, d.file.path = cfg, path
	if err := d.file.set(cfg, mem, "", nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitGzip(d) })
}

// waitGzip waits until d isn't gzipping its config in the background.
func waitGzip(d *deckd) {
	for {
		d.fileMu.Lock()
		busy := d.file.gzipping
		d.fileMu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestConfigOverChannel(t *testing.T) {
	d := newTestDeckd()
	path := filepath.Join(t.TempDir(), "config.json")
	useConfig(t, d, path) // missing: the default
	dev := prototest.New()
	go serve(d, dev)
	c := proto.NewConn(dev)
	defer c.Close()

	b, info, err := c.ReadConfig()
	if def, _ := json.MarshalIndent(deck.DefaultConfig(), "", "  "); err != nil || !bytes.Equal(b, append(def, '\n')) || info.Size != len(b) {
		t.Fatalf("read %d bytes, %+v, %v", len(b), info, err)
	}

	cfg := deck.DefaultConfig()
	cfg.Home = "browser"
	cfg.Images = map[string]string{"noise": noisePNG(t)}
	newFile, _ := json.Marshal(cfg)
	if len(newFile) < 4*proto.MaxConfigData {
		t.Fatalf("config only %d bytes", len(newFile))
	}
	got, err := c.WriteConfig(info.Rev, newFile)
	if err != nil {
		t.Fatal(err)
	}
	runUI(t, d)
	if d.cfg.Home != "browser" || d.page() != "browser" {
		t.Errorf("not applied: home %q, page %q", d.cfg.Home, d.page())
	}
	if onDisk, _ := os.ReadFile(path); !bytes.Equal(onDisk, newFile) || got != d.file.info() {
		t.Errorf("saved %d bytes, info %+v", len(onDisk), got)
	}
	if b, _, err := c.ReadConfig(); err != nil || !bytes.Equal(b, newFile) {
		t.Errorf("read back %d bytes, %v", len(b), err)
	}
	// the gzip stream sent is the one reads get: not gzipped again
	if gz, _ := os.ReadFile(d.file.gzPath(got.Rev)); d.file.gz == nil || !bytes.Equal(gz, proto.Gzip(newFile)) {
		t.Error("the gzip stream sent wasn't kept")
	}

	for _, bad := range []struct {
		base, file, want string
	}{
		{info.Rev, string(newFile), "changed since you read it"},
		{"", `{"version": 1, "home": "nowhere", "pages": {}}`, "nowhere"},
		{"", "not json", "invalid character"},
		{"", `{"version": 1, "home": "m", "pages": {"m": {"keys": {"0": {"icon": "mic"}}}}}`, "icons not in the config"},
		{"", `{"version": 1, "home": "m", "pages": {"m": {"keys": {"0": {"label": "x", "font2": "y"}}}}}`, "pages.m.keys.0.font2: unknown field"},
	} {
		if _, err := c.WriteConfig(bad.base, []byte(bad.file)); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%.20q: %v, want %q", bad.file, err, bad.want)
		}
	}
	if onDisk, _ := os.ReadFile(path); !bytes.Equal(onDisk, newFile) {
		t.Error("a refused config was saved")
	}

	// a client may send it plain, without gzip_size
	plain, _ := json.Marshal(cfg)
	plain = append(plain, ' ')
	sum := sha256.Sum256(plain)
	w, _ := json.Marshal(proto.ConfigWrite{Base: d.file.info().Rev, Size: len(plain), SHA256: hex.EncodeToString(sum[:])})
	if _, err := c.Do(proto.CmdConfigWrite, w); err != nil {
		t.Fatal(err)
	}
	for off := 0; off < len(plain); off += proto.MaxConfigData {
		part := binary.LittleEndian.AppendUint32(nil, uint32(off))
		if _, err := c.Do(proto.CmdConfigData, append(part, plain[off:min(off+proto.MaxConfigData, len(plain))]...)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Do(proto.CmdConfigCommit, nil); err != nil {
		t.Fatal(err)
	}
	runUI(t, d)
	if b, _ := c.Do(proto.CmdConfigInfo, nil); strings.Contains(string(b), "gzip") {
		t.Errorf("info %s", b)
	}
	if b, _ := c.Do(proto.CmdConfigRead, []byte{0, 0, 0, 0, 10, 0}); string(b) != string(plain[:10]) {
		t.Errorf("plain read %q", b)
	}

	// a gzip stream that isn't the config
	w, _ = json.Marshal(proto.ConfigWrite{Size: len(plain), SHA256: hex.EncodeToString(sum[:]), GzipSize: 5})
	c.Do(proto.CmdConfigWrite, w)
	c.Do(proto.CmdConfigData, []byte{0, 0, 0, 0, 1, 2, 3, 4, 5})
	if _, err := c.Do(proto.CmdConfigCommit, nil); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("bad gzip: %v", err)
	}

	// parts out of order, and a commit without a write
	w, _ = json.Marshal(proto.ConfigWrite{Size: 10, SHA256: "x"})
	c.Do(proto.CmdConfigWrite, w)
	if _, err := c.Do(proto.CmdConfigData, []byte{5, 0, 0, 0, 'x'}); err == nil {
		t.Error("data at the wrong offset taken")
	}
	c.Do(proto.CmdConfigCommit, nil)
	if _, err := c.Do(proto.CmdConfigCommit, nil); err == nil {
		t.Error("commit without a write")
	}
}

func TestConfigAsks(t *testing.T) {
	d := newTestDeckd()
	path := filepath.Join(t.TempDir(), "config.json")
	useConfig(t, d, path)
	dev := prototest.New()
	go serve(d, dev)
	c := proto.NewConn(dev)
	defer c.Close()

	cfg := deck.DefaultConfig()
	cfg.Pages["main"].Keys[12] = deck.Button{Label: "x", Action: deck.Action{Type: deck.ActionLaunch, Command: []string{"sh"}}}
	file, _ := json.Marshal(cfg)
	if n := newActions(d.cfg, cfg); n != 1 {
		t.Fatalf("%d new actions", n)
	}
	write := func(key int) error {
		done := make(chan error, 1)
		go func() { _, err := c.WriteConfig("", file); done <- err }()
		runUI(t, d) // shows the question
		if d.overlay == nil || d.overlay.keys[2].Label != "Save keys?" {
			t.Fatal("not asked")
		}
		d.press(key, true)
		runUI(t, d) // clears it
		err := <-done
		if err == nil {
			runUI(t, d) // shows the config
		}
		return err
	}
	if err := write(10); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancel: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("saved when cancelled")
	}
	if err := write(13); err != nil {
		t.Fatal(err)
	}
	if d.overlay != nil || d.cfg.Pages["main"].Keys[12].Label != "x" {
		t.Error("not shown")
	}
	// the same actions again: no question
	cfg.Home = "browser"
	file, _ = json.Marshal(cfg)
	if _, err := c.WriteConfig("", file); err != nil {
		t.Fatal(err)
	}
	runUI(t, d)
	if d.cfg.Home != "browser" {
		t.Error("not saved")
	}
}

func TestNewScreenIgnoresPresses(t *testing.T) {
	d := newTestDeckd()
	pressDelay = time.Hour
	t.Cleanup(func() { pressDelay = 0 })
	pressed := false
	d.setOverlay(askScreen("t", "d", "OK", func(bool) { pressed = true }))
	d.press(13, true)
	if pressed {
		t.Error("answered at once")
	}
}

func TestHostInfo(t *testing.T) {
	d := newTestDeckd()
	host := func() proto.HostInfo {
		_, b := d.handle(proto.Request{Cmd: proto.CmdHost})
		var h proto.HostInfo
		if err := json.Unmarshal(b, &h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	if h := host(); h.Connected {
		t.Errorf("connected with no host: %+v", h)
	}
	d.pc.heartbeat(proto.ActionHost{ID: "h", Name: "deckhost", Version: "v1", OS: "linux",
		Apps: []proto.HostApp{{Name: "discord", Title: "Discord", Status: "connected", Ops: []string{"mute"}}}})
	d.pc.setState(proto.State{Host: "h", State: map[string]bool{"discord.ready": true}})
	h := host()
	if !h.Connected || h.Name != "deckhost" || len(h.Apps) != 1 || h.Apps[0].Ops[0] != "mute" || !h.State["discord.ready"] {
		t.Errorf("%+v", h)
	}
}

// A saved config with fields this deckd doesn't know, e.g. after an OS
// downgrade, is still shown and kept as it is.
func TestLoadNewerConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	b := []byte(`{"version": 1, "home": "m", "pages": {"m": {"keys": {"0": {"label": "x", "font2": "y"}}}}, "later": 1}` + "\n")
	os.WriteFile(path, b, 0o644)
	if c, mem := loadConfig(path); c.Home != "m" || mem != nil {
		t.Errorf("loaded home %q, default %q", c.Home, mem)
	}
}

// The config is served from its file, not memory; without a good file, the
// default config's JSON is. Either way the bytes served match the rev.
func TestConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	d := newTestDeckd()
	dev := prototest.New()
	go serve(d, dev)
	c := proto.NewConn(dev)
	defer c.Close()
	def, _ := json.MarshalIndent(deck.DefaultConfig(), "", "  ")
	def = append(def, '\n')
	read := func(want []byte, what string) {
		t.Helper()
		b, info, err := c.ReadConfig() // checks the bytes against the rev, gzipped
		if err != nil || !bytes.Equal(b, want) || info.Size != len(want) {
			t.Errorf("%s: read %d bytes, %v", what, len(b), err)
		}
		if b, _ := c.Do(proto.CmdConfigRead, []byte{0, 0, 0, 0, 8, 0}); !bytes.Equal(b, want[:8]) {
			t.Errorf("%s: plain read %q", what, b)
		}
	}

	d.fileMu.Lock()
	useConfig(t, d, path) // no file
	d.fileMu.Unlock()
	read(def, "no file")

	os.WriteFile(path, []byte("not json"), 0o644)
	d.fileMu.Lock()
	useConfig(t, d, path)
	d.fileMu.Unlock()
	read(def, "bad file")
	if b, _ := os.ReadFile(path); string(b) != "not json" {
		t.Error("bad file changed")
	}

	cfg := deck.DefaultConfig()
	cfg.Home = "browser"
	cfg.Images = map[string]string{"noise": noisePNG(t)}
	file, _ := json.Marshal(cfg)
	os.WriteFile(path, file, 0o644)
	os.WriteFile(path+".gz", []byte("stale"), 0o644)                  // an older version's
	os.WriteFile(path+".0123456789abcdef.gz", []byte("stale"), 0o644) // another version's
	d.fileMu.Lock()
	useConfig(t, d, path)
	_, onFlash := d.file.data.r.(*os.File)
	d.fileMu.Unlock()
	if !onFlash {
		t.Error("config file kept in memory")
	}
	read(file, "file") // plain, if the gzip isn't made yet
	waitGzip(d)
	if _, err := os.Stat(path + ".0123456789abcdef.gz"); err == nil {
		t.Error("another version's gzip is still there")
	}
	d.fileMu.Lock()
	useConfig(t, d, path) // as after a restart: the gzip made before is used
	reused := d.file.gz != nil
	d.fileMu.Unlock()
	if !reused {
		t.Error("gzip not kept across a restart")
	}
	read(file, "file, gzipped")

	// a save during a read: the reader finds out, and doesn't get the old
	// config's bytes
	var info proto.ConfigInfo
	b, _ := c.Do(proto.CmdConfigInfo, []byte(`{"gzip": true}`))
	json.Unmarshal(b, &info)
	if info.GzipSize == 0 {
		t.Fatal("not gzipped yet")
	}
	first, _ := c.Do(proto.CmdConfigRead, []byte{0, 0, 0, 0, 0, 0x10, proto.ConfigReadGzip})
	cfg.Home = "main"
	newFile, _ := json.Marshal(cfg)
	got, err := c.WriteConfig(info.Rev, newFile)
	if err != nil || got.Rev == info.Rev {
		t.Fatal(err)
	}
	runUI(t, d)
	rest, err := c.Do(proto.CmdConfigRead, append(binary.LittleEndian.AppendUint32(nil, uint32(len(first))), 0, 0x10, proto.ConfigReadGzip))
	if b, gzErr := proto.Gunzip(append(first, rest...), info.Size); err == nil && gzErr == nil && bytes.Equal(b, file) {
		t.Error("read the old config across a save")
	}
	read(newFile, "after the save")
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if c, _ := loadConfig(filepath.Join(dir, "missing.json")); c.Home != "main" {
		t.Error("missing config didn't fall back to the default")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{"), 0o644)
	if c, _ := loadConfig(bad); c.Home != "main" {
		t.Error("broken config didn't fall back to the default")
	}
	custom := &deck.Config{Version: deck.ConfigVersion, Home: "only", Pages: map[string]deck.Page{
		"only": {Keys: map[int]deck.Button{0: {Icon: "mute", Label: "Mute"}}},
	}}
	good := filepath.Join(dir, "good.json")
	b, _ := json.Marshal(custom)
	os.WriteFile(good, b, 0o644)
	if c, _ := loadConfig(good); c.Home != "only" {
		t.Error("good config not loaded")
	}
	custom.Pages["only"].Keys[0] = deck.Button{Icon: "no-such-icon"}
	b, _ = json.Marshal(custom)
	os.WriteFile(good, b, 0o644)
	if c, _ := loadConfig(good); c.Home != "only" {
		t.Error("config missing an icon not loaded: its keys should show without it")
	}
}

func TestGuardConfig(t *testing.T) {
	dir := t.TempDir()
	old := startsFile
	startsFile = filepath.Join(dir, "starts")
	t.Cleanup(func() { startsFile = old })
	cfg := filepath.Join(dir, "deckd", "config.json")
	guardConfig(cfg) // makes the directory
	os.Remove(startsFile)
	if err := os.WriteFile(cfg, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	for range maxQuickStarts {
		guardConfig(cfg)
		if _, err := os.Stat(cfg); err != nil {
			t.Fatal("config moved too soon")
		}
	}
	guardConfig(cfg)
	if _, err := os.Stat(cfg + ".bad"); err != nil {
		t.Error("config not moved aside after repeated crashes")
	}
	if b, _ := os.ReadFile(startsFile); string(b) != "1" {
		t.Errorf("count %q after moving the config", b)
	}
}
