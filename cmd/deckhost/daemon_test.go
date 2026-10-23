package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/proto/prototest"
)

func TestShorten(t *testing.T) {
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"abcdef", 3, "abc"},
		{"aé", 2, "a"}, // é is 2 bytes; don't split it
		{"éé", 1, ""},
	} {
		if got := shorten(c.s, c.n); got != c.want {
			t.Errorf("shorten(%q, %d) = %q", c.s, c.n, got)
		}
	}
	if long := shorten(strings.Repeat("x", 5000), maxResultError); len(long) != maxResultError {
		t.Errorf("%d bytes", len(long))
	}
}

// fakeD200 serves hello, action-host and state like deckd and sends results
// and states to the returned channels. beat, if set, answers each heartbeat
// after the first.
// timeSent is set when a fake device gets the right time.
var timeSent atomic.Bool

func fakeD200(protocol int, beat ...func(proto.ActionHost) byte) (*prototest.Device, chan proto.Result, chan proto.State) {
	dev := prototest.New()
	results := make(chan proto.Result, 4)
	states := make(chan proto.State, 16)
	beats := 0
	go dev.Serve(func(req proto.Request) (byte, []byte) {
		switch req.Cmd {
		case proto.CmdHello:
			b, _ := json.Marshal(proto.Hello{Protocol: protocol, Serial: "00112233445566aa", Keys: 14})
			return proto.StatusOK, b
		case proto.CmdActionHost:
			var h proto.ActionHost
			if json.Unmarshal(req.Payload, &h) != nil || h.ID == "" {
				return proto.StatusBadRequest, nil
			}
			if beats++; beats > 1 && len(beat) > 0 {
				return beat[0](h), nil
			}
			return proto.StatusOK, nil
		case proto.CmdResult:
			var r proto.Result
			json.Unmarshal(req.Payload, &r)
			results <- r
			return proto.StatusOK, nil
		case proto.CmdTime:
			var tm proto.Time
			if json.Unmarshal(req.Payload, &tm) == nil && time.Since(time.UnixMilli(tm.UnixMS)).Abs() < time.Minute {
				timeSent.Store(true)
			}
			return proto.StatusOK, nil
		case proto.CmdState:
			var st proto.State
			json.Unmarshal(req.Payload, &st)
			select {
			case states <- st:
			default:
			}
			return proto.StatusOK, nil
		}
		return proto.StatusUnknownCmd, nil
	})
	return dev, results, states
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDaemonRunsActions(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	dev, results, _ := fakeD200(proto.Version)
	d := newDaemon()
	paths := []string{"/dev/hidraw4"}
	d.find = func() []string { return paths }
	d.open = func(string) (proto.Transport, error) { return dev, nil }
	d.scan()
	waitFor(t, "connection", func() bool { return len(d.getStatus().Connected()) == 1 })
	waitFor(t, "the time", timeSent.Load)

	send := func(id uint32, a deck.Action) proto.Result {
		b, _ := json.Marshal(proto.ActionEvent{ID: id, Host: d.id, Key: 13, Action: a})
		dev.Send(proto.EventSeq, proto.EventAction, b)
		select {
		case r := <-results:
			return r
		case <-time.After(3 * time.Second):
			t.Fatal("no result")
		}
		return proto.Result{}
	}
	if r := send(1, deck.Action{Type: deck.ActionHTTP, URL: srv.URL}); r.ID != 1 || r.Error != "" || hits != 1 {
		t.Errorf("result %+v, %d hits", r, hits)
	}
	if r := send(2, deck.Action{Type: deck.ActionLaunch, Command: []string{"/no/such/program"}}); r.ID != 2 || r.Error == "" {
		t.Errorf("failed action reported %+v", r)
	}
	dev.Send(proto.EventSeq, 0x7f, nil)                      // unknown event: ignored
	dev.Send(proto.EventSeq, proto.EventAction, []byte("{")) // broken event: ignored
	for _, ev := range []proto.ActionEvent{
		{ID: 3, Host: "another deckhost", Action: deck.Action{Type: deck.ActionHTTP, URL: srv.URL}},
		{ID: 1, Host: d.id, Release: true, Action: deck.Action{Type: deck.ActionHTTP, URL: srv.URL}},
	} {
		b, _ := json.Marshal(ev)
		dev.Send(proto.EventSeq, proto.EventAction, b)
	}
	select {
	case r := <-results:
		t.Errorf("ran another host's action or a release: %+v", r)
	case <-time.After(100 * time.Millisecond):
	}

	d.scan() // already running: no second session
	dev.Close()
	waitFor(t, "disconnect", func() bool {
		s := d.getStatus()
		return len(s.Devices) == 1 && s.Devices[0].Hello == nil && s.Devices[0].Error != ""
	})
	paths = nil
	d.scan()
	if n := len(d.getStatus().Devices); n != 0 {
		t.Errorf("%d devices after unplugging", n)
	}
}

func TestDaemonSeveralDevices(t *testing.T) {
	devs := map[string]*prototest.Device{}
	for _, p := range []string{"/dev/hidraw1", "/dev/hidraw2"} {
		devs[p], _, _ = fakeD200(proto.Version)
	}
	devs["/dev/hidraw3"], _, _ = fakeD200(proto.MinVersion - 1)
	devs["/dev/hidraw4"], _, _ = fakeD200(proto.Version + 1)
	d := newDaemon()
	d.os.latest = func(string) (Release, bool, error) { return Release{}, false, nil } // no releases
	d.find = func() []string { return []string{"/dev/hidraw1", "/dev/hidraw2", "/dev/hidraw3", "/dev/hidraw4"} }
	d.open = func(p string) (proto.Transport, error) { return devs[p], nil }
	d.scan()
	waitFor(t, "two connections and two refusals", func() bool {
		s := d.getStatus()
		return len(s.Connected()) == 2 && len(s.Devices) == 4 && strings.Contains(s.Devices[2].Error, "too old") &&
			s.Devices[3].NeedsHost && s.HostUpdate != nil && strings.HasSuffix(s.HostUpdate.URL, "/releases/latest")
	})
	// a device too old to talk to keeps its hello, so it can be updated
	if s := d.getStatus(); s.Devices[2].Hello == nil || len(s.Updatable()) != 3 {
		t.Errorf("too old: %+v", s.Devices[2])
	}
	// told not to look for releases: no link to the official ones
	d.setCheckUpdates(false)
	if s := d.getStatus(); s.CheckUpdates || s.HostUpdate != nil {
		t.Errorf("checks off: %+v", s)
	}
	for _, dev := range devs {
		dev.Close()
	}
}

func TestDaemonOpenFails(t *testing.T) {
	d := newDaemon()
	d.find = func() []string { return []string{"/dev/hidraw5"} }
	d.open = func(string) (proto.Transport, error) { return nil, os.ErrPermission }
	d.scan()
	waitFor(t, "open error", func() bool {
		s := d.getStatus()
		return len(s.Devices) == 1 && strings.Contains(s.Devices[0].Error, "permission")
	})
}

func TestBadHello(t *testing.T) {
	dev := prototest.New()
	go dev.Serve(func(proto.Request) (byte, []byte) { return proto.StatusOK, []byte("not json") })
	defer dev.Close()
	if err := newDaemon().serve(proto.NewConn(dev), "/dev/hidraw0"); err == nil || !strings.Contains(err.Error(), "bad hello") {
		t.Errorf("serve: %v", err)
	}
}

func TestUpdateURL(t *testing.T) {
	dev := func(serial string) Device { return Device{Hello: &proto.Hello{Serial: serial}} }
	one := Status{Devices: []Device{dev("00112233445566aa"), {Error: "e"}}}
	two := Status{Devices: []Device{dev("00112233445566aa"), dev("FFEEDDCCBBAA9988")}}
	cases := []struct {
		s           Status
		serial, url string
		err         string
	}{
		{Status{}, "", "", "no D200 connected"},
		{one, "", "http://198.19.23.5/update", ""},
		{one, "66AA", "http://198.19.23.5/update", ""},
		{one, "9988", "", "no D200 9988 connected"},
		{two, "", "", "several D200s connected: name one by its serial"},
		{two, "aa9988", "http://198.18.72.229/update", ""},
		{Status{Devices: []Device{dev("zz")}}, "", "", `D200 "zz": serial must be 1-64 hex digits`},
		// a device that reports its address, e.g. one set by hand
		{Status{Devices: []Device{{Hello: &proto.Hello{Serial: "00112233445566aa", IPv4: "10.77.0.5"}}}}, "", "http://10.77.0.5/update", ""},
	}
	for _, tc := range cases {
		url, err := tc.s.UpdateURL(tc.serial)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if url != tc.url || msg != tc.err {
			t.Errorf("UpdateURL(%q) = %q, %v; want %q, %q", tc.serial, url, err, tc.url, tc.err)
		}
	}
}

func TestHeartbeatMisses(t *testing.T) {
	old := heartbeatPeriod
	heartbeatPeriod = 5 * time.Millisecond
	t.Cleanup(func() { heartbeatPeriod = old })
	run := func(answers ...byte) *daemon {
		n := 0
		dev, _, _ := fakeD200(proto.Version, func(proto.ActionHost) byte {
			n++
			return answers[(n-1)%len(answers)]
		})
		d := newDaemon()
		d.find = func() []string { return []string{"/dev/hidraw1"} }
		d.open = func(string) (proto.Transport, error) { return dev, nil }
		d.scan()
		t.Cleanup(func() { dev.Close() })
		return d
	}

	// every other heartbeat lost: the session stays up
	d := run(proto.StatusOK, proto.StatusUnknownCmd)
	time.Sleep(100 * time.Millisecond)
	if len(d.getStatus().Connected()) != 1 {
		t.Error("dropped after single misses")
	}
	// two in a row: it reconnects
	d = run(proto.StatusUnknownCmd)
	waitFor(t, "drop", func() bool {
		s := d.getStatus()
		return len(s.Devices) == 1 && s.Devices[0].Error != ""
	})
}

func TestStateResent(t *testing.T) {
	// a restarted deckd has forgotten the state: each heartbeat resends it
	old := heartbeatPeriod
	heartbeatPeriod = 5 * time.Millisecond
	t.Cleanup(func() { heartbeatPeriod = old })
	dev, _, states := fakeD200(proto.Version)
	defer dev.Close()
	d := newDaemon()
	d.setState(map[string]bool{"app.on": true})
	d.find = func() []string { return []string{"/dev/hidraw1"} }
	d.open = func(string) (proto.Transport, error) { return dev, nil }
	d.scan()
	for range 3 {
		select {
		case s := <-states:
			if !s.State["app.on"] {
				t.Fatalf("state %+v", s)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("state not resent")
		}
	}
}

func TestHeartbeat(t *testing.T) {
	var h proto.ActionHost
	if err := json.Unmarshal(newDaemon().heartbeat(), &h); err != nil {
		t.Fatal(err)
	}
	if h.ID == "" || h.Name != "deckhost" || h.OS == "" || len(h.Apps) != 5 ||
		h.Apps[0].Name != "discord" || len(h.Apps[0].Ops) != len(deck.DiscordOps) ||
		h.Apps[1].Name != "media" || h.Apps[2].Name != "mic" || h.Apps[3].Name != "obs" || h.Apps[4].Name != "programs" {
		t.Errorf("%+v", h)
	}
}

func TestStateMessages(t *testing.T) {
	d := newDaemon()
	d.setState(map[string]bool{"a.on": true, "a.off": false})
	msgs, on := d.stateMessages(nil)
	// a new session hears of every name
	if len(msgs) != 1 || string(msgs[0]) != `{"host":"`+d.id+`","state":{"a.off":false,"a.on":true}}` {
		t.Errorf("first %s", msgs)
	}
	d.setState(map[string]bool{"a.on": false, "b.on": true})
	if msgs, _ := d.stateMessages(on); len(msgs) != 1 || string(msgs[0]) != `{"host":"`+d.id+`","state":{"a.on":false,"b.on":true}}` {
		t.Errorf("change %s", msgs)
	}
	// a lot of state goes in several requests
	many := map[string]bool{}
	for i := range 200 {
		many[fmt.Sprintf("obs.scene:Scene number %03d", i)] = i%2 == 0
	}
	d.setState(many)
	msgs, _ = d.stateMessages(nil)
	got := map[string]bool{}
	for _, m := range msgs {
		if len(m) > proto.MaxRequest {
			t.Fatalf("%d bytes", len(m))
		}
		var s proto.State
		json.Unmarshal(m, &s)
		maps.Copy(got, s.State)
	}
	if len(msgs) < 2 || len(got) != 203 || got["obs.scene:Scene number 001"] || !got["obs.scene:Scene number 002"] {
		t.Errorf("%d messages, %d names", len(msgs), len(got))
	}
}

// deckhost tells the device its OS only if the device takes host_os, and
// only when it changed.
func TestSetHostOS(t *testing.T) {
	takes := &deck.FeatureSet{Settings: []string{"host_os", "usb_drive"}}
	for _, tc := range []struct {
		name     string
		features *deck.FeatureSet
		has      string // the device's host_os; "-" for none in its reply
		goos     string
		want     string // what's written; "" for nothing
	}{
		{"no features", nil, "-", "darwin", ""},
		{"no host_os", &deck.FeatureSet{Settings: []string{"usb_drive"}}, "-", "linux", ""},
		{"unknown", takes, "", "linux", `{"host_os":"other"}`},
		{"mac", takes, "", "darwin", `{"host_os":"mac"}`},
		{"unchanged", takes, "other", "windows", ""},
		{"changed", takes, "other", "darwin", `{"host_os":"mac"}`},
	} {
		var reads int
		var wrote string
		do := func(cmd byte, p []byte) ([]byte, error) {
			if cmd != proto.CmdSettings {
				t.Fatalf("%s: command %#x", tc.name, cmd)
			}
			if len(p) > 0 {
				wrote = string(p)
				return nil, nil
			}
			reads++
			if tc.has == "-" {
				return []byte(`{"usb_drive":true}`), nil
			}
			return json.Marshal(proto.Settings{HostOS: &tc.has})
		}
		err := setHostOS(do, proto.Hello{Features: tc.features}, tc.goos)
		if err != nil || wrote != tc.want {
			t.Errorf("%s: wrote %q, %v; want %q", tc.name, wrote, err, tc.want)
		}
		if tc.has == "-" && reads > 0 {
			t.Errorf("%s: read the settings", tc.name)
		}
	}
}

func TestAppQueuesReleaseHeldKeys(t *testing.T) {
	acts := make(chan proto.ActionEvent, 16)
	done := make(chan struct{})
	queue := appQueues(func(a proto.ActionEvent) { acts <- a }, done)
	next := func() proto.ActionEvent {
		t.Helper()
		select {
		case a := <-acts:
			return a
		case <-time.After(3 * time.Second):
			t.Fatal("no action")
			return proto.ActionEvent{}
		}
	}
	for _, a := range []proto.ActionEvent{{Key: 3}, {Key: 4}, {Key: 4, Release: true}} {
		queue(deck.AppMic) <- a
		next()
	}
	close(done) // the device went away with key 3 held
	if a := next(); a.Key != 3 || !a.Release {
		t.Errorf("got %+v, want key 3's release", a)
	}
	select {
	case a := <-acts:
		t.Errorf("extra action %+v", a)
	case <-time.After(50 * time.Millisecond):
	}
}
