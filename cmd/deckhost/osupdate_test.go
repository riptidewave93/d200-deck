package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/proto"
)

func TestNewerOS(t *testing.T) {
	for _, c := range []struct {
		tag, have string
		want      bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.10.0", "v0.9.3", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "v0.1.0-3-g1a2b3c4", false}, // past the release
		{"v0.2.0", "v0.1.0-3-g1a2b3c4-dirty", true},
		{"v0.1.0", "v0.1.0-local", false},
		{"v0.1.0", "v0.1.0-rc1", true},
		{"v0.1.0", "v0.1.0-rc1-2-gabc1234", true},
		{"v0.2.0", "469d57d", false}, // a build with no tag
		{"v0.2.0", "", false},
		{"nightly", "v0.1.0", false},
	} {
		if got := newerOS(c.tag, c.have); got != c.want {
			t.Errorf("newerOS(%q, %q) = %v", c.tag, c.have, got)
		}
	}
}

func TestOSUpdates(t *testing.T) {
	type note struct{ title, body, url string }
	var notes []note
	u := newOSUpdates(func(title, body, url string) { notes = append(notes, note{title, body, url}) })
	rel, ok, fail := OSUpdate{Version: "v0.2.0", URL: "https://example.com/r"}, true, error(nil)
	u.latest = func() (OSUpdate, bool, error) { return rel, ok, fail }
	dev := func(v, flavour string) proto.Hello {
		return proto.Hello{OS: osrelease.Info{Version: v, Flavour: flavour}}
	}

	u.check([]proto.Hello{dev("v0.1.0", "release")}) // not known yet
	if err := u.refresh(); err != nil || len(notes) != 0 {
		t.Fatal(err, notes)
	}
	u.check([]proto.Hello{dev("469d57d", "release"), dev("v0.2.0", "release")})
	if len(notes) != 0 {
		t.Errorf("none older: %v", notes)
	}
	u.check([]proto.Hello{dev("v0.1.0", "debug")})
	u.check([]proto.Hello{dev("v0.1.0", "release")}) // once per release
	if len(notes) != 1 || notes[0].url != rel.URL || notes[0].title != "Device OS v0.2.0 is out" {
		t.Fatalf("%+v", notes)
	}
	if want := "Your device has v0.1.0. Download d200-v0.2.0-debug-update.tar from the release page, then open Update in the deckhost menu."; notes[0].body != want {
		t.Errorf("body %q", notes[0].body)
	}
	if up := u.update(dev("v0.1.0", "")); up == nil || up.File != "d200-v0.2.0-release-update.tar" {
		t.Errorf("update %+v", up)
	}

	// a newer one: told again; a failed check keeps what's known
	rel = OSUpdate{Version: "v0.3.0", URL: "https://example.com/r3"}
	u.refresh()
	fail = errors.New("offline")
	if u.refresh() == nil {
		t.Error("no error")
	}
	u.check([]proto.Hello{dev("v0.2.0", "release")})
	if len(notes) != 2 || notes[1].url != rel.URL {
		t.Errorf("%+v", notes)
	}

	// no release any more
	fail, ok = nil, false
	u.refresh()
	if u.update(dev("v0.1.0", "release")) != nil {
		t.Error("an update with no release")
	}
}

func TestLatestOS(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent")
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"tag_name": "v0.2.0", "html_url": "https://example.com/r", "assets": []}`))
	}))
	defer srv.Close()
	defer func(u string) { latestOSURL = u }(latestOSURL)
	latestOSURL = srv.URL

	if rel, ok, err := latestOS(); err != nil || !ok || rel.Version != "v0.2.0" || rel.URL != "https://example.com/r" {
		t.Errorf("%+v %v %v", rel, ok, err)
	}
	status = http.StatusNotFound // no releases yet
	if _, ok, err := latestOS(); ok || err != nil {
		t.Errorf("404: %v %v", ok, err)
	}
	status = http.StatusForbidden // rate limited
	if _, _, err := latestOS(); err == nil {
		t.Error("403: no error")
	}
}
