package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

// OSUpdate is a device OS release newer than a device's.
type OSUpdate struct {
	Version string `json:"version"` // its tag, e.g. v0.2.0
	URL     string `json:"url"`     // its release page
	File    string `json:"file"`    // its file for the device's update page
}

// How often deckhost looks for a new device OS release, and again after
// failing to.
const (
	osCheckEvery = 24 * time.Hour
	osRetry      = time.Hour
)

var latestOSURL = "https://api.github.com/repos/" + deck.OSRepo + "/releases/latest"

// latestOS reads the newest published device OS release; ok is false if
// there's none yet.
func latestOS() (rel OSUpdate, ok bool, err error) {
	req, err := http.NewRequest("GET", latestOSURL, nil)
	if err != nil {
		return rel, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "deckhost/"+version)
	resp, err := artClient.Do(req)
	if err != nil {
		return rel, false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return rel, false, nil
	default:
		return rel, false, fmt.Errorf("GitHub: %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return rel, false, err
	}
	return OSUpdate{Version: r.Tag, URL: r.URL}, r.Tag != "", nil
}

// osUpdates looks for new device OS releases at start and every day, and
// tells the user of each once if a connected device is older.
type osUpdates struct {
	latest func() (OSUpdate, bool, error)
	notify func(title, body, url string)

	mu   sync.Mutex
	rel  *OSUpdate // the newest release, if known
	told map[string]bool
}

func newOSUpdates(notify func(title, body, url string)) *osUpdates {
	return &osUpdates{latest: latestOS, notify: notify, told: map[string]bool{}}
}

// run checks until the process exits; devices are those connected.
func (u *osUpdates) run(devices func() []proto.Hello) {
	for {
		wait := osCheckEvery
		if err := u.refresh(); err != nil {
			log.Printf("OS releases: %v", err)
			wait = osRetry
		}
		u.check(devices())
		time.Sleep(wait)
	}
}

func (u *osUpdates) refresh() error {
	rel, ok, err := u.latest()
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rel = nil
	if ok {
		u.rel = &rel
	}
	return nil
}

// check tells the user of the newest release if a device is older, unless
// it has already.
func (u *osUpdates) check(devices []proto.Hello) {
	for _, h := range devices {
		up := u.update(h)
		if up == nil {
			continue
		}
		u.mu.Lock()
		told := u.told[up.Version]
		u.told[up.Version] = true
		u.mu.Unlock()
		if !told {
			u.notify("Device OS "+up.Version+" is out",
				fmt.Sprintf("Your device has %s. Download %s from the release page, then open Update in the deckhost menu.", h.OS.Version, up.File),
				up.URL)
		}
		return
	}
}

// update is the newest release if it's newer than device h's OS.
func (u *osUpdates) update(h proto.Hello) *OSUpdate {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.rel == nil || !newerOS(u.rel.Version, h.OS.Version) {
		return nil
	}
	up := *u.rel
	flavour := h.OS.Flavour
	if flavour == "" {
		flavour = "release"
	}
	up.File = fmt.Sprintf(deck.OSUpdateFile, up.Version, flavour)
	return &up
}

var (
	osVersionRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(.*)$`)
	// what git describe adds to a tag: commits past it, and local changes
	describeRE = regexp.MustCompile(`(-\d+-g[0-9a-f]+)?(-dirty)?(-local)?$`)
)

// newerOS reports whether release tag is newer than device OS version
// have. A version without a leading vX.Y.Z, such as a bare commit, never
// has a newer release.
func newerOS(tag, have string) bool {
	t, tpre, ok1 := parseOSVersion(tag)
	h, hpre, ok2 := parseOSVersion(have)
	if !ok1 || !ok2 {
		return false
	}
	if c := slices.Compare(t, h); c != 0 {
		return c > 0
	}
	return hpre != "" && tpre == "" // a release is newer than its pre-releases
}

func parseOSVersion(s string) (core []int, pre string, ok bool) {
	m := osVersionRE.FindStringSubmatch(s)
	if m == nil {
		return nil, "", false
	}
	for _, n := range m[1:4] {
		i, err := strconv.Atoi(n)
		if err != nil {
			return nil, "", false
		}
		core = append(core, i)
	}
	return core, describeRE.ReplaceAllString(m[4], ""), true
}
