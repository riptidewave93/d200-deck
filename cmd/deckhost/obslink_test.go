package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/obs"
	"github.com/riptidewave93/d200-deck/internal/obs/obstest"
	"github.com/riptidewave93/d200-deck/proto"
)

func TestDaemonOBS(t *testing.T) {
	fake := obstest.New("")
	defer fake.Close()
	dev, results, states := fakeD200(proto.Version)
	defer dev.Close()
	d := newDaemon()
	link := newOBSLink(func() (*obs.Conn, bool, error) {
		c, err := obs.Dial(fake.URL, "")
		return c, true, err
	}, d.setState)
	link.retry = 10 * time.Millisecond
	d.apps[deck.AppOBS] = link
	stop := make(chan struct{})
	defer close(stop)
	go link.run(stop)
	d.find = func() []string { return []string{"/dev/hidraw5"} }
	d.open = func(string) (proto.Transport, error) { return dev, nil }
	d.scan()

	// the deck learns the state names as they change
	seen := map[string]bool{}
	waitDeck := func(name string, on bool) {
		t.Helper()
		for deadline := time.After(3 * time.Second); seen[name] != on || !seen["obs.ready"]; {
			select {
			case s := <-states:
				for k, v := range s.State {
					seen[k] = v
				}
			case <-deadline:
				t.Fatalf("deck never told %s %v: %v", name, on, seen)
			}
		}
	}
	waitDeck("obs.scene:Main", true)
	press := func(op, id string) {
		t.Helper()
		var args json.RawMessage
		if id != "" {
			args, _ = json.Marshal(map[string]string{"id": id})
		}
		b, _ := json.Marshal(proto.ActionEvent{ID: 9, Host: d.id, Key: 1,
			Action: deck.Action{Type: deck.ActionApp, App: deck.AppOBS, Op: op, Args: args}})
		dev.Send(proto.EventSeq, proto.EventAction, b)
		select {
		case r := <-results:
			if r.Error != "" {
				t.Errorf("%s: %s", op, r.Error)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: no result", op)
		}
	}
	press(deck.OBSScene, "BRB")
	waitDeck("obs.scene:BRB", true)
	waitDeck("obs.scene:Main", false)
	press(deck.OBSRecord, "")
	waitDeck("obs.record", true)
	press(deck.OBSMute, "Mic")
	waitDeck("obs.mute:Mic", true)
	if err := link.do(deck.OBSScene, nil, false); err == nil {
		t.Error("scene without an id worked")
	}
	if err := link.do("explode", nil, false); err == nil {
		t.Error("an unknown op worked")
	}

	fake.Close()
	waitFor(t, "disconnect", func() bool { return link.statusText() == "not running" })
	d.mu.Lock()
	on := len(d.state)
	d.mu.Unlock()
	if on != 0 {
		t.Errorf("OBS state still on after it went: %v", d.state)
	}
	if err := link.do(deck.OBSRecord, nil, false); err == nil {
		t.Error("record worked without OBS")
	}
}

func TestOBSStatus(t *testing.T) {
	fake := obstest.New("secret")
	defer fake.Close()
	for _, tc := range []struct {
		setUp bool
		url   string
		want  string
	}{
		{false, "ws://127.0.0.1:1", ""}, // OBS may not be installed
		{true, "ws://127.0.0.1:1", "not running"},
		{true, fake.URL, "needs its WebSocket password (see docs/obs.md)"},
	} {
		l := newOBSLink(func() (*obs.Conn, bool, error) {
			c, err := obs.Dial(tc.url, "")
			return c, tc.setUp, err
		}, func(map[string]bool) {})
		l.retry = time.Hour
		l.status = "-"
		stop := make(chan struct{})
		go l.run(stop)
		waitFor(t, tc.want, func() bool { return l.statusText() == tc.want })
		close(stop)
	}
}
