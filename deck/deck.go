// Package deck is the device model shared by the D200 binary, the PC daemon
// and the configuration app: keys, pages of buttons and what they do.
package deck

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// KeyCount is the number of front keys. Keys are numbered in reading order:
// 0-4 top row, 5-9 middle row, 10-12 bottom row, 13 the wide bottom-right key.
const KeyCount = 14

// ConfigVersion is the config format version this package reads and writes.
const ConfigVersion = 1

// Limits that keep a config's memory use and run time bounded on the device.
const (
	MaxLabel   = 32    // runes in a button label
	MaxText    = 1024  // runes typed by one type-text action
	MaxSteps   = 256   // steps in one macro
	MaxDelayMS = 60000 // one macro delay
)

// RepoURL is the project's source repository.
const RepoURL = "https://github.com/riptidewave93/d200-deck"

// Config is the whole key setup: named pages of buttons, starting at Home.
type Config struct {
	Version int             `json:"version"`
	Home    string          `json:"home"`
	Pages   map[string]Page `json:"pages"`
}

// Page assigns buttons to keys; keys without a button are blank.
type Page struct {
	Keys map[int]Button `json:"keys"`
}

// Button is what a key shows and does.
type Button struct {
	// Icon names a built-in icon.
	Icon   string `json:"icon,omitempty"`
	Label  string `json:"label,omitempty"`
	Action Action `json:"action"`
}

// ActionType says what pressing a key does.
type ActionType string

const (
	// ActionNone does nothing.
	ActionNone ActionType = ""
	// ActionConsumer sends a USB HID consumer control usage (media keys).
	ActionConsumer ActionType = "hid-consumer"
	// ActionPage opens Page, remembering the current page for ActionBack.
	ActionPage ActionType = "page"
	// ActionBack returns to the previous page.
	ActionBack ActionType = "back"
	// ActionHome returns to the home page.
	ActionHome ActionType = "home"
	// ActionKey holds a key combination (Keys) for as long as the key is held.
	ActionKey ActionType = "hid-key"
	// ActionText types Text (printable ASCII, tab and newline; US layout).
	ActionText ActionType = "type-text"
	// ActionMacro runs Steps in order.
	ActionMacro ActionType = "macro"
	// ActionDelay waits MS milliseconds; only valid as a macro step.
	ActionDelay ActionType = "delay"

	// The remaining actions run on the PC and need the PC daemon.

	// ActionOpenURL opens URL in the default browser.
	ActionOpenURL ActionType = "open-url"
	// ActionLaunch runs Command (program and arguments).
	ActionLaunch ActionType = "launch"
	// ActionHTTP sends an HTTP request, e.g. to a webhook.
	ActionHTTP ActionType = "http"
)

// OnPC reports whether the action runs on the PC rather than the device.
func (t ActionType) OnPC() bool {
	return t == ActionOpenURL || t == ActionLaunch || t == ActionHTTP
}

