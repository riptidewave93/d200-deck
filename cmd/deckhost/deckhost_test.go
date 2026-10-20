package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/proto/prototest"
)

// fakeBrowser records the URLs openURL is asked to open.
func fakeBrowser(t *testing.T) func() []string {
	var mu sync.Mutex
	var urls []string
	old := openURL
	openURL = func(u string) error {
		mu.Lock()
		defer mu.Unlock()
		urls = append(urls, u)
		return nil
	}
	t.Cleanup(func() { openURL = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), urls...)
	}
}

func TestRunHTTP(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+r.Header.Get("X-Test")+" "+string(body))
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	if err := run(deck.Action{Type: deck.ActionHTTP, URL: srv.URL + "/get"}); err != nil {
		t.Error(err)
	}
	if err := run(deck.Action{Type: deck.ActionHTTP, URL: srv.URL + "/post", Body: "hi", Headers: map[string]string{"X-Test": "1"}}); err != nil {
		t.Error(err)
	}
	if err := run(deck.Action{Type: deck.ActionHTTP, Method: "PUT", URL: srv.URL + "/put"}); err != nil {
		t.Error(err)
	}
	if err := run(deck.Action{Type: deck.ActionHTTP, URL: srv.URL + "/fail"}); err == nil {
		t.Error("no error for a 500")
	}
	want := []string{"GET /get  ", "POST /post 1 hi", "PUT /put  ", "GET /fail  "}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests %q", got)
	}
	if run(deck.Action{Type: deck.ActionHTTP, Method: "BAD METHOD", URL: srv.URL}) == nil {
		t.Error("no error for a bad method")
	}
	if run(deck.Action{Type: deck.ActionHTTP, URL: "http://127.0.0.1:1/"}) == nil {
		t.Error("no error for a refused connection")
	}
}

func TestRunOther(t *testing.T) {
	urls := fakeBrowser(t)
	if err := run(deck.Action{Type: deck.ActionOpenURL, URL: "https://example.com"}); err != nil {
		t.Error(err)
	}
	if u := urls(); len(u) != 1 || u[0] != "https://example.com" {
		t.Errorf("opened %v", u)
	}
	if err := run(deck.Action{Type: deck.ActionLaunch, Command: []string{"true"}}); err != nil {
		t.Error(err)
	}
	if run(deck.Action{Type: deck.ActionLaunch, Command: []string{"/no/such/program"}}) == nil {
		t.Error("launched a missing program")
	}
	for _, cmd := range [][]string{nil, {""}} {
		if run(deck.Action{Type: deck.ActionLaunch, Command: cmd}) == nil {
			t.Errorf("launched %q", cmd)
		}
	}
	if run(deck.Action{Type: deck.ActionKey}) == nil {
		t.Error("ran a device action")
	}
}

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

func TestControl(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	urls := fakeBrowser(t)
	d := newDaemon()
	d.devices["/dev/hidraw9"] = &Device{Path: "/dev/hidraw9", Error: "not yet"}
	d.devices["/dev/hidraw1"] = &Device{Path: "/dev/hidraw1", Hello: &proto.Hello{Serial: "00112233445566aa"}}
	l, err := listenControl(d)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	reply, err := control("status")
	var s Status
	if err != nil || json.Unmarshal([]byte(reply), &s) != nil || len(s.Devices) != 2 || s.Devices[1].Error != "not yet" {
		t.Errorf("status %q, %v", reply, err)
	}
	for _, cmd := range []string{"configure", "update", "update 5566aa"} {
		if reply, err := control(cmd); err != nil || reply != "ok" {
			t.Errorf("%s: %q, %v", cmd, reply, err)
		}
	}
	const page = "http://198.19.23.5/update"
	if u := urls(); len(u) != 3 || u[0] != deck.ConfigureURL || u[1] != page || u[2] != page {
		t.Errorf("opened %v", u)
	}
	if _, err := control("update ffff"); err == nil || err.Error() != "no D200 ffff connected" {
		t.Errorf("update of a missing deck: %v", err)
	}
	for _, cmd := range []string{"dance", "status now"} {
		if _, err := control(cmd); err == nil || err.Error() != "unknown command" {
			t.Errorf("%s: %v", cmd, err)
		}
	}
	if _, err := listenControl(d); err == nil {
		t.Error("a second daemon started")
	}
	info, _ := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "deckhost.sock"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("socket mode %v", info.Mode().Perm())
	}
}

func TestControlStaleSocket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	os.WriteFile(filepath.Join(dir, "deckhost.sock"), nil, 0o600) // left by a crash
	l, err := listenControl(newDaemon())
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	if _, err := control("status"); err == nil {
		t.Error("talked to a stopped daemon")
	}
}

func TestControlNoRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := listenControl(newDaemon()); err == nil {
		t.Error("listened without XDG_RUNTIME_DIR")
	}
	if _, err := control("status"); err == nil {
		t.Error("connected without XDG_RUNTIME_DIR")
	}
}

