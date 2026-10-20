package deckd

import (
	"bytes"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
)

// reportLen is the length of each report by its ID.
var reportLen = map[byte]int{hidg.ReportKeyboard: 9, hidg.ReportConsumer: 3, hidg.ReportSystem: 2}

var released = [][]byte{kbd(0), {2, 0, 0}, {3, 0}}

// gadget reads the reports a runner sends to a pipe.
type gadget struct {
	t       *testing.T
	r       *os.File
	pending []byte
}

// read returns the reports sent within wait.
func (g *gadget) read(wait time.Duration) [][]byte {
	g.t.Helper()
	buf := make([]byte, 4096)
	g.r.SetReadDeadline(time.Now().Add(wait))
	for {
		n, err := g.r.Read(buf)
		g.pending = append(g.pending, buf[:n]...)
		if err != nil {
			break
		}
	}
	var out [][]byte
	for len(g.pending) > 0 {
		n := reportLen[g.pending[0]]
		if n == 0 || n > len(g.pending) {
			g.t.Fatalf("bad report stream % x", g.pending)
		}
		out = append(out, g.pending[:n])
		g.pending = g.pending[n:]
	}
	return out
}

// testRunner returns a runner writing to a pipe, after its first reports
// (everything released), and a function that waits for the queued jobs and
// returns the reports they sent.
func testRunner(t *testing.T) (*runner, *gadget, func() [][]byte) {
	p, r := fifo(t)
	run := newRunner(p)
	t.Cleanup(func() { run.stop(time.Second) })
	g := &gadget{t: t, r: r}
	reports := func() [][]byte {
		done := make(chan struct{})
		run.jobs <- func() { close(done) }
		<-done
		return g.read(20 * time.Millisecond)
	}
	expect(t, reports(), released...)
	return run, g, reports
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

func TestEachChange(t *testing.T) {
	r, _, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionConsumer, Usage: deck.UsageMute})
	expect(t, reports(), []byte{2, 0xe2, 0})
	r.release(0, deck.Action{Type: deck.ActionConsumer})
	expect(t, reports(), []byte{2, 0, 0})
	r.press(1, deck.Action{Type: deck.ActionSystem, System: deck.SystemSleep})
	expect(t, reports(), []byte{3, 2})
	r.release(0, deck.Action{}) // a key that holds nothing
	expect(t, reports())
}

func TestReopenReplacedGadget(t *testing.T) {
	r, g, reports := testRunner(t)
	// a USB gadget re-bind re-creates the device node
	os.Remove(r.path)
	if err := syscall.Mkfifo(r.path, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(r.path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g.r = f
	r.press(0, deck.Action{Type: deck.ActionConsumer, Usage: deck.UsageMute})
	// the new node gets the press, then every report, as after any reopen
	expect(t, reports(), []byte{2, 0xe2, 0}, kbd(0), []byte{2, 0xe2, 0}, []byte{3, 0})
}

func TestComboUnion(t *testing.T) {
	r, _, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"ctrl", "a"}})
	expect(t, reports(), kbd(1, 4))
	r.press(1, deck.Action{Type: deck.ActionKey, Keys: []string{"shift", "b"}})
	expect(t, reports(), kbd(3, 4, 5))
	r.release(0, deck.Action{Type: deck.ActionKey})
	expect(t, reports(), kbd(2, 5))
	r.release(1, deck.Action{Type: deck.ActionKey})
	expect(t, reports(), kbd(0))
}

func TestComboOverflow(t *testing.T) {
	r, _, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"a", "b", "c", "d"}})
	r.press(1, deck.Action{Type: deck.ActionKey, Keys: []string{"e", "f", "g"}})
	expect(t, reports(), kbd(0, 4, 5, 6, 7, 8, 9))
}

func TestTypeText(t *testing.T) {
	r, _, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionText, Text: "aB"})
	expect(t, reports(), kbd(0, 4), kbd(0), kbd(2, 5), kbd(0))
}

func TestMacro(t *testing.T) {
	r, _, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{
		{Type: deck.ActionKey, Keys: []string{"ctrl", "c"}},
		{Type: deck.ActionDelay, MS: 1},
		{Type: deck.ActionConsumer, Usage: deck.UsagePlayPause},
		{Type: deck.ActionSystem, System: deck.SystemWakeUp},
		{Type: deck.ActionText, Text: "z"},
	}})
	expect(t, reports(),
		kbd(1, 6), kbd(0),
		[]byte{2, 0xcd, 0}, []byte{2, 0, 0},
		[]byte{3, 4}, []byte{3, 0},
		kbd(0, 0x1d), kbd(0),
	)
}

func TestMacroKeepsHeldKeys(t *testing.T) {
	r, _, reports := testRunner(t)
	r.press(1, deck.Action{Type: deck.ActionConsumer, Usage: deck.UsageVolumeUp})
	expect(t, reports(), []byte{2, 0xe9, 0})
	r.press(0, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{{Type: deck.ActionConsumer, Usage: deck.UsageMute}}})
	// the macro's release of its own media key doesn't release volume up
	expect(t, reports(), []byte{2, 0xe2, 0}, []byte{2, 0, 0}, []byte{2, 0xe9, 0})
}

