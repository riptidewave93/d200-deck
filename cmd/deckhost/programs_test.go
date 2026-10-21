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
		if !id.MatchString(p.id) || seen[p.id] {
			t.Errorf("id %q", p.id)
		}
		seen[p.id] = true
		if len(p.windows) == 0 && len(p.linux) == 0 {
			t.Errorf("%s: no way to find it", p.id)
		}
		for _, c := range p.windows {
			if c.kind != "exe" && c.kind != "file" && c.kind != "uri" || c.kind == "uri" && !strings.HasSuffix(c.value, ":") {
				t.Errorf("%s: Windows candidate %+v", p.id, c)
			}
		}
		for _, c := range p.linux {
			if c.kind != "desktop" && c.kind != "run" {
				t.Errorf("%s: Linux candidate %+v", p.id, c)
			}
		}
	}
}

func TestProgramsLink(t *testing.T) {
	var state map[string]bool
	l := newProgramsLink(func(s map[string]bool) { state = s })
	installed := map[string]bool{"chrome": true}
	var opened []string
	l.find = func(cs []candidate) func() error {
		for _, p := range programs {
			if len(cs) > 0 && len(p.candidates()) > 0 && &p.candidates()[0] == &cs[0] && installed[p.id] {
				id := p.id
				return func() error { opened = append(opened, id); return nil }
			}
		}
		return nil
	}
	if l.statusText() != "" {
		t.Error("a status before looking")
	}
	l.look()
	if !state["programs.ready"] || !state["programs.available:chrome"] || state["programs.available:firefox"] {
		t.Fatalf("state %v", state)
	}
	if _, ok := state["programs.available:firefox"]; !ok {
		t.Error("firefox isn't published as off")
	}
	if l.statusText() != "1 app found" {
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
