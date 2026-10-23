package deckd

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/hidg"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/render"
)

func TestPCActionWithoutHost(t *testing.T) {
	d := newTestDeckd()
	a := d.cfg.Pages[d.cfg.Home].Keys[13].Action // Configure Buttons: open-url
	if d.showsPressed(a) {
		t.Error("working blue shown without deckhost")
	}
	d.press(13, true)
	if got := keyColour(d, 13); got != render.Colours[render.Error] {
		t.Errorf("no red flash without deckhost: %#x", got)
	}
	if !d.showsPressed(d.cfg.Pages[d.cfg.Home].Keys[6].Action) {
		t.Error("device key not shown pressed")
	}
	d.pc.heartbeat(proto.ActionHost{ID: "host1"})
	if !d.showsPressed(a) {
		t.Error("working blue not shown with deckhost")
	}
}

func TestPCActionEvent(t *testing.T) {
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDeckd()
	d.ch.set(raw)
	defer d.ch.set(nil)
	d.pc.heartbeat(proto.ActionHost{ID: "host1"})
	d.press(13, true)
	ev := readEvent(t, r)
	if ev.Key != 13 || ev.Host != "host1" || ev.Release || ev.Action.URL != deck.ConfigureURL {
		t.Errorf("event %+v", ev)
	}
	d.press(13, false)
	if rel := readEvent(t, r); rel.ID != ev.ID || !rel.Release || rel.Host != "host1" || rel.Key != 13 {
		t.Errorf("release event %+v", rel)
	}
	// a failed result flashes the key
	d.handle(proto.Request{Cmd: proto.CmdResult, Payload: []byte(`{"id":` + itoa(ev.ID) + `,"error":"no browser"}`)})
	runUI(t, d)
	if got := keyColour(d, 13); got != render.Colours[render.Error] {
		t.Errorf("no red flash after a failed result: %#x", got)
	}
}

