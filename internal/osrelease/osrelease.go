// Package osrelease reads the OS version from /etc/os-release.
package osrelease

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Path is where the OS describes itself.
const Path = "/etc/os-release"

// Info is the subset of os-release the deck tools report.
type Info struct {
	Name    string `json:"name"`    // PRETTY_NAME
	Version string `json:"version"` // VERSION_ID
	Flavour string `json:"flavour"` // VARIANT_ID: release or debug
	Build   string `json:"build"`   // BUILD_ID
}

// Read parses an os-release file.
func Read(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	var i Info
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), "=")
		if !ok {
			continue
		}
		if u, err := strconv.Unquote(v); err == nil {
			v = u
		}
		switch k {
		case "PRETTY_NAME":
			i.Name = v
		case "VERSION_ID":
			i.Version = v
		case "VARIANT_ID":
			i.Flavour = v
		case "BUILD_ID":
			i.Build = v
		}
	}
	return i, s.Err()
}
