package deckd

import (
	"errors"
	"image"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/unix"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/render"
)

// While the PC hasn't configured the USB device (the PC is asleep, off, or
// still starting), deckd dims the screen to brightnessMin and shows the sleep
// screen, or turns the screen off (the sleep_screen setting), and runs no
// actions; while the bus is suspended, a press asks the PC to wake.

var (
	udcStateGlob = "/sys/class/udc/*/state"
	wakePC       = hidg.Wake
)

// watchUSB sends the USB device controller's state from the file at path:
// first as it is, then each time it changes. wait blocks until the file may
// have changed.
func watchUSB(path string, wait func(*os.File) error, states chan<- string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 64)
	was := ""
	for {
		n, err := f.ReadAt(buf, 0)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if now := strings.TrimSpace(string(buf[:n])); now != was {
			states <- now
			was = now
		}
		if err := wait(f); err != nil {
			return err
		}
	}
}

// waitSysfs blocks until the kernel notifies a change to the sysfs file f.
// Go's poller doesn't watch sysfs files, so this holds a thread.
func waitSysfs(f *os.File) error {
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLPRI | unix.POLLERR}}
	for {
		_, err := unix.Poll(fds, -1)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// watchSleep follows the USB state for the main loop, if the controller has
// one.
func watchSleep(states chan<- string) {
	paths, _ := filepath.Glob(udcStateGlob)
	if len(paths) == 0 {
		log.Print("no USB device controller state: no sleep screen")
		return
	}
	log.Printf("USB state: %v", watchUSB(paths[0], waitSysfs, states))
}

// setUSB follows the USB state: the PC is awake only once it has configured
// the device.
func (d *deckd) setUSB(state string) {
	log.Printf("USB state: %s", state)
	d.suspended = state == "suspended"
	d.setAsleep(state != "configured")
}

// setAsleep enters or leaves the sleep screen.
func (d *deckd) setAsleep(v bool) {
	if v == d.asleep {
		return
	}
	d.asleep, d.askedWake = v, false
	if v {
		d.editing.keep(false, 0, nil)
	}
	if v && d.pattern.shown {
		d.pattern.keep(false, nil)
		d.pattern.shown = false // showSleep redraws the keys
	}
	if v {
		log.Print("PC asleep or off: sleep screen")
	} else {
		log.Print("PC awake")
	}
	d.showSleep()
}

// screenPower turns the panel off and on (a drm.Display).
type screenPower interface {
	Off() error
	On() error
}

// showSleep sets the screen for d.asleep and the sleep screen setting:
// dimmed with the sleep mark, off, or as it was. Off, the backlight goes
// first; back on, it comes last, after the keys are drawn.
func (d *deckd) showSleep() {
	off := d.asleep && d.sleep.off()
	if off && d.light != nil {
		d.light.setSleep(false, true)
	}
	if off && !d.screenOff && d.power != nil {
		if err := d.power.Off(); err != nil {
			log.Printf("screen off: %v", err)
		}
	}
	for i := range deck.KeyCount {
		d.drawKey(i, d.state[i])
	}
	if !off && d.screenOff && d.power != nil {
		if err := d.power.On(); err != nil {
			log.Printf("screen on: %v", err)
		}
	}
	d.screenOff = off
	if !off && d.light != nil {
		d.light.setSleep(d.asleep, false)
	}
}

// sleepScreen is the sleep screen setting, kept in file.
type sleepScreen struct {
	file  string
	isOff atomic.Bool
}

func (s *sleepScreen) off() bool { return s.isOff.Load() }

func (s *sleepScreen) get() string {
	if s.off() {
		return proto.SleepScreenOff
	}
	return proto.SleepScreenZzz
}

// load reads file; anything but "off" is the default.
func (s *sleepScreen) load(file string) {
	b, _ := os.ReadFile(file)
	s.file = file
	s.isOff.Store(strings.TrimSpace(string(b)) == proto.SleepScreenOff)
}

// set takes a new setting and saves it; false if v isn't one.
func (s *sleepScreen) set(v string) (bool, error) {
	if v != proto.SleepScreenZzz && v != proto.SleepScreenOff {
		return false, nil
	}
	s.isOff.Store(v == proto.SleepScreenOff)
	return true, saveSetting(s.file, v)
}

// saveSetting writes v to file, if there is one.
func saveSetting(file, v string) error {
	if file == "" {
		return nil
	}
	tmp := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+".tmp")
	err := os.WriteFile(tmp, []byte(v+"\n"), 0o644)
	if err == nil {
		err = os.Rename(tmp, file)
	}
	return err
}

