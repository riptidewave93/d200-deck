package deck

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestParseStrict(t *testing.T) {
	good, _ := json.Marshal(DefaultConfig())
	if _, err := ParseStrict(good); err != nil {
		t.Fatalf("default config: %v", err)
	}
	page := func(key string) string {
		return `{"version": 1, "home": "m", "pages": {"m": {"keys": {"0": ` + key + `}}}}`
	}
	for _, c := range []struct{ config, want string }{
		{page(`{"label": "x", "font2": "y"}`), "pages.m.keys.0.font2: unknown field"},
		{page(`{"label": "x", "on": {"blink": true}}`), "pages.m.keys.0.on.blink: unknown field"},
		{page(`{"action": {"type": "macro", "steps": [{"type": "delay", "ms": 5, "repeat": 2}]}}`), "pages.m.keys.0.action.steps.0.repeat: unknown field"},
		{page(`{"action": {"type": "app", "app": "obs", "op": "record", "fallback": {"type": "hid-key", "keys": ["a"], "hold": 1}}}`), "action.fallback.hold: unknown field"},
		{`{"version": 1, "home": "m", "pages": {"m": {"keys": {}}}, "theme": "dark"}`, "theme: unknown field"},
		{`{"version": 1, "home": "m", "pages": {"m": {"keys": {}}}, "icons": {"a": {"d": "M0 0Z", "viewBox": "0 0 1 1", "fill": 1}}}`, "icons.a.fill: unknown field"},
		{page(`{"action": {"type": "app", "app": "discord", "op": "mute", "args": {"id": "1", "volume": 3}}}`), `discord mute: unknown arg "volume"`},
		{page(`{"label": "x"}`) + ` {}`, "data after the config"},
	} {
		if _, err := ParseStrict([]byte(c.config)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.config, err, c.want)
		}
		if strings.Contains(c.want, "unknown field") {
			if _, err := Parse([]byte(c.config)); err != nil {
				t.Errorf("Parse refused %s: %v", c.config, err)
			}
		}
	}
	for _, ok := range []string{
		page(`{"Label": "x"}`), // encoding/json matches names in any case
		page(`{"action": {"type": "app", "app": "media", "op": "now-playing", "args": {"show": "both", "cycle": 5}}}`),
		page(`{"action": {"type": "app", "app": "newapp", "op": "go", "args": {"anything": 1}}}`), // its host checks
	} {
		if _, err := ParseStrict([]byte(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestFeatures(t *testing.T) {
	f := Features()
	c := &Config{Pages: map[string]Page{"p": {}}}
	for _, typ := range f.Actions {
		err := c.validateAction(Action{Type: ActionType(typ)}, true)
		if err != nil && strings.Contains(err.Error(), "unknown action type") {
			t.Errorf("features list %q, which Validate doesn't take", typ)
		}
	}
	for _, typ := range []ActionType{ActionConsumer, ActionApp, ActionMacro} {
		if !slices.Contains(f.Actions, string(typ)) {
			t.Errorf("%q missing", typ)
		}
	}
	for _, field := range []string{"button.on", "action.fallback", "config.fonts", "icon.png"} {
		if !slices.Contains(f.Fields, field) {
			t.Errorf("field %q missing", field)
		}
	}
	if f.Limits["label"] != MaxLabel || f.Limits["anim_bytes"] != MaxAnimBytes || len(f.Apps[AppDiscord]) == 0 || len(f.Args[AppMedia]) == 0 {
		t.Errorf("%+v", f)
	}
}
