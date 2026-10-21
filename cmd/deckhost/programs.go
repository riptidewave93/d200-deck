package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// The "Open app" keys: {"type": "app", "app": "programs", "op": "open",
// "args": {"id": "chrome"}}. deckhost opens only the apps in its own list,
// finding each the way its OS installs it. It sets "programs.available:<id>"
// for the apps it found, so the configuration website can grey out the rest.
const (
	appPrograms  = "programs"
	programsOpen = "open"
)

// A candidate is one way an app may be installed. Each OS lists its own in
// programCandidates and finds them with findProgram.
type candidate struct {
	kind  string   // exe, file, uri (Windows); desktop, run (Linux)
	value string   // its name, path, URI or desktop file ID
	args  []string // for file and run
}

// programs are the ids of the apps "Open app" keys can open. Keep docs/api.md's
// list in step.
var programs = []string{
	// system
	"files", "settings", "terminal", "task-manager", "calculator", "text-editor", "screenshot",
	"control-panel",
	// browsers
	"chrome", "firefox", "edge", "brave", "opera", "vivaldi",
	// chat
	"discord", "telegram", "whatsapp", "signal", "zoom", "slack", "teams",
	// games
	"steam", "epic-games", "battle-net", "gog-galaxy", "ea-app", "ubisoft-connect", "riot-client",
	// media
	"spotify", "vlc", "plex", "kodi", "obs",
	// creative
	"photoshop", "gimp", "inkscape", "krita", "blender", "davinci-resolve", "audacity", "figma",
	// work
	"word", "excel", "powerpoint", "outlook", "libreoffice", "thunderbird", "notion", "obsidian",
	"1password", "bitwarden", "vscode",
}

// programsLink opens the apps in programs.
type programsLink struct {
	publish func(map[string]bool)
	// find returns how to open the first of an app's candidates that's
	// installed, or nil.
	find  func([]candidate) func() error
	every time.Duration // how often to look again for installed apps

	mu    sync.Mutex
	found map[string]func() error
}

func newProgramsLink(publish func(map[string]bool)) *programsLink {
	return &programsLink{publish: publish, find: findProgram, every: time.Minute}
}

func (l *programsLink) title() string { return "Open app" }

func (l *programsLink) ops() []string { return []string{programsOpen} }

func (l *programsLink) statusText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.found == nil {
		return ""
	}
	if len(l.found) == 1 {
		return "1 app found"
	}
	return fmt.Sprintf("%d apps found", len(l.found))
}

func (l *programsLink) run(stop <-chan struct{}) {
	t := time.NewTicker(l.every)
	defer t.Stop()
	for {
		l.look()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// look finds the installed apps and publishes them.
func (l *programsLink) look() {
	found := map[string]func() error{}
	state := map[string]bool{appPrograms + ".ready": true}
	for _, id := range programs {
		if open := l.find(programCandidates[id]); open != nil {
			found[id] = open
		}
		state[appPrograms+".available:"+id] = found[id] != nil
	}
	l.mu.Lock()
	l.found = found
	l.mu.Unlock()
	l.publish(state)
}

func (l *programsLink) do(op string, args json.RawMessage, release bool) error {
	if release {
		return nil
	}
	if op != programsOpen {
		return fmt.Errorf("programs has no op %q", op)
	}
	var a struct{ ID string }
	if err := json.Unmarshal(args, &a); err != nil || a.ID == "" {
		return errors.New("open needs args.id")
	}
	if !slices.Contains(programs, a.ID) {
		return fmt.Errorf("no app %q to open; is deckhost up to date?", a.ID)
	}
	l.mu.Lock()
	open := l.found[a.ID]
	l.mu.Unlock()
	if open == nil {
		// installed since the last look?
		if open = l.find(programCandidates[a.ID]); open == nil {
			return fmt.Errorf("%s isn't installed on this PC", a.ID)
		}
	}
	return open()
}
