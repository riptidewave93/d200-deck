package deck

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestDefaultConfigValid(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if err := back.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTypeKey(t *testing.T) {
	for _, r := range `Hello, World! ~_+{}|:\"<>?` {
		if _, _, ok := TypeKey(r); !ok {
			t.Errorf("can't type %q", r)
		}
	}
	if _, _, ok := TypeKey('é'); ok {
		t.Error("typed a non-ASCII rune")
	}
}

func TestValidateLimits(t *testing.T) {
	bad := []Button{
		{Label: strings.Repeat("x", MaxLabel+1)},
		{Action: Action{Type: ActionText, Text: strings.Repeat("a", MaxText+1)}},
		{Action: Action{Type: ActionMacro, Steps: make([]Action, MaxSteps+1)}},
		{Action: Action{Type: ActionMacro, Steps: []Action{{Type: ActionDelay, MS: MaxDelayMS + 1}}}},
		{Action: Action{Type: ActionHTTP, URL: "https://x", Body: strings.Repeat("b", MaxPCAction)}},
		{Action: Action{Type: ActionLaunch, Command: slices.Repeat([]string{"arg"}, MaxPCAction/4)}},
	}
	ok := DefaultConfig()
	ok.Pages[ok.Home].Keys[4] = Button{Action: Action{Type: ActionHTTP, URL: "https://x", Body: strings.Repeat("b", MaxPCAction-100)}}
	if err := ok.Validate(); err != nil {
		t.Errorf("PC action under the limit: %v", err)
	}
	for i, b := range bad {
		c := DefaultConfig()
		c.Pages[c.Home].Keys[4] = b
		if err := c.Validate(); err == nil {
			t.Errorf("case %d: over the limit but valid", i)
		}
	}
}
