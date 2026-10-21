package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func (p program) candidates() []candidate { return p.windows }

// findProgram returns how to open the first of cs that's installed, or nil.
func findProgram(cs []candidate) func() error {
	for _, c := range cs {
		switch c.kind {
		case "exe":
			if appPath(c.value) {
				name := c.value
				return func() error { return openURL(name) } // ShellExecute reads App Paths
			}
			if path, err := exec.LookPath(c.value); err == nil {
				return func() error { return start(path) }
			}
		case "file":
			p, err := registry.ExpandString(c.value)
			if err != nil {
				continue
			}
			matches, _ := filepath.Glob(p)
			if len(matches) == 0 {
				continue
			}
			path := matches[len(matches)-1]
			argv := append([]string{path}, c.args...)
			return func() error { return startIn(filepath.Dir(path), argv...) }
		case "uri":
			if uriScheme(c.value) {
				u := c.value
				return func() error { return openURL(u) }
			}
		}
	}
	return nil
}

// appPath reports whether program name (e.g. chrome.exe) is registered in
// App Paths, for the user or the PC, to a file that's there.
func appPath(name string) bool {
	const key = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`
	for _, k := range []struct {
		root   registry.Key
		access uint32
	}{
		{registry.CURRENT_USER, 0},
		{registry.LOCAL_MACHINE, 0},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
	} {
		h, err := registry.OpenKey(k.root, key+name, registry.QUERY_VALUE|k.access)
		if err != nil {
			continue
		}
		v, _, err := h.GetStringValue("")
		h.Close()
		if err != nil {
			continue
		}
		if v, err = registry.ExpandString(strings.Trim(v, `"`)); err == nil {
			if _, err := os.Stat(v); err == nil {
				return true
			}
		}
	}
	return false
}

// uriScheme reports whether the scheme of URI u (e.g. "spotify:") is
// registered.
func uriScheme(u string) bool {
	scheme, _, _ := strings.Cut(u, ":")
	h, err := registry.OpenKey(registry.CLASSES_ROOT, scheme, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer h.Close()
	_, _, err = h.GetStringValue("URL Protocol")
	return err == nil
}