func TestControlGarbage(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	l, err := listenControl(newDaemon())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	path, _ := socketPath()
	c, _ := net.Dial("unix", path)
	c.Write([]byte("no newline"))
	c.(*net.UnixConn).CloseWrite()
	b, _ := io.ReadAll(c)
	c.Close()
	if len(b) != 0 {
		t.Errorf("answered a partial command: %q", b)
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
	d.os = nil // no release checks
	d.find = func() []string { return []string{"/dev/hidraw1", "/dev/hidraw2", "/dev/hidraw3", "/dev/hidraw4"} }
	d.open = func(p string) (proto.Transport, error) { return devs[p], nil }
	d.scan()
	waitFor(t, "two connections and two refusals", func() bool {
		s := d.getStatus()
		return len(s.Connected()) == 2 && len(s.Devices) == 4 && strings.Contains(s.Devices[2].Error, "too old") &&
			s.Devices[3].NeedsHost && s.HostUpdate != nil && strings.HasSuffix(s.HostUpdate.URL, "/releases/latest")
	})
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

func TestDescribe(t *testing.T) {
	hello := func(serial, version string) *proto.Hello {
		h := &proto.Hello{Serial: serial}
		h.OS.Version, h.OS.Flavour = version, "debug"
		return h
	}
	cases := []struct {
		s       Status
		running bool
		header  string
		lines   []string
	}{
		{Status{}, false, "deckhost isn't running", nil},
		{Status{}, true, "D200 not connected", nil},
		{Status{Devices: []Device{{Path: "/dev/hidraw1", Hello: hello("00112233445566aa", "v1")}}}, true,
			"D200 connected", []string{"OS v1 (debug)"}},
		{Status{Devices: []Device{
			{Path: "/dev/hidraw1", Hello: hello("00112233445566aa", "v1")},
			{Path: "/dev/hidraw2", Hello: hello("0123456789abcdef", "v2")},
			{Path: "/dev/hidraw3", Error: "device speaks protocol 1"},
			{Path: "/dev/hidraw4"},
		}}, true, "2 D200s connected", []string{
			"D200 …5566aa: OS v1 (debug)", "D200 …abcdef: OS v2 (debug)", "hidraw3: device speaks protocol 1",
		}},
		{Status{Apps: []AppStatus{{"Discord", "not running"}}}, true, "D200 not connected", []string{"Discord: not running"}},
	}
	for _, tc := range cases {
		header, lines := describe(tc.s, tc.running)
		if header != tc.header || strings.Join(lines, "|") != strings.Join(tc.lines, "|") {
			t.Errorf("describe: %q %q, want %q %q", header, lines, tc.header, tc.lines)
		}
	}
	long := Status{Devices: []Device{{Path: "/dev/hidraw1", Error: strings.Repeat("x", 200)}}}
	if _, lines := describe(long, true); len(lines[0]) > 80 {
		t.Errorf("line not shortened: %d chars", len(lines[0]))
	}
	var many Status
	for range maxTrayDevices + 3 {
		many.Devices = append(many.Devices, Device{Path: "/dev/x", Error: "e"})
	}
	if _, lines := describe(many, true); len(lines) != maxTrayDevices {
		t.Errorf("%d lines", len(lines))
	}
	if shortSerial("abc") != "D200 …abc" {
		t.Error("short serial changed")
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

func TestUpdateSerials(t *testing.T) {
	var s Status
	for i := range maxTrayDevices + 2 {
		s.Devices = append(s.Devices, Device{Hello: &proto.Hello{Serial: fmt.Sprintf("%016x", i)}}, Device{Error: "e"})
	}
	got := updateSerials(s)
	if len(got) != maxTrayDevices || got[0] != "0000000000000000" || got[1] != "0000000000000001" {
		t.Errorf("updateSerials = %q", got)
	}
	if updateSerials(Status{}) != nil {
		t.Error("serials with nothing connected")
	}
	if notRunning() == nil {
		t.Error("notRunning returned nil")
	}
}

func TestIcon(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(drawIcon(iconBlue, trayIconSize, false)))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("icon is %v", b)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("corner isn't transparent")
	}
	if r, g, b, a := img.At(16, 24).RGBA(); a>>8 != 0xff || r>>8 != 0xff || g>>8 != 0xff || b>>8 != 0xff {
		t.Errorf("key pixel %v", img.At(16, 24))
	}
	if r, _, b, _ := img.At(4, 32).RGBA(); r>>8 != 0x2f || b>>8 != 0xde {
		t.Errorf("body pixel %v", img.At(4, 32))
	}
}

func TestDaemonStatusWithoutDaemon(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, running := daemonStatus(); running {
		t.Error("daemon reported running")
	}
	fallback := false
	ask("configure", func() error { fallback = true; return nil })
	if !fallback {
		t.Error("no fallback without a daemon")
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

func TestIcoFromPNG(t *testing.T) {
	p := drawIcon(iconBlue, trayIconSize, false)
	ico := icoFromPNG(p)
	if len(ico) != 22+len(p) || ico[2] != 1 || ico[4] != 1 || ico[6] != 64 || ico[7] != 64 || ico[18] != 22 || !bytes.Equal(ico[22:], p) {
		t.Errorf("ico header % x", ico[:22])
	}
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
	if h.ID == "" || h.Name != "deckhost" || h.OS == "" || len(h.Apps) != 3 ||
		h.Apps[0].Name != "discord" || len(h.Apps[0].Ops) != len(deck.DiscordOps) ||
		h.Apps[1].Name != "media" || h.Apps[2].Name != "obs" {
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

func TestWebURL(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://example.com/x": true, "http://localhost:8080": true,
		"file:///etc/passwd": false, `\\host\share\x.exe`: false, `C:\Windows\System32\cmd.exe`: false,
		"javascript:alert(1)": false, "https:example.com": false, "": false,
	} {
		if webURL(u) != ok {
			t.Errorf("%q: %v", u, !ok)
		}
	}
	if err := run(deck.Action{Type: deck.ActionOpenURL, URL: "file:///etc/passwd"}); err == nil {
		t.Error("opened a file URL")
	}
}
