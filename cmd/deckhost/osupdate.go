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

// Release is a newer release of the device OS or deckhost.
type Release struct {
	Version string `json:"version"`        // its tag, e.g. v0.2.0
	URL     string `json:"url"`            // its release page
	File    string `json:"file,omitempty"` // an OS release's file for the device's update page

	files []string // its uploaded files
}

// hostReadyFile is the file a deckhost release uploads last, once its
// installers are there.
const hostReadyFile = "SHA256SUMS"

// How often deckhost looks for new device OS and deckhost releases, and
// again after failing to.
const (
	osCheckEvery = 24 * time.Hour
	osRetry      = time.Hour
)

var latestURL = "https://api.github.com/repos/%s/releases/latest"

// latestRelease reads GitHub repo's newest published release; ok is false if
// there's none yet, or it has no files yet.
func latestRelease(repo string) (rel Release, ok bool, err error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(latestURL, repo), nil)
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
		Tag    string `json:"tag_name"`
		URL    string `json:"html_url"`
		Assets []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return rel, false, err
	}
	rel = Release{Version: r.Tag, URL: r.URL}
	for _, a := range r.Assets {
		if a.State == "uploaded" {
			rel.files = append(rel.files, a.Name)
		}
	}
	return rel, r.Tag != "" && len(rel.files) > 0, nil
}

// osUpdates looks for new device OS and deckhost releases at start and every
// day, and tells the user of each once if it's newer than a connected
// device's OS, or than deckhost.
type osUpdates struct {
	latest func(repo string) (Release, bool, error)
	notify func(title, body, url string)

	mu   sync.Mutex
	rel  *Release // the newest OS release, if known
	host *Release // the newest deckhost release, if known
	told map[string]bool
}

func newOSUpdates(notify func(title, body, url string)) *osUpdates {
	return &osUpdates{latest: latestRelease, notify: notify, told: map[string]bool{}}
}

// run checks until the process exits; devices are those connected.
func (u *osUpdates) run(devices func() []proto.Hello) {
	for {
		wait := osCheckEvery
		if err := u.refresh(); err != nil {
			log.Printf("releases: %v", err)
			wait = osRetry
		}
		u.check(devices())
		time.Sleep(wait)
	}
}

func (u *osUpdates) refresh() error {
	rel, ok, err := u.latest(deck.OSRepo)
	if err != nil {
		return err
	}
	host, hostOK, err := u.latest(deck.HostRepo)
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rel, u.host = nil, nil
	if ok {
		u.rel = &rel
	}
	if hostOK {
		u.host = &host
	}
	return nil
}

// check tells the user of the newest releases if a device or deckhost is
// older, unless it has already.
func (u *osUpdates) check(devices []proto.Hello) {
	if up := u.hostUpdate(); up != nil && u.first("deckhost "+up.Version) {
		u.notify("deckhost "+up.Version+" is out",
			fmt.Sprintf("You have deckhost %s. Download the new one from the release page.", version), up.URL)
	}
	for _, h := range devices {
		if up := u.update(h); up != nil {
			if u.first(up.Version) {
				u.notify("Device OS "+up.Version+" is out",
					fmt.Sprintf("Your device has %s. Download %s from the release page, then open Update in the deckhost menu.", h.OS.Version, up.File),
					up.URL)
			}
			return
		}
	}
}

// first reports whether the user hasn't been told of what yet.
func (u *osUpdates) first(what string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	told := u.told[what]
	u.told[what] = true
	return !told
}

// hostUpdate is the newest deckhost release if it's newer than this one and
// its installers are uploaded.
func (u *osUpdates) hostUpdate() *Release {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.host == nil || !newerOS(u.host.Version, version) || !slices.Contains(u.host.files, hostReadyFile) {
		return nil
	}
	up := *u.host
	return &up
}

// update is the newest OS release if it's newer than device h's OS and its
// file for h is uploaded.
func (u *osUpdates) update(h proto.Hello) *Release {
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
	if !slices.Contains(up.files, up.File) {
		return nil // not uploaded yet
	}
	return &up
}

var (
	osVersionRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(.*)$`)
	// what git describe adds to a tag: commits past it, and local changes
	describeRE = regexp.MustCompile(`(-\d+-g[0-9a-f]+)?(-dirty)?(-local)?$`)
)

// newerOS reports whether release tag is newer than version have (the
// device OS's or deckhost's). A version without a leading vX.Y.Z, such as a
// bare commit, never has a newer release.
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
