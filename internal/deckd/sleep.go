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

	"golang.org/x/sys/unix"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/render"
)

// While the PC's USB is suspended (the PC is asleep, or off with its USB
// ports powered), deckd dims the screen to brightnessMin, shows the sleep screen and runs no
// actions; a press asks the PC to wake.

var (
	udcStateGlob = "/sys/class/udc/*/state"
	wakePC       = hidg.Wake
)

// watchUSB sends whether the USB device controller's state file at path says
// suspended: first as it is, then each time that changes. wait blocks until
// the file may have changed.
func watchUSB(path string, wait func(*os.File) error, asleep chan<- bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 64)
	was := -1
	for {
		n, err := f.ReadAt(buf, 0)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		now := 0
		if strings.TrimSpace(string(buf[:n])) == "suspended" {
			now = 1
		}
		if now != was {
			asleep <- now == 1
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
func watchSleep(asleep chan<- bool) {
	paths, _ := filepath.Glob(udcStateGlob)
	if len(paths) == 0 {
		log.Print("no USB device controller state: no sleep screen")
		return
	}
	log.Printf("USB state: %v", watchUSB(paths[0], waitSysfs, asleep))
}

// setAsleep enters or leaves the sleep screen.
func (d *deckd) setAsleep(v bool) {
	if v == d.asleep {
		return
	}
	d.asleep, d.askedWake = v, false
	if v {
		log.Print("PC asleep or off: sleep screen")
	} else {
		log.Print("PC awake")
	}
	if d.light != nil {
		d.light.setDim(v)
	}
	for i := range deck.KeyCount {
		d.drawKey(i, d.state[i])
	}
}

// pressAsleep asks the PC to wake, once per sleep: the kernel takes it even
// when the PC doesn't allow waking.
func (d *deckd) pressAsleep() {
	if d.askedWake || d.test {
		return
	}
	d.askedWake = true
	if err := wakePC(); err != nil {
		log.Printf("asking the PC to wake: %v", err)
	} else {
		log.Print("asked the PC to wake")
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
