package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/proto"
)

func TestInstall(t *testing.T) {
	const file = "d200-v0.2.0-release-update.tar"
	bundle := bytes.Repeat([]byte("bundle"), 1000)
	sum := sha256.Sum256(bundle)
	sums := hex.EncodeToString(sum[:]) + "  " + file + "\n"
	var mu sync.Mutex // guards the servers' and notify's variables
	var got [][]byte
	var notes []string
	refuse := ""
	locked := func(f func()) {
		mu.Lock()
		defer mu.Unlock()
		f()
	}
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/sums":
			io.WriteString(w, sums)
		case "/file":
			w.Write(bundle)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()

	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "PUT" || r.URL.Path != "/update/bundle" || r.Header.Get("X-Deck-Update") == "" || r.ContentLength != int64(len(b)) {
			t.Errorf("%s %s %q %d", r.Method, r.URL.Path, r.Header.Get("X-Deck-Update"), r.ContentLength)
		}
		if refuse != "" {
			http.Error(w, refuse, http.StatusConflict)
			return
		}
		got = append(got, b)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer device.Close()

	rel := Release{Version: "v0.2.0", URL: "https://example.com/r", files: []string{file, readyFile},
		urls: map[string]string{file: gh.URL + "/file", readyFile: gh.URL + "/sums"}}
	d := newDaemon()
	d.os.latest = func(repo string) (Release, bool, error) { return rel, repo != "riptidewave93/d200-deck", nil }
	d.os.notify = func(title, body, url string) { locked(func() { notes = append(notes, title) }) }
	d.os.refresh()
	const path = "/dev/hidraw1"
	d.devices[path] = &Device{Path: path, Hello: &proto.Hello{Serial: "00112233445566aa",
		IPv4: strings.TrimPrefix(device.URL, "http://"), OS: osrelease.Info{Version: "v0.1.0", Flavour: "release"}}}
	opened := fakeBrowser(t)
	run := func() Device {
		t.Helper()
		if err := d.startInstall("66aa"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the install", func() bool { return d.getStatus().Devices[0].Install == "" })
		return d.getStatus().Devices[0]
	}

	dev := run()
	locked(func() {
		if dev.InstallError != "" || len(got) != 1 || !bytes.Equal(got[0], bundle) ||
			!slices.Equal(opened(), []string{device.URL + "/update"}) || !slices.Equal(notes, []string{"Press Install on the device"}) {
			t.Fatalf("%q %d %q %q", dev.InstallError, len(got), opened(), notes)
		}
	})

	// one at a time
	d.setInstall(path, "", "Sending to device…")
	if err := d.startInstall(""); err == nil {
		t.Error("two installs at once")
	}
	d.setInstall(path, "Sending to device…", "")

	// the device refuses it: the reason, and the release page
	locked(func() { refuse = "an update is already running" })
	dev = run()
	locked(func() {
		if !strings.Contains(dev.InstallError, refuse) || opened()[1] != rel.URL || notes[1] != "Device update failed" {
			t.Errorf("%q %q %q", dev.InstallError, opened(), notes)
		}
	})

	// a bad download never reaches the device
	locked(func() { refuse, bundle = "", []byte("tampered") })
	dev = run()
	locked(func() {
		if !strings.Contains(dev.InstallError, "doesn't match") || len(got) != 1 {
			t.Errorf("%q %d", dev.InstallError, len(got))
		}
	})

	// no update for it
	d.mu.Lock()
	d.devices[path].Hello.OS.Version = "v0.2.0"
	d.mu.Unlock()
	if err := d.startInstall(""); err == nil || !strings.Contains(err.Error(), "no update") {
		t.Errorf("up to date: %v", err)
	}
}

func TestSumOf(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("B", 64)
	sums := []byte(a + "  one.tar\n" + b + " *two.tar\nbad  three.tar\n")
	if s, err := sumOf(sums, "two.tar"); err != nil || s != strings.ToLower(b) {
		t.Errorf("%q %v", s, err)
	}
	if s, err := sumOf(sums, "one.tar"); err != nil || s != a {
		t.Errorf("%q %v", s, err)
	}
	for _, f := range []string{"three.tar", "four.tar"} {
		if _, err := sumOf(sums, f); err == nil {
			t.Errorf("%s: no error", f)
		}
	}
}