func TestReleaseDuringMacro(t *testing.T) {
	r, g, reports := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{
		{Type: deck.ActionDelay, MS: deck.MaxDelayMS},
		{Type: deck.ActionKey, Keys: []string{"a"}},
	}})
	r.press(1, deck.Action{Type: deck.ActionKey, Keys: []string{"ctrl"}})
	expect(t, g.read(100*time.Millisecond), kbd(1))
	r.release(1, deck.Action{Type: deck.ActionKey})
	expect(t, g.read(100*time.Millisecond), kbd(0))

	// pressing the macro's key again cancels it
	r.press(0, deck.Action{Type: deck.ActionMacro})
	expect(t, reports())
}

func TestCancelTyping(t *testing.T) {
	r, _, reports := testRunner(t)
	block := make(chan struct{})
	r.jobs <- func() { <-block }
	r.press(0, deck.Action{Type: deck.ActionText, Text: "abc"})
	r.cancelJob() // cancelled before it starts: that's for an earlier job
	close(block)
	if got := reports(); len(got) != 6 {
		t.Errorf("typed %d reports, want 6", len(got))
	}
	r.busy.Store(0)
	r.press(0, deck.Action{Type: deck.ActionText, Text: "x"}) // cancels, doesn't queue
	r.busy.Store(-1)
	if !r.cancelled() {
		t.Error("second press didn't cancel")
	}
	r.cancelJob()
	if err := r.typeText("abc"); err != errStopped || len(reports()) != 0 {
		t.Error("cancelled typing sent reports")
	}
}

// Stopping a macro while it types stops the whole macro, not just the typing.
func TestStopMacroWhileTyping(t *testing.T) {
	r, _, reports := testRunner(t)
	typing := make(chan struct{})
	r.jobs <- func() { close(typing) } // runs just before the macro
	r.press(0, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{
		{Type: deck.ActionText, Text: strings.Repeat("a", deck.MaxText)},
		{Type: deck.ActionKey, Keys: []string{"f13"}},
	}})
	<-typing
	for r.busy.Load() != 0 {
		time.Sleep(time.Millisecond)
	}
	r.press(0, deck.Action{Type: deck.ActionMacro}) // pressed again: stop
	for _, rep := range reports() {
		if rep[0] == hidg.ReportKeyboard && rep[3] == 0x68 {
			t.Fatal("the macro went on after it was stopped")
		}
	}
}

func TestStop(t *testing.T) {
	r, g, _ := testRunner(t)
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"a"}})
	r.press(1, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{{Type: deck.ActionDelay, MS: deck.MaxDelayMS}}})
	r.press(2, deck.Action{Type: deck.ActionText, Text: "queued"})
	start := time.Now()
	r.stop(time.Second)
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("stop took %v", time.Since(start))
	}
	got := g.read(50 * time.Millisecond)
	expect(t, got[len(got)-3:], released...)
	for _, rep := range got {
		if rep[0] == hidg.ReportKeyboard && rep[3] != 0 && rep[3] != 4 {
			t.Errorf("queued typing ran after stop: % x", rep)
		}
	}
}

func TestStopTimesOut(t *testing.T) {
	r := newRunner("")
	block := make(chan struct{})
	defer close(block)
	r.jobs <- func() { <-block } // a job that ignores cancel
	start := time.Now()
	r.stop(50 * time.Millisecond)
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("stop took %v", time.Since(start))
	}
}

func TestBusyDropsPresses(t *testing.T) {
	r := newRunner("")
	block := make(chan struct{})
	r.jobs <- func() { <-block }
	for range 32 {
		r.press(1, deck.Action{Type: deck.ActionText, Text: "x"}) // never blocks
	}
	close(block)
	r.stop(time.Second)
}

func TestNoGadget(t *testing.T) {
	// a runner without a gadget (e.g. none at startup) does nothing
	r := newRunner("")
	done := make(chan struct{})
	r.press(0, deck.Action{Type: deck.ActionMacro, Steps: []deck.Action{{Type: deck.ActionConsumer, Usage: deck.UsageMute}, {Type: deck.ActionSystem, System: deck.SystemSleep}}})
	r.press(1, deck.Action{Type: deck.ActionKey, Keys: []string{"a"}})
	r.jobs <- func() { close(done) }
	<-done
	r.stop(time.Second)
}

// inRunner runs f on the runner's goroutine, between reports.
func inRunner(r *runner, f func()) {
	done := make(chan struct{})
	r.jobs <- func() { f(); close(done) }
	<-done
}

func TestReopen(t *testing.T) {
	old := reopenEvery
	reopenEvery = 10 * time.Millisecond
	t.Cleanup(func() { reopenEvery = old })
	r, g, reports := testRunner(t)

	// the gadget goes away (writes fail), then comes back
	g.r.Close()
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"a"}})
	inRunner(r, func() {
		if r.hid != nil || r.valid {
			t.Error("gadget not closed after a failed write")
		}
	})
	rd, err := os.OpenFile(r.path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rd.Close() })
	g.r, g.pending = rd, nil
	time.Sleep(20 * time.Millisecond)
	r.press(1, deck.Action{Type: deck.ActionKey, Keys: []string{"b"}})
	// all of the state is sent again, not just the change
	expect(t, reports(), kbd(0, 4, 5), released[1], released[2])

	// a gadget that can't be opened is logged once and retried
	missing := t.TempDir() + "/missing"
	inRunner(r, func() {
		r.path = missing
		r.hid.Close()
		r.hid = nil
	})
	time.Sleep(20 * time.Millisecond)
	r.press(0, deck.Action{Type: deck.ActionKey, Keys: []string{"c"}})
	inRunner(r, func() {
		if r.openErr == "" {
			t.Error("open error not kept")
		}
	})
}