// pressAsleep asks the PC to wake, once per sleep, if the bus is suspended.
func (d *deckd) pressAsleep() {
	if d.askedWake || d.test || !d.suspended {
		return
	}
	d.askedWake = true
	askWake()
}

// askWake asks the PC to wake (srp) and logs the result. The kernel may say
// yes even when the PC doesn't allow waking.
func askWake() {
	if err := wakePC(); err != nil {
		log.Printf("asked the PC to wake: %v", err)
	} else {
		log.Print("asked the PC to wake: ok")
	}
}

// sleepKey is the key that shows the sleep mark: the widest.
var sleepKey = func() int {
	k := 0
	for i := range deck.KeyCount {
		if render.Key(i).Dx() > render.Key(k).Dx() {
			k = i
		}
	}
	return k
}()

var sleepMask *image.Alpha

// drawSleepKey draws key i as it is on the sleep screen: black, the widest
// key with the sleep mark.
func drawSleepKey(c *gfx.Canvas, i int, r gfx.Rect) {
	c.Fill(r, 0)
	if i != sleepKey {
		return
	}
	if sleepMask == nil || sleepMask.Rect.Size() != r.Size() {
		sleepMask = sleepMark(r.Dx(), r.Dy())
	}
	c.DrawMask(sleepMask, r.Min, 0xffffff)
}

// sleepMark is ". . z z Z Z" rising from bottom left to top right, each
// bigger than the last, in thick rounded strokes, centred in w x h.
func sleepMark(w, h int) *image.Alpha {
	type seg struct{ ax, ay, bx, by, r float64 }
	var segs []seg
	// sizes are letter heights (dot diameters for the dots) in units of h
	x, base := 0.0, 0.0 // pen position and baseline, rising
	for _, s := range []struct {
		size float64
		dot  bool
	}{{0.06, true}, {0.08, true}, {0.19, false}, {0.26, false}, {0.34, false}, {0.44, false}} {
		size := s.size * float64(h)
		if s.dot {
			r := size / 2
			segs = append(segs, seg{x + r, base - r, x + r, base - r, r})
			x += size + 0.07*float64(h)
			base -= 0.04 * float64(h)
			continue
		}
		t := size * 0.17 // stroke width
		r, wd := t/2, size*0.86
		l, rt, top, bot := x+r, x+wd-r, base-size+r, base-r
		segs = append(segs, seg{l, top, rt, top, r}, seg{rt, top, l, bot, r}, seg{l, bot, rt, bot, r})
		x += wd + 0.07*float64(h)
		base -= 0.09 * float64(h)
	}
	// bounds, to centre the mark
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, s := range segs {
		minX = min(minX, s.ax-s.r, s.bx-s.r)
		maxX = max(maxX, s.ax+s.r, s.bx+s.r)
		minY = min(minY, s.ay-s.r, s.by-s.r)
		maxY = max(maxY, s.ay+s.r, s.by+s.r)
	}
	dx, dy := (float64(w)-(maxX-minX))/2-minX, (float64(h)-(maxY-minY))/2-minY
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	for _, s := range segs {
		ax, ay, bx, by := s.ax+dx, s.ay+dy, s.bx+dx, s.by+dy
		x0, x1 := int(min(ax, bx)-s.r-1), int(max(ax, bx)+s.r+1)
		y0, y1 := int(min(ay, by)-s.r-1), int(max(ay, by)+s.r+1)
		for py := max(y0, 0); py <= min(y1, h-1); py++ {
			for px := max(x0, 0); px <= min(x1, w-1); px++ {
				d := segDist(float64(px)+0.5, float64(py)+0.5, ax, ay, bx, by)
				cov := min(max(s.r+0.5-d, 0), 1)
				if a := uint8(cov * 255); a > m.Pix[py*m.Stride+px] {
					m.Pix[py*m.Stride+px] = a
				}
			}
		}
	}
	return m
}

// segDist is the distance from (px, py) to the segment from a to b.
func segDist(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	t := 0.0
	if l := vx*vx + vy*vy; l > 0 {
		t = min(max(((px-ax)*vx+(py-ay)*vy)/l, 0), 1)
	}
	return math.Hypot(px-ax-t*vx, py-ay-t*vy)
}
