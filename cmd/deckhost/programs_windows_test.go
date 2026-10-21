package main

import (
	"strings"
	"testing"
)

func TestWindowsCandidates(t *testing.T) {
	for id, cs := range programCandidates {
		for _, c := range cs {
			if c.kind != "exe" && c.kind != "file" && c.kind != "uri" || c.kind == "uri" && !strings.HasSuffix(c.value, ":") {
				t.Errorf("%s: candidate %+v", id, c)
			}
		}
	}
}
