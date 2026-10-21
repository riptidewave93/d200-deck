package main

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestProgramsTable(t *testing.T) {
	id := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	seen := map[string]bool{}
	for _, p := range programs {
		if !id.MatchString(p) || seen[p] {
			t.Errorf("id %q", p)
		}
		seen[p] = true
	}
	for p := range programCandidates {
		if !seen[p] {
			t.Errorf("%s: candidates for an app not in programs", p)
		}
	}
}

func TestProgramsLink(t *testing.T) {
	var state map[string]bool
	l := newProgramsLink(func(s map[string]bool) { state = s })
	installed := map[string]bool{"chrome": true}
	var opened []string
	l.find = func(cs []candidate) func() error {
		for _, id := range programs {
			pc := programCandidates[id]
			if len(cs) > 0 && len(pc) > 0 && &pc[0] == &cs[0] && installed[id] {
				return func() error { opened = append(opened, id); return nil }
			}
		}
		return nil
	}
	l.look()
	if !state["programs.ready"] || !state["programs.available:chrome"] || state["programs.available:firefox"] {
		t.Fatalf("state %v", state)
	}
	if _, ok := state["programs.available:firefox"]; !ok {
		t.Error("firefox isn't published as off")
	}
	if l.statusText() != "" {
		t.Errorf("status %q", l.statusText())
	}

	open := func(id string) error {
		b, _ := json.Marshal(map[string]string{"id": id})
		return l.do("open", b, false)
	}
	if err := open("chrome"); err != nil || len(opened) != 1 {
		t.Errorf("%v %q", err, opened)
	}
	if err := l.do("open", json.RawMessage(`{"id":"chrome"}`), true); err != nil || len(opened) != 1 {
		t.Error("a release opened it")
	}
	if err := open("firefox"); err == nil || !strings.Contains(err.Error(), "isn't installed") {
		t.Errorf("firefox: %v", err)
	}
	installed["firefox"] = true // installed since the last look
	if err := open("firefox"); err != nil || len(opened) != 2 {
		t.Errorf("%v %q", err, opened)
	}
	for _, bad := range []error{open("nope"), l.do("open", json.RawMessage(`{}`), false), l.do("close", nil, false)} {
		if bad == nil {
			t.Error("no error")
		}
	}
	l.find = func([]candidate) func() error { return func() error { return errors.New("failed") } }
	if err := open("edge"); err == nil {
		t.Error("a failed start isn't an error")
	}
}
