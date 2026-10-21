package main

import "testing"

func TestMacCandidates(t *testing.T) {
	for id, cs := range programCandidates {
		for _, c := range cs {
			if c.kind != "app" {
				t.Errorf("%s: candidate %+v", id, c)
			}
		}
	}
	// apps every Mac has
	for _, id := range []string{"files", "settings", "terminal", "task-manager", "calculator", "text-editor"} {
		if findProgram(programCandidates[id]) == nil {
			t.Errorf("%s not found", id)
		}
	}
	if findProgram(app("No Such App.app")) != nil {
		t.Error("found a missing app")
	}
}
