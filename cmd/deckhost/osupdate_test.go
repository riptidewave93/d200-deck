package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/riptidewave93/d200-deck/deck"
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
	files := []string{"d200-v0.2.0-release-update.tar", "d200-v0.2.0-debug-update.tar"}
	rel, ok, fail := Release{Version: "v0.2.0", URL: "https://example.com/r", files: files}, true, error(nil)
	u.latest = func(string) (Release, bool, error) { return rel, ok, fail }
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

	// a newer one, told of once its file is uploaded; a failed check keeps
	// what's known
	rel = Release{Version: "v0.3.0", URL: "https://example.com/r3", files: []string{"d200-v0.3.0-debug-update.tar"}}
	u.refresh()
	u.check([]proto.Hello{dev("v0.2.0", "release")})
	if len(notes) != 1 || u.update(dev("v0.2.0", "release")) != nil {
		t.Fatalf("told before the file is there: %+v", notes)
	}
	rel.files = append(rel.files, "d200-v0.3.0-release-update.tar")
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
	assets := `{"name": "a.tar", "state": "uploaded"}, {"name": "SHA256SUMS", "state": "starter"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" || r.URL.Path != "/o/r" {
			t.Errorf("User-Agent %q, path %q", r.Header.Get("User-Agent"), r.URL.Path)
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"tag_name": "v0.2.0", "html_url": "https://example.com/r", "assets": [` + assets + `]}`))
	}))
	defer srv.Close()
	defer func(u string) { latestURL = u }(latestURL)
	latestURL = srv.URL + "/%s"

	if rel, ok, err := latestRelease("o/r"); err != nil || !ok || rel.Version != "v0.2.0" || rel.URL != "https://example.com/r" ||
		!slices.Equal(rel.files, []string{"a.tar"}) {
		t.Errorf("%+v %v %v", rel, ok, err)
	}
	assets = `{"name": "a.tar", "state": "starter"}` // published, files still uploading
	if _, ok, err := latestRelease("o/r"); ok || err != nil {
		t.Errorf("no files: %v %v", ok, err)
	}
	status = http.StatusNotFound // no releases yet
	if _, ok, err := latestRelease("o/r"); ok || err != nil {
		t.Errorf("404: %v %v", ok, err)
	}
	status = http.StatusForbidden // rate limited
	if _, _, err := latestRelease("o/r"); err == nil {
		t.Error("403: no error")
	}
}

func TestHostUpdate(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "v0.1.0"
	var notes []string
	u := newOSUpdates(func(title, body, url string) { notes = append(notes, title+" "+url) })
	files := []string{"deckhost-0.2.0-windows-amd64-setup.exe"}
	u.latest = func(repo string) (Release, bool, error) {
		if repo == deck.HostRepo {
			return Release{Version: "v0.2.0", URL: "https://example.com/host", files: files}, true, nil
		}
		return Release{}, false, nil
	}
	if u.hostUpdate() != nil {
		t.Error("an update before looking")
	}
	if err := u.refresh(); err != nil {
		t.Fatal(err)
	}
	u.check(nil)
	if u.hostUpdate() != nil || len(notes) != 0 {
		t.Errorf("an update before %s is uploaded: %q", hostReadyFile, notes)
	}
	files = append(files, hostReadyFile)
	u.refresh()
	u.check(nil)
	u.check(nil) // once per release
	if up := u.hostUpdate(); up == nil || up.Version != "v0.2.0" || len(notes) != 1 || notes[0] != "deckhost v0.2.0 is out https://example.com/host" {
		t.Errorf("%+v %q", up, notes)
	}
	version = "v0.2.0"
	if u.hostUpdate() != nil {
		t.Error("an update to the same version")
	}
}
