package deckd

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/internal/ota"
	"github.com/riptidewave93/d200-deck/proto"
)

// The image's boot picks the panel config from the U-Boot env's "panel" and
// passes the one in use as d200.panel (d200-buildroot's docs/hardware.md).
var (
	panelEnv    ota.Env = ota.FWEnv{}
	cmdlinePath         = "/proc/cmdline"
)

// panelSetting is the panel override and the panel in use, read on first
// use; active is "" if the image can't pick.
type panelSetting struct {
	mu               sync.Mutex
	read             bool
	override, active string
}

func validPanel(s string) bool {
	return s == proto.PanelAuto || s == proto.PanelH1 || s == proto.PanelH3
}

// get returns the override and the panel in use; ok is false if the image
// can't pick or the env can't be read.
func (p *panelSetting) get() (override, active string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	return p.override, p.active, p.read && p.active != ""
}

func (p *panelSetting) load() {
	if p.read {
		return
	}
	b, _ := os.ReadFile(cmdlinePath)
	for _, f := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(f, "d200.panel="); ok && v != proto.PanelAuto && validPanel(v) {
			p.active = v
		}
	}
	if p.active == "" {
		p.read = true
		return
	}
	env, err := panelEnv.Get()
	if err != nil {
		log.Printf("panel: %v", err)
		return
	}
	p.override = env["panel"]
	if !validPanel(p.override) {
		p.override = proto.PanelAuto
	}
	p.read = true
}

// set saves a new override, for the next boot; changed says it differs.
func (p *panelSetting) set(v string) (changed bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if !p.read || p.active == "" || v == p.override {
		return false, nil
	}
	kv := []string{"panel", v}
	if v == proto.PanelAuto {
		kv = kv[:1] // removes it
	}
	if err := panelEnv.Set(kv...); err != nil {
		return false, err
	}
	p.override = v
	log.Printf("settings: panel=%s", v)
	return true, nil
}

// restart shows that the device restarts, then does.
func (d *deckd) restart() {
	d.ui <- func() { d.setOverlay(restartingScreen()) }
	go func() {
		time.Sleep(resetDelay)
		if err := reboot(); err != nil {
			log.Printf("restart: %v", err)
		}
	}()
}