// readEvent reads one action event from the config channel's pipe.
func readEvent(t *testing.T, r *os.File) proto.ActionEvent {
	t.Helper()
	var msg []byte
	buf := make([]byte, proto.RawInSize)
	r.SetReadDeadline(time.Now().Add(2 * time.Second))
	for done := false; !done; {
		if _, err := io.ReadFull(r, buf); err != nil {
			t.Fatal(err)
		}
		part, err := proto.DecodeReply(buf)
		if err != nil || part.Seq != proto.EventSeq || part.Status != proto.EventAction || part.First != (msg == nil) {
			t.Fatalf("report %+v, %v", part, err)
		}
		msg = append(msg, part.Payload...)
		done = !part.More
	}
	var ev proto.ActionEvent
	if err := json.Unmarshal(msg, &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

func itoa(n uint32) string { b, _ := json.Marshal(n); return string(b) }

func TestPCActionSendFails(t *testing.T) {
	d := newTestDeckd()
	d.pc.heartbeat(proto.ActionHost{ID: "h"})
	d.press(13, true) // channel closed: the send fails
	runUI(t, d)
	if got := keyColour(d, 13); got != render.Colours[render.Error] {
		t.Errorf("no red flash: %#x", got)
	}
}

// A press sent always gets its release, even while the host seems gone.
func TestPCReleaseHostQuiet(t *testing.T) {
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDeckd()
	d.ch.set(raw)
	defer d.ch.set(nil)
	d.pc.heartbeat(proto.ActionHost{ID: "h"})
	d.press(13, true)
	ev := readEvent(t, r)
	d.pc.mu.Lock()
	d.pc.beat = time.Now().Add(-proto.ActionHostTimeout)
	d.pc.mu.Unlock()
	d.press(13, false)
	if rel := readEvent(t, r); rel.ID != ev.ID || !rel.Release || rel.Host != "h" {
		t.Errorf("release event %+v", rel)
	}
}

// A release that fails goes with the next heartbeat.
func TestPCReleaseRetried(t *testing.T) {
	logs := make(chan string, 8)
	log.SetOutput(chanWriter(logs))
	defer log.SetOutput(os.Stderr)
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDeckd()
	d.ch.set(raw)
	d.pc.heartbeat(proto.ActionHost{ID: "h"})
	d.press(13, true)
	ev := readEvent(t, r)
	d.ch.set(nil)
	d.press(13, false)
	for l := ""; !strings.Contains(l, "key 13 release"); {
		select {
		case l = <-logs:
		case <-time.After(2 * time.Second):
			t.Fatal("release didn't fail")
		}
	}

	p, r = fifo(t)
	if raw, err = hidg.OpenRaw(p); err != nil {
		t.Fatal(err)
	}
	d.ch.set(raw)
	defer d.ch.set(nil)
	d.handle(proto.Request{Cmd: proto.CmdActionHost, Payload: []byte(`{"id":"h"}`)})
	if rel := readEvent(t, r); rel.ID != ev.ID || !rel.Release || rel.Key != 13 {
		t.Errorf("release event %+v", rel)
	}
	for start := time.Now(); len(d.pc.unsentReleases()) > 0; time.Sleep(time.Millisecond) {
		if time.Since(start) > 2*time.Second {
			t.Fatal("release still unsent after it went")
		}
	}
}

type chanWriter chan string

func (c chanWriter) Write(b []byte) (int, error) {
	select {
	case c <- string(b):
	default:
	}
	return len(b), nil
}

// A new press of the key replaces an unsent release.
func TestPCReleaseReplaced(t *testing.T) {
	p := pcActions{fail: func(int) {}}
	p.addRelease(proto.ActionEvent{ID: 1, Key: 3, Release: true})
	p.addRelease(proto.ActionEvent{ID: 2, Key: 4, Release: true})
	p.start(3)
	if got := p.unsentReleases(); len(got) != 1 || got[0].Key != 4 {
		t.Errorf("unsent releases %+v", got)
	}
	p.released(proto.ActionEvent{ID: 9, Key: 4}) // not the one kept
	p.released(proto.ActionEvent{ID: 2, Key: 4})
	if got := p.unsentReleases(); len(got) != 0 {
		t.Errorf("unsent releases %+v", got)
	}
}

func TestPCActions(t *testing.T) {
	var failed []int
	p := pcActions{fail: func(k int) { failed = append(failed, k) }}
	if _, ok := p.hostAlive(); ok {
		t.Error("host alive before any heartbeat")
	}
	p.heartbeat(proto.ActionHost{ID: "h"})
	if h, ok := p.hostAlive(); !ok || h != "h" {
		t.Error("host not alive after a heartbeat")
	}
	ok := p.start(1)
	p.result(proto.Result{ID: ok})
	bad := p.start(2)
	p.result(proto.Result{ID: bad, Error: "boom"})
	p.result(proto.Result{ID: bad, Error: "again"}) // already finished
	p.result(proto.Result{ID: 999, Error: "unknown"})
	if len(failed) != 1 || failed[0] != 2 {
		t.Errorf("failed keys %v", failed)
	}

	old := pcActionTimeout
	pcActionTimeout = 10 * time.Millisecond
	defer func() { pcActionTimeout = old }()
	done := make(chan int, 1)
	p.fail = func(k int) { done <- k }
	p.start(7)
	select {
	case k := <-done:
		if k != 7 {
			t.Errorf("timed out key %d", k)
		}
	case <-time.After(2 * time.Second):
		t.Error("no timeout")
	}
}

func TestPCState(t *testing.T) {
	changed := make(chan struct{}, 8)
	p := pcActions{changed: func() { changed <- struct{}{} }}
	p.setState(proto.State{Host: "a", State: map[string]bool{"x": true}})
	if p.getState("x") {
		t.Error("state taken without an action host")
	}
	p.heartbeat(proto.ActionHost{ID: "a"})
	p.setState(proto.State{Host: "b", State: map[string]bool{"x": true}})
	if p.getState("x") {
		t.Error("state taken from another host")
	}
	p.setState(proto.State{Host: "a", State: map[string]bool{"x": true, "y": true}})
	p.setState(proto.State{Host: "a", State: map[string]bool{"y": false}})
	if !p.getState("x") || p.getState("y") {
		t.Error("state not merged")
	}
	<-changed

	// a new host starts afresh
	p.heartbeat(proto.ActionHost{ID: "b"})
	<-changed
	if p.getState("x") {
		t.Error("old host's state kept")
	}

	// and a host that went quiet has no state
	p.setState(proto.State{Host: "b", State: map[string]bool{"x": true}})
	p.mu.Lock()
	p.beat = time.Now().Add(-proto.ActionHostTimeout)
	p.mu.Unlock()
	if p.getState("x") {
		t.Error("state of a dead host")
	}
}

// The host resends its state with every heartbeat: only a change redraws.
func TestPCStateUnchanged(t *testing.T) {
	changed := make(chan struct{}, 8)
	p := pcActions{changed: func() { changed <- struct{}{} }}
	p.heartbeat(proto.ActionHost{ID: "a"})
	redrew := func() bool {
		select {
		case <-changed:
			return true
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}
	steps := []struct {
		state map[string]bool
		want  bool
	}{
		{map[string]bool{"x": true}, true},
		{map[string]bool{"x": true}, false},  // the same again
		{map[string]bool{"y": false}, false}, // off, as it was
		{map[string]bool{"x": false}, true},
	}
	for i, s := range steps {
		p.setState(proto.State{Host: "a", State: s.state})
		if got := redrew(); got != s.want {
			t.Errorf("step %d (%v): redraw %v, want %v", i, s.state, got, s.want)
		}
	}
}

func TestStateLook(t *testing.T) {
	d := newTestDeckd()
	d.cfg.Pages["main"].Keys[4] = deck.Button{Icon: "mic", Action: deck.Action{Type: deck.ActionApp, App: deck.AppDiscord, Op: deck.DiscordMute}}
	d.pc.changed = func() { d.ui <- d.drawPage }
	d.drawPage()
	if got := keyColour(d, 4); got != render.Colours[render.Idle] {
		t.Fatalf("unmuted key colour %#x", got)
	}
	d.pc.heartbeat(proto.ActionHost{ID: "h"})
	d.pc.setState(proto.State{Host: "h", State: map[string]bool{"discord.mute": true}})
	runUI(t, d)
	if got := keyColour(d, 4); got != 0xc62828 {
		t.Errorf("muted key colour %#x, want the on look's red", got)
	}
	d.pc.setState(proto.State{Host: "h", State: map[string]bool{"discord.mute": false}})
	runUI(t, d)
	if got := keyColour(d, 4); got != render.Colours[render.Idle] {
		t.Errorf("unmuted again: %#x", got)
	}
}

func TestDiscordFallback(t *testing.T) {
	d := newTestDeckd()
	d.run.stop(time.Second)
	run, _, reports := testRunner(t)
	d.run = run
	d.cfg.Pages["main"].Keys[4] = deck.Button{Action: deck.Action{Type: deck.ActionApp, App: deck.AppDiscord, Op: deck.DiscordMute}}
	keybind := func() { // Discord's, held while the key is
		t.Helper()
		d.press(4, true)
		expect(t, reports(), kbd(0x03, 0x10))
		d.press(4, false)
		expect(t, reports(), kbd(0))
	}

	keybind() // no deckhost
	d.pc.heartbeat(proto.ActionHost{ID: "h"})
	d.pc.setState(proto.State{Host: "h", State: map[string]bool{"discord.ready": false}})
	keybind() // deckhost without Discord

	// Discord ready: the PC does it, without the fallback
	p, r := fifo(t)
	raw, err := hidg.OpenRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	d.ch.set(raw)
	defer d.ch.set(nil)
	d.pc.setState(proto.State{Host: "h", State: map[string]bool{"discord.ready": true}})
	d.press(4, true)
	if ev := readEvent(t, r); ev.Action.Op != deck.DiscordMute || ev.Action.Fallback != nil {
		t.Errorf("event %+v", ev)
	}
	d.press(4, false)
	if ev := readEvent(t, r); !ev.Release {
		t.Errorf("release %+v", ev)
	}
	expect(t, reports())
}
