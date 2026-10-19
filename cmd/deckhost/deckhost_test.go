package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if run(deck.Action{Type: deck.ActionKey}) == nil {
		t.Error("ran a device action")
	}
}

func TestControl(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	urls := fakeBrowser(t)
	d := newDaemon()
	d.devices["/dev/hidraw9"] = &Device{Path: "/dev/hidraw9", Error: "not yet"}
	l, err := listenControl(d)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	reply, err := control("status")
	var s Status
	if err != nil || json.Unmarshal([]byte(reply), &s) != nil || len(s.Devices) != 1 || s.Devices[0].Error != "not yet" {
		t.Errorf("status %q, %v", reply, err)
	}
	for _, cmd := range []string{"configure", "update"} {
		if reply, err := control(cmd); err != nil || reply != "ok" {
			t.Errorf("%s: %q, %v", cmd, reply, err)
		}
	}
	if u := urls(); len(u) != 2 || u[0] != deck.ConfigureURL || u[1] != deck.UpdateURL {
		t.Errorf("opened %v", u)
	}
	if _, err := control("dance"); err == nil || err.Error() != "unknown command" {
		t.Errorf("unknown command: %v", err)
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

// fakeD200 serves hello and action-host like deckd and sends results to the
// returned channel.
func fakeD200(protocol int) (*prototest.Device, chan proto.Result) {
	dev := prototest.New()
	results := make(chan proto.Result, 4)
	go dev.Serve(func(req proto.Request) (byte, []byte) {
		switch req.Cmd {
		case proto.CmdHello:
			b, _ := json.Marshal(proto.Hello{Protocol: protocol, Serial: "00112233445566aa", Keys: 14})
			return proto.StatusOK, b
		case proto.CmdActionHost:
			return proto.StatusOK, nil
		case proto.CmdResult:
			var r proto.Result
			json.Unmarshal(req.Payload, &r)
			results <- r
			return proto.StatusOK, nil
		}
		return proto.StatusUnknownCmd, nil
	})
	return dev, results
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

	dev, results := fakeD200(proto.Version)
	d := newDaemon()
	paths := []string{"/dev/hidraw4"}
	d.find = func() []string { return paths }
	d.open = func(string) (proto.Transport, error) { return dev, nil }
	d.scan()
	waitFor(t, "connection", func() bool { return len(d.getStatus().Connected()) == 1 })

	send := func(id uint32, a deck.Action) proto.Result {
		b, _ := json.Marshal(proto.ActionEvent{ID: id, Key: 13, Action: a})
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
		devs[p], _ = fakeD200(proto.Version)
	}
	old, _ := fakeD200(1)
	devs["/dev/hidraw3"] = old
	d := newDaemon()
	d.find = func() []string { return []string{"/dev/hidraw1", "/dev/hidraw2", "/dev/hidraw3"} }
	d.open = func(p string) (proto.Transport, error) { return devs[p], nil }
	d.scan()
	waitFor(t, "two connections and one refusal", func() bool {
		s := d.getStatus()
		return len(s.Connected()) == 2 && len(s.Devices) == 3 && strings.Contains(s.Devices[2].Error, "protocol 1")
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

func TestIcon(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(drawIcon(0x2f6fde)))
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
