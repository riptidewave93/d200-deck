package deckd

import (
	"errors"
	"log"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
)

// runner sends HID reports on one goroutine, so typing and macros don't hold
// up drawing and reports never interleave. What the keys hold down is state,
// not a queued job, so a release always gets through, even during a macro.
type runner struct {
	path     string // the HID gadget; "" for none
	hid      *hidg.Device
	lastOpen time.Time
	lastWake time.Time
	asked    bool // logged asking the PC to wake, since a report last went out
	openErr  string

	mu   sync.Mutex
	want holding // set by press and release

	wake   chan struct{}      // want changed
	jobs   chan func()        // typing and macros
	cancel chan struct{}      // stop the running job
	final  chan chan struct{} // stop: release everything, then close
	busy   atomic.Int32       // the key whose job is running, or -1
	ending atomic.Bool        // stop was called; skip queued jobs

	sent  reports // what the PC last took
	valid bool    // sent is known
}

// holding is what each key holds down.
type holding struct {
	combos   [deck.KeyCount]*combo
	consumer [deck.KeyCount]deck.Usage
	system   [deck.KeyCount]deck.System
}

type combo struct {
	mods byte
	keys []byte
}

// reports are the keyboard, consumer and system reports for a holding.
type reports struct {
	mods     byte
	keys     []byte
	consumer uint16
	system   byte
}

func (h *holding) reports() reports {
	var r reports
	for i, c := range h.combos {
		if c != nil {
			r.mods |= c.mods
			r.keys = append(r.keys, c.keys...)
		}
		if r.consumer == 0 {
			r.consumer = uint16(h.consumer[i])
		}
		r.system |= byte(h.system[i])
	}
	if len(r.keys) > deck.MaxKeys {
		r.keys = r.keys[:deck.MaxKeys]
	}
	return r
}

// wakeEvery limits how often a sleeping PC is asked to wake.
var wakeEvery = time.Second

// reopenEvery limits how often a missing gadget is opened again.
var reopenEvery = time.Second

func newRunner(path string) *runner {
	r := &runner{
		path:   path,
		wake:   make(chan struct{}, 1),
		jobs:   make(chan func(), 16),
		cancel: make(chan struct{}, 1),
		final:  make(chan chan struct{}),
	}
	r.busy.Store(-1)
	r.wake <- struct{}{} // report everything released, in case deckd died with keys down
	go func() {
		for {
			select {
			case <-r.wake:
				r.flush()
			case job := <-r.jobs:
				r.flush()
				job()
				r.flush()
			case done := <-r.final:
				r.valid = false
				r.flush()
				close(done)
				return
			}
		}
	}()
	return r
}

// stop cancels any job and reports everything released, waiting at most
// timeout. The runner does nothing after.
func (r *runner) stop(timeout time.Duration) {
	if r.ending.Swap(true) {
		return
	}
	r.set(func(h *holding) { *h = holding{} })
	r.cancelJob()
	done := make(chan struct{})
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r.final <- done:
	case <-t.C:
		return
	}
	select {
	case <-done:
	case <-t.C:
	}
}

