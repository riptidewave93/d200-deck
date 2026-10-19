package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
)

// reportLen is the length of each report by its ID.
var reportLen = map[byte]int{hidg.ReportKeyboard: 9, hidg.ReportConsumer: 3, hidg.ReportSystem: 2}

// testRunner returns a runner writing to a pipe, and a function that waits
// for the queued jobs and returns the reports they sent.
func testRunner(t *testing.T) (*runner, func() [][]byte) {
	p, r := fifo(t)
	dev, err := hidg.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dev.Close() })
	run := newRunner(dev)
	var pending []byte
	return run, func() [][]byte {
		done := make(chan struct{})
		run.jobs <- func() { close(done) }
		<-done
		buf := make([]byte, 4096)
		r.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		for {
			n, err := r.Read(buf)
			pending = append(pending, buf[:n]...)
			if err != nil {
				break
			}
		}
		var out [][]byte
		for len(pending) > 0 {
			n := reportLen[pending[0]]
			if n == 0 || n > len(pending) {
				t.Fatalf("bad report stream % x", pending)
			}
			out = append(out, pending[:n])
			pending = pending[n:]
		}
		return out
	}
}

func kbd(mods byte, keys ...byte) []byte {
	r := []byte{hidg.ReportKeyboard, mods, 0, 0, 0, 0, 0, 0, 0}
	copy(r[3:], keys)
	return r
}

func expect(t *testing.T, got [][]byte, want ...[]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d reports % x, want %d", len(got), got, len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("report %d: % x, want % x", i, got[i], want[i])
		}
	}
}

func TestConsumerAndSystem(t *testing.T) {
	r, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionConsumer, Usage: deck.UsageMute})
	r.release(0, deck.Action{Type: deck.ActionConsumer})
	r.press(1, deck.Action{Type: deck.ActionSystem, System: deck.SystemSleep})
	r.release(1, deck.Action{Type: deck.ActionSystem})
	expect(t, reports(), []byte{2, 0xe2, 0}, []byte{2, 0, 0}, []byte{3, 2}, []byte{3, 0})
}

func TestComboUnion(t *testing.T) {
	r, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"ctrl", "a"}})
	r.press(1, deck.Action{Type: deck.ActionKey, Keys: []string{"shift", "b"}})
	r.release(0, deck.Action{Type: deck.ActionKey})
	r.release(1, deck.Action{Type: deck.ActionKey})
	expect(t, reports(), kbd(1, 4), kbd(3, 4, 5), kbd(2, 5), kbd(0))
}

func TestComboOverflow(t *testing.T) {
	r, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"a", "b", "c", "d"}})
	r.press(1, deck.Action{Type: deck.ActionKey, Keys: []string{"e", "f", "g"}})
	got := reports()
	expect(t, got, kbd(0, 4, 5, 6, 7), kbd(0, 4, 5, 6, 7, 8, 9))
}

func TestTypeText(t *testing.T) {
	r, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionText, Text: "aB"})
	expect(t, reports(), kbd(0, 4), kbd(0), kbd(2, 5), kbd(0), kbd(0))
}

func TestMacro(t *testing.T) {
	r, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{
		{Type: deck.ActionKey, Keys: []string{"ctrl", "c"}},
		{Type: deck.ActionDelay, MS: 1},
		{Type: deck.ActionConsumer, Usage: deck.UsagePlayPause},
		{Type: deck.ActionSystem, System: deck.SystemWakeUp},
		{Type: deck.ActionText, Text: "z"},
		{Type: deck.ActionOpenURL, URL: "https://example.com"},
	}})
	expect(t, reports(),
		kbd(1, 6), kbd(0),
		[]byte{2, 0xcd, 0}, []byte{2, 0, 0},
		[]byte{3, 4}, []byte{3, 0},
		kbd(0, 0x1d), kbd(0), kbd(0),
	)
}

func TestReleaseAll(t *testing.T) {
	r, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"a"}})
	r.do(r.releaseAll)
	expect(t, reports(), kbd(0, 4), kbd(0), []byte{2, 0, 0}, []byte{3, 0})
}

func TestBusyDropsPresses(t *testing.T) {
	r := newRunner(nil)
	block := make(chan struct{})
	r.do(func() { <-block })
	for range 32 {
		r.do(func() {})
	}
	close(block)
	// nothing to check but that do never blocked and a nil device is safe
	r.press(0, deck.Action{Type: deck.ActionText, Text: "x"})
}

func TestNoGadget(t *testing.T) {
	// a runner without a gadget (e.g. none at startup) does nothing
	r := newRunner(nil)
	done := make(chan struct{})
	r.press(0, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{{Type: deck.ActionConsumer, Usage: deck.UsageMute}, {Type: deck.ActionSystem, System: deck.SystemSleep}}})
	r.jobs <- func() { close(done) }
	<-done
}
