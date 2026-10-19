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

func TestLit(t *testing.T) {
	mute := Action{Type: ActionApp, App: AppDiscord, Op: DiscordMute}
	b := Button{Icon: "mic", Label: "Mute", Color: "#ffffff", Action: mute}
	if got := b.lit(); got.Icon != "mic_off" || got.Background != "#c62828" || got.Label != "Mute" || got.Color != "#ffffff" {
		t.Errorf("built-in on look: %+v", got)
	}
	b.On = &Look{Label: "Muted", Image: "x"}
	if got := b.lit(); got.Icon != "" || got.Image != "x" || got.Label != "Muted" || got.Background != "" {
		t.Errorf("custom on look: %+v", got)
	}
	b.Image, b.Icon, b.On = "pic", "", &Look{Icon: "mic_off"}
	if got := b.lit(); got.Image != "" || got.Icon != "mic_off" {
		t.Errorf("on icon over an image: %+v", got)
	}
	plain := Button{Icon: "play", Action: Action{Type: ActionApp, App: AppDiscord, Op: DiscordPushToTalk}}
	if got := plain.lit(); got.Icon != "play" || got.Background != "" {
		t.Errorf("no on look: %+v", got)
	}
}

func TestFallback(t *testing.T) {
	own := &Action{Type: ActionKey, Keys: []string{"f14"}}
	cases := []struct {
		a    Action
		keys string
	}{
		{Action{Type: ActionApp, App: AppDiscord, Op: DiscordMute}, "ctrl shift m"},
		{Action{Type: ActionApp, App: AppDiscord, Op: DiscordDeafen}, "ctrl shift d"},
		{Action{Type: ActionApp, App: AppDiscord, Op: DiscordMute, Fallback: own}, "f14"},
		{Action{Type: ActionApp, App: AppDiscord, Op: DiscordPushToTalk}, ""},
		{Action{Type: ActionOpenURL, URL: "https://x"}, ""},
	}
	for _, tc := range cases {
		f := tc.a.FallbackAction()
		got := ""
		if f != nil {
			got = strings.Join(f.Keys, " ")
		}
		if got != tc.keys {
			t.Errorf("%+v: fallback %q, want %q", tc.a, got, tc.keys)
		}
	}
	a := Action{Type: ActionApp, App: AppDiscord, Op: DiscordMute, Fallback: own}
	if a.ForPC().Fallback != nil || a.Fallback == nil {
		t.Error("ForPC kept the fallback, or changed a")
	}
	if a.Ready() != "discord.ready" || (Action{Type: ActionHTTP}).Ready() != "" {
		t.Error("Ready")
	}
}

func TestShown(t *testing.T) {
	mute := Action{Type: ActionApp, App: AppDiscord, Op: DiscordMute}
	cases := []struct {
		b         Button
		on        bool
		icon      string
		color, bg string
	}{
		{Button{Action: mute}, false, "mic", white, DiscordBlurple},
		{Button{Action: mute}, true, "mic_off", white, "#c62828"},
		{Button{Background: "#000000", Action: mute}, false, "mic", white, "#000000"},
		{Button{Label: "Mute", Action: mute}, false, "", "", ""}, // its own look
		{Button{Action: Action{Type: ActionApp, App: AppDiscord, Op: DiscordPushToTalk}}, true, "record_voice_over", white, DiscordBlurple},
		{Button{Icon: "discord"}, false, "discord", white, ""},
		{Button{Icon: "discord", Color: DiscordBlurple}, false, "discord", DiscordBlurple, ""},
		{Button{Icon: "play"}, false, "play", "", ""},
		{Button{Action: Action{Type: ActionApp, App: "obs", Op: "record"}}, true, "", "", ""}, // no built-in look
	}
	for i, tc := range cases {
		got := tc.b.Shown(tc.on)
		if got.Icon != tc.icon || got.Color != tc.color || got.Background != tc.bg {
			t.Errorf("case %d: %+v", i, got)
		}
	}
}

func TestState(t *testing.T) {
	for _, c := range []struct {
		a    Action
		want string
	}{
		{Action{Type: ActionApp, App: AppDiscord, Op: DiscordMute}, "discord.mute"},
		{Action{Type: ActionApp, App: AppDiscord, Op: DiscordDeafen}, "discord.deafen"},
		{Action{Type: ActionApp, App: "obs", Op: "record"}, "obs.record"},
		{Action{Type: ActionConsumer}, ""},
	} {
		if got := c.a.State(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.a, got, c.want)
		}
	}
}

func TestValidateAppAndOn(t *testing.T) {
	try := func(b Button) error {
		c := DefaultConfig()
		c.Pages[c.Home].Keys[4] = b
		return c.Validate()
	}
	// the device checks only the shape: the host knows the apps
	for _, a := range []Action{
		{Type: ActionApp, App: AppDiscord, Op: DiscordMute},
		{Type: ActionApp, App: "obs", Op: "set-scene", Args: json.RawMessage(`{"scene": "Gaming"}`)},
	} {
		if err := try(Button{Action: a}); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	for i, b := range []Button{
		{Action: Action{Type: ActionApp}},
		{Action: Action{Type: ActionApp, App: "Discord", Op: DiscordMute}},
		{Action: Action{Type: ActionApp, App: AppDiscord, Op: "ready"}},
		{Action: Action{Type: ActionApp, App: AppDiscord, Op: strings.Repeat("x", 33)}},
		{Action: Action{Type: ActionApp, App: "obs", Op: "x", Args: json.RawMessage(`[1]`)}},
		{Action: Action{Type: ActionMacro, Steps: []Action{{Type: ActionApp, App: AppDiscord, Op: DiscordMute}}}},
		{On: &Look{Icon: "a", Image: "b"}},
		{On: &Look{Image: "missing"}},
		{On: &Look{Font: "missing"}},
		{On: &Look{Background: "red"}},
		{On: &Look{Label: strings.Repeat("x", MaxLabel+1)}},
	} {
		if try(b) == nil {
			t.Errorf("case %d valid: %+v", i, b)
		}
	}
	if err := try(Button{Icon: "mic", On: &Look{Icon: "mic_off", Label: "Muted", Background: "#c62828"}, Action: Action{Type: ActionApp, App: AppDiscord, Op: DiscordMute}}); err != nil {
		t.Errorf("good on look: %v", err)
	}
}

func TestAppState(t *testing.T) {
	join := Action{Type: ActionApp, App: AppDiscord, Op: DiscordJoinVoice, Args: json.RawMessage(`{"id": "123"}`)}
	if s := join.State(); s != "discord.join-voice:123" {
		t.Errorf("state %q", s)
	}
	// built-ins are by op, whatever the id
	b := Button{Action: join}.Shown(true)
	if b.Icon != "headset_mic" || b.Background != green {
		t.Errorf("lit join key %+v", b)
	}
	mode := Button{Action: Action{Type: ActionApp, App: AppDiscord, Op: DiscordVoiceMode}}
	if b := mode.Shown(false); b.Label != "Voice" || b.Icon != "graphic_eq" {
		t.Errorf("voice mode %+v", b)
	}
	if b := mode.Shown(true); b.Label != "PTT" || b.Icon != "keyboard" {
		t.Errorf("push to talk mode %+v", b)
	}
	for _, op := range DiscordOps {
		if _, ok := appOps["discord."+op]; !ok {
			t.Errorf("%s has no built-in look", op)
		}
	}
}