// set changes what's held and wakes the runner to report it.
func (r *runner) set(f func(*holding)) {
	r.mu.Lock()
	f(&r.want)
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// do queues a job for key i, or cancels key i's job if it's running.
func (r *runner) do(i int, job func()) {
	if r.busy.Load() == int32(i) {
		r.cancelJob()
		return
	}
	select {
	case r.jobs <- func() {
		if r.ending.Load() {
			return
		}
		select {
		case <-r.cancel: // meant for an earlier job
		default:
		}
		r.busy.Store(int32(i))
		defer r.busy.Store(-1)
		job()
	}:
	default:
		log.Print("busy, key press dropped")
	}
}

func (r *runner) cancelJob() {
	select {
	case r.cancel <- struct{}{}:
	default:
	}
}

func (r *runner) cancelled() bool {
	select {
	case <-r.cancel:
		return true
	default:
		return false
	}
}

// press starts a's press on key i; release ends it.
func (r *runner) press(i int, a deck.Action) {
	switch a.Type {
	case deck.ActionConsumer:
		r.set(func(h *holding) { h.consumer[i] = a.Usage })
	case deck.ActionSystem:
		r.set(func(h *holding) { h.system[i] = a.System })
	case deck.ActionKey:
		mods, keys, _ := deck.ParseKeys(a.Keys)
		r.set(func(h *holding) { h.combos[i] = &combo{mods, keys} })
	case deck.ActionText:
		r.do(i, func() { r.typeText(a.Text) })
	case deck.ActionMacro:
		r.do(i, func() { r.macro(a.Steps) })
	}
}

func (r *runner) release(i int, a deck.Action) {
	r.set(func(h *holding) {
		h.combos[i], h.consumer[i], h.system[i] = nil, 0, 0
	})
}

// flush reports what's held, where it differs from what the PC has.
func (r *runner) flush() {
	r.mu.Lock()
	want := r.want.reports()
	r.mu.Unlock()
	all := !r.valid
	r.valid = true // until a send fails
	if all || want.mods != r.sent.mods || !slices.Equal(want.keys, r.sent.keys) {
		r.keyboard(want.mods, want.keys...)
	}
	if all || want.consumer != r.sent.consumer {
		r.consumer(want.consumer)
	}
	if all || want.system != r.sent.system {
		r.system(want.system)
	}
}

// macro runs steps until one fails or the job is cancelled. Releases are
// reported between steps and during delays.
func (r *runner) macro(steps []deck.Action) {
	for _, s := range steps {
		if r.cancelled() {
			return
		}
		var err error
		switch s.Type {
		case deck.ActionDelay:
			if !r.sleep(time.Duration(s.MS) * time.Millisecond) {
				return
			}
		case deck.ActionConsumer:
			if err = r.consumer(uint16(s.Usage)); err == nil {
				err = r.consumer(0)
			}
		case deck.ActionSystem:
			if err = r.system(byte(s.System)); err == nil {
				err = r.system(0)
			}
		case deck.ActionKey:
			mods, keys, _ := deck.ParseKeys(s.Keys)
			err = r.keyboard(mods, keys...)
		case deck.ActionText:
			err = r.typeText(s.Text)
		}
		if err != nil {
			return
		}
		r.flush()
	}
}

// sleep waits d, reporting releases meanwhile. It's false if cancelled.
func (r *runner) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			return true
		case <-r.wake:
			r.flush()
		case <-r.cancel:
			return false
		}
	}
}

func (r *runner) typeText(text string) error {
	for _, c := range text {
		if r.cancelled() {
			return nil
		}
		mods, key, _ := deck.TypeKey(c)
		if err := r.keyboard(mods, key); err != nil {
			return err
		}
		if err := r.keyboard(0); err != nil {
			return err
		}
	}
	return nil
}

var errNoGadget = errors.New("no HID gadget")

// send sends one report, opening the gadget if needed. A write that fails for
// any reason but the PC not reading closes it, so it's opened afresh (it may
// have been re-created).
func (r *runner) send(f func(*hidg.Device) error) error {
	if r.hid != nil && r.hid.Replaced(r.path) {
		log.Print("HID gadget re-created: reopening it")
		r.hid.Close()
		r.hid, r.lastOpen, r.valid = nil, time.Time{}, false
	}
	if r.hid == nil && r.path != "" && time.Since(r.lastOpen) >= reopenEvery {
		r.lastOpen = time.Now()
		dev, err := hidg.Open(r.path)
		if err != nil {
			if err.Error() != r.openErr {
				log.Printf("HID gadget: %v", err)
				r.openErr = err.Error()
			}
		} else {
			r.hid, r.openErr = dev, ""
		}
	}
	if r.hid == nil {
		r.valid = false
		return errNoGadget
	}
	err := f(r.hid)
	if errors.Is(err, hidg.ErrNotReading) {
		// the PC may be asleep: ask it to wake (only a sleeping PC that
		// allows it does). The press is lost, as on a keyboard.
		if time.Since(r.lastWake) >= wakeEvery {
			r.lastWake = time.Now()
			// the kernel says yes even when it doesn't wake the PC
			if hidg.Wake() == nil && !r.asked {
				log.Print("asked the PC to wake")
				r.asked = true
			}
		}
		r.valid = false
		return err
	}
	if err == nil {
		r.asked = false
	}
	if err != nil {
		log.Print(err)
		r.valid = false
		if !errors.Is(err, hidg.ErrNotReading) {
			r.hid.Close()
			r.hid = nil
		}
	}
	return err
}

func (r *runner) keyboard(mods byte, keys ...byte) error {
	err := r.send(func(d *hidg.Device) error { return d.Keyboard(mods, keys...) })
	if err == nil {
		r.sent.mods, r.sent.keys = mods, slices.Clone(keys)
	}
	return err
}

func (r *runner) consumer(u uint16) error {
	err := r.send(func(d *hidg.Device) error { return d.Consumer(u) })
	if err == nil {
		r.sent.consumer = u
	}
	return err
}

func (r *runner) system(s byte) error {
	err := r.send(func(d *hidg.Device) error { return d.System(s) })
	if err == nil {
		r.sent.system = s
	}
	return err
}