// Action is what pressing a key does; the fields used depend on Type.
type Action struct {
	Type    ActionType        `json:"type,omitempty"`
	Usage   Usage             `json:"usage,omitempty"`
	Page    string            `json:"page,omitempty"`
	Keys    []string          `json:"keys,omitempty"`
	Text    string            `json:"text,omitempty"`
	Steps   []Action          `json:"steps,omitempty"`
	MS      int               `json:"ms,omitempty"`
	URL     string            `json:"url,omitempty"`
	Command []string          `json:"command,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// Usage is a HID consumer control usage (USB HID usage tables, page 0x0C).
// In JSON it is a name from UsageNames or a number.
type Usage uint16

// Common consumer control usages.
const (
	UsageNextTrack Usage = 0x00b5
	UsagePrevTrack Usage = 0x00b6
	UsagePlayPause Usage = 0x00cd
	UsageMute      Usage = 0x00e2
	UsageVolumeUp  Usage = 0x00e9
	UsageVolumeDn  Usage = 0x00ea
)

// UsageNames are the usage names accepted in JSON.
var UsageNames = map[string]Usage{
	"next-track":  UsageNextTrack,
	"prev-track":  UsagePrevTrack,
	"play-pause":  UsagePlayPause,
	"mute":        UsageMute,
	"volume-up":   UsageVolumeUp,
	"volume-down": UsageVolumeDn,
}

func (u Usage) MarshalJSON() ([]byte, error) {
	for name, v := range UsageNames {
		if v == u {
			return json.Marshal(name)
		}
	}
	return json.Marshal(uint16(u))
}

func (u *Usage) UnmarshalJSON(b []byte) error {
	var name string
	if json.Unmarshal(b, &name) == nil {
		v, ok := UsageNames[name]
		if !ok {
			return fmt.Errorf("unknown usage %q", name)
		}
		*u = v
		return nil
	}
	var n uint16
	if err := json.Unmarshal(b, &n); err != nil {
		return errors.New("usage must be a name or a number")
	}
	*u = Usage(n)
	return nil
}

// Validate checks that the config is complete and consistent.
func (c *Config) Validate() error {
	if c.Version != ConfigVersion {
		return fmt.Errorf("version %d, want %d", c.Version, ConfigVersion)
	}
	if _, ok := c.Pages[c.Home]; !ok {
		return fmt.Errorf("home page %q does not exist", c.Home)
	}
	for name, p := range c.Pages {
		for k, b := range p.Keys {
			where := "page " + strconv.Quote(name) + " key " + strconv.Itoa(k)
			if k < 0 || k >= KeyCount {
				return fmt.Errorf("%s: no such key", where)
			}
			if utf8.RuneCountInString(b.Label) > MaxLabel {
				return fmt.Errorf("%s: label longer than %d characters", where, MaxLabel)
			}
			if err := c.validateAction(b.Action, false); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
		}
	}
	return nil
}

func (c *Config) validateAction(a Action, inMacro bool) error {
	switch a.Type {
	case ActionNone, ActionBack, ActionHome:
	case ActionConsumer:
		if a.Usage == 0 {
			return errors.New("hid-consumer needs a usage")
		}
	case ActionPage:
		if _, ok := c.Pages[a.Page]; !ok {
			return fmt.Errorf("page %q does not exist", a.Page)
		}
	case ActionKey:
		if len(a.Keys) == 0 {
			return errors.New("hid-key needs keys")
		}
		if _, _, err := ParseKeys(a.Keys); err != nil {
			return err
		}
	case ActionText:
		if utf8.RuneCountInString(a.Text) > MaxText {
			return fmt.Errorf("type-text longer than %d characters", MaxText)
		}
		for _, r := range a.Text {
			if _, _, ok := TypeKey(r); !ok {
				return fmt.Errorf("type-text can't type %q", r)
			}
		}
	case ActionMacro:
		if inMacro {
			return errors.New("macros can't be nested")
		}
		if len(a.Steps) > MaxSteps {
			return fmt.Errorf("macro has more than %d steps", MaxSteps)
		}
		for i, step := range a.Steps {
			if step.Type == ActionPage || step.Type == ActionBack || step.Type == ActionHome {
				return fmt.Errorf("step %d: %s can't be a macro step", i, step.Type)
			}
			if err := c.validateAction(step, true); err != nil {
				return fmt.Errorf("step %d: %w", i, err)
			}
		}
	case ActionDelay:
		if !inMacro || a.MS <= 0 || a.MS > MaxDelayMS {
			return fmt.Errorf("delay needs ms (1-%d) and is only valid in a macro", MaxDelayMS)
		}
	case ActionOpenURL:
		if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
			return errors.New("open-url needs an http(s) url")
		}
	case ActionLaunch:
		if len(a.Command) == 0 {
			return errors.New("launch needs a command")
		}
	case ActionHTTP:
		if !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
			return errors.New("http needs an http(s) url")
		}
	default:
		return fmt.Errorf("unknown action type %q", a.Type)
	}
	return nil
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// DefaultConfig is used when there is no config file: media keys, folders
// of browser, Discord and macro examples, and a link to the source repository.
func DefaultConfig() *Config {
	media := func(icon string, u Usage) Button {
		return Button{Icon: icon, Action: Action{Type: ActionConsumer, Usage: u}}
	}
	folder := func(icon, label, page string) Button {
		return Button{Icon: icon, Label: label, Action: Action{Type: ActionPage, Page: page}}
	}
	key := func(icon, label string, keys ...string) Button {
		return Button{Icon: icon, Label: label, Action: Action{Type: ActionKey, Keys: keys}}
	}
	back := Button{Icon: "back", Label: "Back", Action: Action{Type: ActionBack}}
	repo := Button{Icon: "github", Label: "Source Repo", Action: Action{Type: ActionOpenURL, URL: RepoURL}}
	return &Config{
		Version: ConfigVersion,
		Home:    "main",
		Pages: map[string]Page{
			"main": {Keys: map[int]Button{
				0:  folder("globe", "Browser", "browser"),
				1:  media("prev", UsagePrevTrack),
				2:  media("play-pause", UsagePlayPause),
				3:  media("next", UsageNextTrack),
				5:  folder("headphones", "Discord", "discord"),
				6:  media("volume-down", UsageVolumeDn),
				7:  media("mute", UsageMute),
				8:  media("volume-up", UsageVolumeUp),
				10: folder("keyboard", "Macros", "macros"),
				13: repo,
			}},
			"browser": {Keys: map[int]Button{
				0:  back,
				1:  key("browser-back", "Back", "alt", "left"),
				2:  key("reload", "Reload", "ctrl", "r"),
				3:  key("browser-forward", "Forward", "alt", "right"),
				6:  key("open-external", "New Tab", "ctrl", "t"),
				13: repo,
			}},
			// Mute and deafen are Discord's default keybinds; disconnect has
			// none, so bind Ctrl+Shift+F13 to it in Discord.
			"discord": {Keys: map[int]Button{
				0: back,
				1: key("mic-off", "Mute", "ctrl", "shift", "m"),
				2: key("headphones-off", "Deafen", "ctrl", "shift", "d"),
				3: key("hang-up", "Leave", "ctrl", "shift", "f13"),
			}},
			"macros": {Keys: map[int]Button{
				0: back,
				1: {Icon: "keyboard", Label: "Hello", Action: Action{Type: ActionText, Text: "Hello from the D200!"}},
				2: {Icon: "terminal", Label: "Copy All", Action: Action{Type: ActionMacro, Steps: []Action{
					{Type: ActionKey, Keys: []string{"ctrl", "a"}},
					{Type: ActionDelay, MS: 50},
					{Type: ActionKey, Keys: []string{"ctrl", "c"}},
				}}},
			}},
		},
	}
}
