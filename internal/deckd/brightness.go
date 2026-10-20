package deckd

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// brightnessMin is the lowest brightness in percent: the least a person can
// set, and the sleep screen's. Still visible on the D200.
const brightnessMin = 10

var (
	backlightGlob = "/sys/class/backlight/*"
	// saveDelay lets a slider settle before the level is written to flash.
	saveDelay = time.Second
)

// backlight is the screen's brightness: the person's level in percent, kept
// in file, or brightnessMin while the PC sleeps. Percent maps onto the
// kernel's levels.
type backlight struct {
	mu      sync.Mutex
	dir     string // the sysfs backlight; "" if there's none
	max     int
	file    string
	percent int
	dim     bool
	unsaved bool
	save    *time.Timer
}

func newBacklight(file string) *backlight {
	b := &backlight{file: file, percent: 100}
	if dirs, _ := filepath.Glob(backlightGlob); len(dirs) > 0 {
		if m, err := readInt(filepath.Join(dirs[0], "max_brightness")); err == nil && m > 0 {
			b.dir, b.max = dirs[0], m
		}
	}
	if p, err := readInt(file); err == nil {
		b.percent = clampBrightness(p)
	}
	b.apply()
	return b
}

func readInt(path string) (int, error) {
	s, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(s)))
}

func clampBrightness(p int) int { return min(max(p, brightnessMin), 100) }

// get is the person's level, if there's a backlight to set.
func (b *backlight) get() (percent int, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.percent, b.dir != ""
}

// set changes the person's level now and saves it saveDelay later.
func (b *backlight) set(percent int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.percent, b.unsaved = clampBrightness(percent), true
	b.applyLocked()
	if b.save == nil {
		b.save = time.AfterFunc(saveDelay, b.flush)
	} else {
		b.save.Reset(saveDelay)
	}
}

// setDim dims to brightnessMin while the PC sleeps, and back after.
func (b *backlight) setDim(dim bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dim = dim
	b.applyLocked()
}

func (b *backlight) apply() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.applyLocked()
}

func (b *backlight) applyLocked() {
	if b.dir == "" {
		return
	}
	p := b.percent
	if b.dim {
		p = brightnessMin
	}
	level := max((p*b.max+50)/100, 1)
	if err := os.WriteFile(filepath.Join(b.dir, "brightness"), []byte(strconv.Itoa(level)), 0); err != nil {
		log.Printf("backlight: %v", err)
	}
}

// flush saves the person's level if it changed.
func (b *backlight) flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.unsaved {
		return
	}
	tmp := filepath.Join(filepath.Dir(b.file), "."+filepath.Base(b.file)+".tmp")
	err := os.WriteFile(tmp, []byte(strconv.Itoa(b.percent)+"\n"), 0o644)
	if err == nil {
		err = os.Rename(tmp, b.file)
	}
	if err != nil {
		log.Printf("brightness: %v", err)
		return
	}
	b.unsaved = false
}
