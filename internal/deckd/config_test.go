package deckd

import (
	"bytes"
	"encoding/base64"
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

func TestConfigOverChannel(t *testing.T) {
	d := newTestDeckd()
	path := filepath.Join(t.TempDir(), "config.json")
	d.cfg, d.file.b = loadConfig(path) // missing: the default
	d.file.path = path
	dev := prototest.New()
	go serve(d, dev)
	c := proto.NewConn(dev)
	defer c.Close()

	b, info, err := c.ReadConfig()
	if err != nil || !bytes.Equal(b, d.file.b) || info.Size != len(b) {
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

	for _, bad := range []struct {
		base, file, want string
	}{
		{info.Rev, string(newFile), "changed since you read it"},
		{"", `{"version": 1, "home": "nowhere", "pages": {}}`, "nowhere"},
		{"", "not json", "invalid character"},
		{"", `{"version": 1, "home": "m", "pages": {"m": {"keys": {"0": {"icon": "mic"}}}}}`, "icons not in the config"},
	} {
		if _, err := c.WriteConfig(bad.base, []byte(bad.file)); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%.20q: %v, want %q", bad.file, err, bad.want)
		}
	}
	if onDisk, _ := os.ReadFile(path); !bytes.Equal(onDisk, newFile) {
		t.Error("a refused config was saved")
	}

	// parts out of order, and a commit without a write
	w, _ := json.Marshal(proto.ConfigWrite{Size: 10, SHA256: "x"})
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
	d.cfg, d.file.b = loadConfig(path)
	d.file.path = path
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
