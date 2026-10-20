package deck

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withKey(b Button) *Config {
	c := DefaultConfig()
	c.Pages[c.Home].Keys[9] = b
	return c
}

func TestValidateActions(t *testing.T) {
	good := []Action{
		{},
		{Type: ActionBack},
		{Type: ActionHome},
		{Type: ActionConsumer, Usage: UsageMute},
		{Type: ActionSystem, System: SystemSleep},
		{Type: ActionPage, Page: "browser"},
		{Type: ActionKey, Keys: []string{"Ctrl", "shift", "f13"}},
		{Type: ActionText, Text: "Hi!\n\t"},
		{Type: ActionMacro, Steps: []Action{{Type: ActionKey, Keys: []string{"a"}}, {Type: ActionDelay, MS: 1}}},
		{Type: ActionOpenURL, URL: "http://example.com"},
		{Type: ActionLaunch, Command: []string{"obs"}},
		{Type: ActionHTTP, URL: "https://example.com/hook"},
		{Type: ActionApp, App: AppDiscord, Op: DiscordPushToTalk, Fallback: &Action{Type: ActionKey, Keys: []string{"ctrl", "f14"}}},
		{Type: ActionOpenURL, URL: "https://x", Fallback: &Action{Type: ActionMacro, Steps: []Action{{Type: ActionText, Text: "x"}}}},
		{Type: ActionApp, App: AppMedia, Op: MediaNowPlaying, Args: json.RawMessage(`{"show": "both", "cycle": 60, "id": "spotify"}`)},
		{Type: ActionApp, App: AppMedia, Op: MediaNext, Args: json.RawMessage(`{"show": "x"}`)}, // not now-playing's
	}
	for _, a := range good {
		if err := withKey(Button{Action: a}).Validate(); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	bad := []Action{
		{Type: "nope"},
		{Type: ActionConsumer},
		{Type: ActionSystem},
		{Type: ActionPage, Page: "missing"},
		{Type: ActionKey},
		{Type: ActionKey, Keys: []string{"hyper"}},
		{Type: ActionKey, Keys: []string{"a", "b", "c", "d", "e", "f", "g"}},
		{Type: ActionText, Text: "é"},
		{Type: ActionMacro, Steps: []Action{{Type: ActionMacro}}},
		{Type: ActionMacro, Steps: []Action{{Type: ActionPage, Page: "main"}}},
		{Type: ActionMacro, Steps: []Action{{Type: ActionOpenURL, URL: "https://x"}}},
		{Type: ActionMacro, Steps: []Action{{Type: ActionConsumer}}},
		{Type: ActionDelay, MS: 10},
		{Type: ActionMacro, Steps: []Action{{Type: ActionDelay}}},
		{Type: ActionOpenURL, URL: "file:///etc/passwd"},
		{Type: ActionLaunch},
		{Type: ActionHTTP, URL: "ftp://x"},
		{Type: ActionKey, Keys: []string{"a"}, Fallback: &Action{Type: ActionKey, Keys: []string{"b"}}},
		{Type: ActionApp, App: AppDiscord, Op: DiscordMute, Fallback: &Action{Type: ActionOpenURL, URL: "https://x"}},
		{Type: ActionApp, App: AppDiscord, Op: DiscordMute, Fallback: &Action{Type: ActionPage, Page: "main"}},
		{Type: ActionApp, App: AppDiscord, Op: DiscordMute, Fallback: &Action{Type: ActionKey}},
		{Type: ActionApp, App: AppMedia, Op: MediaNowPlaying, Args: json.RawMessage(`{"show": "x"}`)},
		{Type: ActionApp, App: AppMedia, Op: MediaNowPlaying, Args: json.RawMessage(`{"cycle": 61}`)},
		{Type: ActionApp, App: AppMedia, Op: MediaNowPlaying, Args: json.RawMessage(`{"cycle": 1.5}`)},
	}
	for _, a := range bad {
		if err := withKey(Button{Action: a}).Validate(); err == nil {
			t.Errorf("%+v: valid", a)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	c := DefaultConfig()
	c.Version = 99
	if c.Validate() == nil {
		t.Error("wrong version is valid")
	}
	c = DefaultConfig()
	c.Home = "nowhere"
	if c.Validate() == nil {
		t.Error("missing home page is valid")
	}
	for _, k := range []int{-1, KeyCount} {
		c = DefaultConfig()
		c.Pages[c.Home].Keys[k] = Button{}
		if c.Validate() == nil {
			t.Errorf("key %d is valid", k)
		}
	}
}

func TestUsageJSON(t *testing.T) {
	for name, u := range UsageNames {
		b, _ := json.Marshal(u)
		if string(b) != `"`+name+`"` {
			t.Errorf("%s marshals to %s", name, b)
		}
	}
	var u Usage
	if err := json.Unmarshal([]byte(`"volume-up"`), &u); err != nil || u != UsageVolumeUp {
		t.Errorf("name: %v, %v", u, err)
	}
	if err := json.Unmarshal([]byte(`394`), &u); err != nil || u != 394 {
		t.Errorf("number: %v, %v", u, err)
	}
	if b, _ := json.Marshal(Usage(394)); string(b) != "394" {
		t.Errorf("unnamed usage marshals to %s", b)
	}
	for _, bad := range []string{`"loud"`, `true`, `-1`} {
		if json.Unmarshal([]byte(bad), &u) == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestSystemJSON(t *testing.T) {
	for name, s := range SystemNames {
		b, err := json.Marshal(s)
		if err != nil || string(b) != `"`+name+`"` {
			t.Errorf("%s marshals to %s, %v", name, b, err)
		}
		var back System
		if err := json.Unmarshal(b, &back); err != nil || back != s {
			t.Errorf("%s: %v, %v", name, back, err)
		}
	}
	if _, err := json.Marshal(System(0x80)); err == nil {
		t.Error("marshalled an unknown control")
	}
	var s System
	for _, bad := range []string{`"nap"`, `2`} {
		if json.Unmarshal([]byte(bad), &s) == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestParseKeys(t *testing.T) {
	mods, keys, err := ParseKeys([]string{"ctrl", "SHIFT", "win", "m", "f24", "0", "enter"})
	if err != nil {
		t.Fatal(err)
	}
	if mods != 0x01|0x02|0x08 {
		t.Errorf("mods %#x", mods)
	}
	want := []byte{0x10, 0x73, 0x27, 0x28}
	if string(keys) != string(want) {
		t.Errorf("keys %#v, want %#v", keys, want)
	}
}

func TestTypeKeyCodes(t *testing.T) {
	cases := map[rune][3]byte{
		'a': {0, 0x04}, 'Z': {0x02, 0x1d}, '1': {0, 0x1e}, '0': {0, 0x27},
		'!': {0x02, 0x1e}, ' ': {0, 0x2c}, '\n': {0, 0x28}, '?': {0x02, 0x38},
	}
	for r, want := range cases {
		mods, key, ok := TypeKey(r)
		if !ok || mods != want[0] || key != want[1] {
			t.Errorf("%q: %#x %#x %v", r, mods, key, ok)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(s), 0o644)
		return p
	}
	b, _ := json.Marshal(DefaultConfig())
	if _, err := Load(write("good.json", string(b))); err != nil {
		t.Error(err)
	}
	if _, err := Load(write("syntax.json", "{")); err == nil {
		t.Error("loaded broken JSON")
	}
	if _, err := Load(write("invalid.json", `{"version":1,"home":"x","pages":{}}`)); err == nil {
		t.Error("loaded an invalid config")
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
}

func TestOnPC(t *testing.T) {
	for _, typ := range []ActionType{ActionOpenURL, ActionLaunch, ActionHTTP} {
		if !typ.OnPC() {
			t.Errorf("%s doesn't run on the PC", typ)
		}
	}
	if ActionKey.OnPC() {
		t.Error("hid-key runs on the PC")
	}
}

func TestSongLook(t *testing.T) {
	np := func(args string) Action {
		return Action{Type: ActionApp, App: AppMedia, Op: MediaNowPlaying, Args: json.RawMessage(args)}
	}
	for _, c := range []struct {
		a     Action
		show  string
		cycle time.Duration
	}{
		{Action{}, SongArt, 3 * time.Second},
		{np(`{}`), SongArt, 3 * time.Second},
		{np(`{"show": "text"}`), SongText, 3 * time.Second},
		{np(`{"show": "both", "cycle": 5}`), SongBoth, 5 * time.Second},
		{Action{Type: ActionApp, App: AppMedia, Op: MediaNext, Args: json.RawMessage(`{"show": "text"}`)}, SongArt, 3 * time.Second},
	} {
		if show, cycle := c.a.SongLook(); show != c.show || cycle != c.cycle {
			t.Errorf("%s: %s %v", c.a.Args, show, cycle)
		}
	}
}
