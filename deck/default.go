package deck

import (
	"encoding/json"
	"maps"
	"strings"
)

// DefaultConfig is used when there is no config file: media keys, folders
// of browser, Discord and macro examples, and a link to the source repository.
func DefaultConfig() *Config {
	media := func(icon string, u Usage) Button {
		return Button{Icon: icon, Action: Action{Type: ActionConsumer, Usage: u}}
	}
	folder := func(icon, label, page string) Button {
		return Button{Icon: icon, Label: label, Action: Action{Type: ActionPage, Page: page}}
	}
	// combos such as "ctrl+r"; a Mac gets mac, if set, instead of keys
	step := func(keys, mac string) Action {
		a := Action{Type: ActionKey, Keys: strings.Split(keys, "+")}
		if mac != "" {
			a.MacKeys = strings.Split(mac, "+")
		}
		return a
	}
	key := func(icon, label, keys, mac string) Button {
		return Button{Icon: icon, Label: label, Action: step(keys, mac)}
	}
	back := Button{Icon: "back", Label: "Back", Action: Action{Type: ActionBack}}
	repo := Button{Icon: "github", Label: "Source Repo", Action: Action{Type: ActionOpenURL, URL: RepoURL}}
	discord := func(icon, label string, a Action) Button {
		return Button{Icon: icon, Label: label, Color: white, Background: DiscordBlurple, Action: a}
	}
	discordOp := func(op string) Action { return Action{Type: ActionApp, App: AppDiscord, Op: op} }
	obsAction := func(op, id string) Action {
		a := Action{Type: ActionApp, App: AppOBS, Op: op}
		if id != "" {
			a.Args, _ = json.Marshal(map[string]string{"id": id})
		}
		return a
	}
	obs := func(op, id string) Button { return Button{Action: obsAction(op, id)} }
	c := &Config{
		Version: ConfigVersion,
		Home:    "main",
		Pages: map[string]Page{
			"main": {Keys: map[int]Button{
				0: folder("globe", "Browser", "browser"),
				// with deckhost, for the player playing, showing whether it
				// is; without, the media keys
				1:  {Action: Action{Type: ActionApp, App: AppMedia, Op: MediaPrevious}},
				2:  {Action: Action{Type: ActionApp, App: AppMedia, Op: MediaPlayPause}},
				3:  {Action: Action{Type: ActionApp, App: AppMedia, Op: MediaNext}},
				4:  {Action: Action{Type: ActionApp, App: AppMedia, Op: MediaNowPlaying}},
				5:  {Icon: "discord", Label: "Discord", Background: DiscordBlurple, Action: Action{Type: ActionPage, Page: "discord"}},
				6:  media("volume-down", UsageVolumeDn),
				7:  media("mute", UsageMute),
				8:  media("volume-up", UsageVolumeUp),
				9:  folder("power_settings_new", "Power", "power"),
				10: folder("keyboard", "Macros", "macros"),
				11: folder("obs", "OBS", "obs"),
				13: repo,
			}},
			// Shortcuts Chrome, Edge and Firefox share on Windows, Linux and Mac
			"browser": {Keys: map[int]Button{
				0:  back,
				1:  key("browser-back", "Back", "alt+left", "gui+leftbrace"),
				2:  key("reload", "Reload", "ctrl+r", "gui+r"),
				3:  key("browser-forward", "Forward", "alt+right", "gui+rightbrace"),
				4:  key("link", "Address", "ctrl+l", "gui+l"),
				5:  key("chevron_left", "Prev Tab", "ctrl+shift+tab", ""),
				6:  key("add", "New Tab", "ctrl+t", "gui+t"),
				7:  key("close", "Close Tab", "ctrl+w", "gui+w"),
				8:  key("history", "Reopen", "ctrl+shift+t", "gui+shift+t"),
				9:  key("chevron_right", "Next Tab", "ctrl+tab", ""),
				10: key("search", "Find", "ctrl+f", "gui+f"),
				11: key("zoom_out", "Zoom Out", "ctrl+minus", "gui+minus"),
				12: key("zoom_in", "Zoom In", "ctrl+equal", "gui+equal"),
				13: key("fullscreen", "Full Screen", "f11", "ctrl+gui+f"),
			}},
			// Mute and deafen use Discord's default keybinds until deckhost
			// is connected to Discord. Leave has no keybind by default: bind
			// Ctrl+Shift+F13 to Disconnect in Discord for it.
			"discord": {Keys: map[int]Button{
				0: back,
				1: discord("mic", "Mute", discordOp(DiscordMute)),
				2: discord("headphones", "Deafen", discordOp(DiscordDeafen)),
				3: discord("hang-up", "Leave", Action{Type: ActionApp, App: AppDiscord, Op: DiscordLeaveVoice,
					Fallback: &Action{Type: ActionKey, Keys: []string{"ctrl", "shift", "f13"}}}),
				4: discord("discord", "Open", discordOp(DiscordOpen)),
				6: discord("graphic_eq", "Voice", discordOp(DiscordVoiceMode)),
				7: discord("noise_control_off", "Noise", discordOp(DiscordNoiseSuppression)),
			}},
			"power": {Keys: map[int]Button{
				0: back,
				1: {Icon: "sleep", Label: "Sleep", Action: Action{Type: ActionSystem, System: SystemSleep}},
				2: {Icon: "power_settings_new", Label: "Power Off", Action: Action{Type: ActionSystem, System: SystemPowerDown}},
			}},
			// Shortcuts that work on Windows, Mac and most Linux desktops, and in
			// Zoom, Google Meet and Teams.
			"macros": {Keys: map[int]Button{
				0: back,
				1: key("lock", "Lock", "gui+l", "ctrl+gui+q"),
				2: key("screenshot_monitor", "Screenshot", "printscreen", "gui+shift+5"),
				3: key("content_paste", "Paste Plain", "ctrl+shift+v", "gui+alt+shift+v"),
				5: {Icon: "select_all", Label: "Copy All", Action: Action{Type: ActionMacro, Steps: []Action{
					step("ctrl+a", "gui+a"),
					{Type: ActionDelay, MS: 50},
					step("ctrl+c", "gui+c"),
				}}},
				// in a browser: search for the selected text in a new tab
				6: {Icon: "search", Label: "Search It", Action: Action{Type: ActionMacro, Steps: []Action{
					step("ctrl+c", "gui+c"),
					{Type: ActionDelay, MS: 50},
					step("ctrl+t", "gui+t"),
					{Type: ActionDelay, MS: 300},
					step("ctrl+v", "gui+v"),
					step("enter", ""),
				}}},
				7:  {Icon: "reply", Label: "Reply", Action: Action{Type: ActionText, Text: "Thanks! I'll take a look and get back to you."}},
				10: key("mic_off", "Zoom Mic", "alt+a", "gui+shift+a"),
				11: key("mic_off", "Meet Mic", "ctrl+d", "gui+d"),
				12: key("mic_off", "Teams Mic", "ctrl+shift+m", "gui+shift+m"),
			}},
			// OBS's default scene and audio sources: rename to match yours
			"obs": {Keys: map[int]Button{
				0: back,
				1: obs(OBSStream, ""),
				2: obs(OBSRecord, ""),
				3: obs(OBSPauseRecord, ""),
				4: obs(OBSVirtualCam, ""),
				5: {Icon: "tv", Label: "Scene", Action: obsAction(OBSScene, "Scene")},
				6: {Icon: "mic", Label: "Mic", Action: obsAction(OBSMute, "Mic/Aux")},
				7: {Icon: "volume_up", Label: "Desktop", On: &Look{Icon: "volume_off", Background: red},
					Action: obsAction(OBSMute, "Desktop Audio")},
				8: obs(OBSReplayBuffer, ""),
				9: obs(OBSSaveReplay, ""),
			}},
		},
	}
	c.Icons = maps.Clone(defaultIcons)
	return c
}
