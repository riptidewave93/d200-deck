package main

import (
	"log"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
)

// runner carries out HID actions in order on one goroutine, so macros and
// typing don't hold up drawing and reports never interleave.
type runner struct {
	hid  *hidg.Device
	jobs chan func()
	// combos are the hid-key combinations held down, by key
	combos [deck.KeyCount]*combo
}

type combo struct {
	mods byte
	keys []byte
}

func newRunner(hid *hidg.Device) *runner {
	r := &runner{hid: hid, jobs: make(chan func(), 16)}
	go func() {
		for job := range r.jobs {
			job()
		}
	}()
	return r
}

// releaseAll reports every key and media key as released. Run at start and
// on stop, so keys held when deckd died or stopped don't stay down on the PC.
func (r *runner) releaseAll() {
	r.combos = [deck.KeyCount]*combo{}
	r.keyboard(0)
	r.consumer(0)
}

func (r *runner) do(job func()) {
	select {
	case r.jobs <- job:
	default:
		log.Print("busy, key press dropped")
	}
}

// press starts a's press on key i; release ends it.
func (r *runner) press(i int, a deck.Action) {
	switch a.Type {
	case deck.ActionConsumer:
		r.do(func() { r.consumer(a.Usage) })
	case deck.ActionKey:
		mods, keys, _ := deck.ParseKeys(a.Keys)
		r.do(func() { r.combos[i] = &combo{mods, keys}; r.sendKeys() })
	case deck.ActionText:
		r.do(func() { r.typeText(a.Text) })
	case deck.ActionMacro:
		r.do(func() { r.macro(a.Steps) })
	}
}

func (r *runner) release(i int, a deck.Action) {
	switch a.Type {
	case deck.ActionConsumer:
		r.do(func() { r.consumer(0) })
	case deck.ActionKey:
		r.do(func() { r.combos[i] = nil; r.sendKeys() })
	}
}

func (r *runner) macro(steps []deck.Action) {
	for _, s := range steps {
		switch s.Type {
		case deck.ActionDelay:
			time.Sleep(time.Duration(s.MS) * time.Millisecond)
		case deck.ActionConsumer:
			r.consumer(s.Usage)
			r.consumer(0)
		case deck.ActionKey:
			mods, keys, _ := deck.ParseKeys(s.Keys)
			r.keyboard(mods, keys...)
			r.sendKeys()
		case deck.ActionText:
			r.typeText(s.Text)
		default:
			if s.Type.OnPC() {
				log.Printf("macro step %s: needs the PC daemon", s.Type)
			}
		}
	}
}

func (r *runner) typeText(text string) {
	for _, c := range text {
		mods, key, _ := deck.TypeKey(c)
		r.keyboard(mods, key)
		r.keyboard(0)
	}
	r.sendKeys()
}

// sendKeys reports the union of the held hid-key combinations.
func (r *runner) sendKeys() {
	var mods byte
	var keys []byte
	for _, c := range r.combos {
		if c != nil {
			mods |= c.mods
			keys = append(keys, c.keys...)
		}
	}
	if len(keys) > deck.MaxKeys {
		keys = keys[:deck.MaxKeys]
	}
	r.keyboard(mods, keys...)
}

func (r *runner) keyboard(mods byte, keys ...byte) {
	if r.hid == nil {
		return
	}
	if err := r.hid.Keyboard(mods, keys...); err != nil {
		log.Print(err)
	}
}

func (r *runner) consumer(u deck.Usage) {
	if r.hid == nil {
		return
	}
	if err := r.hid.Consumer(uint16(u)); err != nil {
		log.Print(err)
	}
}
