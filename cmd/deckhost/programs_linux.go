package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

func (p program) candidates() []candidate { return p.linux }

// findProgram returns how to open the first of cs that's installed, or nil.
func findProgram(cs []candidate) func() error {
	for _, c := range cs {
		switch c.kind {
		case "desktop":
			if open := findDesktop(c.value); open != nil {
				return open
			}
		case "run":
			path, err := exec.LookPath(c.value)
			if err != nil {
				continue
			}
			argv := []string{path}
			for _, a := range c.args {
				if a == "~" {
					a = homeDir()
				}
				argv = append(argv, a)
			}
			return func() error { return startIn(homeDir(), argv...) }
		}
	}
	return nil
}

// findDesktop returns how to open the app of desktop file ID id, or nil if
// it isn't installed. As on the desktop, the first file with the ID counts.
func findDesktop(id string) func() error {
	for _, dir := range applicationDirs() {
		f, err := os.Open(filepath.Join(dir, id+".desktop"))
		if err != nil {
			continue
		}
		e, err := parseDesktopEntry(f)
		f.Close()
		if err != nil || e.Hidden {
			return nil
		}
		if e.TryExec != "" {
			if _, err := exec.LookPath(e.TryExec); err != nil {
				return nil
			}
		}
		path, err := exec.LookPath(e.Exec[0])
		if err != nil {
			return nil
		}
		argv := append([]string{path}, e.Exec[1:]...)
		dir := e.Path
		if dir == "" {
			dir = homeDir()
		}
		return func() error { return startIn(dir, argv...) }
	}
	return nil
}

// homeDir is where apps start unless their desktop file says otherwise, as
// they do from the desktop: deckhost's own directory may be anything.
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// applicationDirs are where desktop files are, the user's first. Flatpak's
// and Snap's are added, since deckhost's service may not have them in
// XDG_DATA_DIRS.
func applicationDirs() []string {
	home, _ := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	dirs := append([]string{dataHome}, filepath.SplitList(dataDirs)...)
	dirs = append(dirs, filepath.Join(home, ".local", "share", "flatpak", "exports", "share"),
		"/var/lib/flatpak/exports/share")
	var out []string
	for _, d := range dirs {
		if d != "" && !slices.Contains(out, filepath.Join(d, "applications")) {
			out = append(out, filepath.Join(d, "applications"))
		}
	}
	return append(out, "/var/lib/snapd/desktop/applications")
}
