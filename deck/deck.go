// Package deck is the device model shared by the D200 binary, the PC daemon
// and the configuration app: keys, pages of buttons and what they do.
package deck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
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
	MaxLabel      = 32      // runes in a button label
	MaxLabelLines = 3       // lines in a label
	MaxText       = 1024    // runes typed by one type-text action
	MaxSteps      = 256     // steps in one macro
	MaxDelayMS    = 60000   // one macro delay
	MaxPCAction   = 8 << 10 // one PC action as JSON: it's sent to the PC on each press

	MaxConfigBytes = 2 << 20   // the whole config file, images included
	MaxImageBytes  = 256 << 10 // one image file, before base64
	MaxImageSide   = 512       // pixels; the keys are 196 px
	MaxFontBytes   = 512 << 10 // one font file, before base64
)

// BuiltinFonts are the fonts deckd has built in. The default is go-medium.
var BuiltinFonts = []string{
	"go-bold", "go-bold-italic", "go-italic", "go-medium", "go-medium-italic", "go-mono",
	"go-mono-bold", "go-mono-bold-italic", "go-mono-italic", "go-regular", "go-smallcaps",
	"go-smallcaps-italic",
}

// Project URLs.
const (
	RepoURL = "https://github.com/riptidewave93/d200-deck"
	// ConfigureURL is the configuration website (not built yet).
	ConfigureURL = "https://riptidewave93.github.io/d200-configure/"
	// OSRepo is the device OS's GitHub repository, whose releases are
	// tagged vMAJOR.MINOR.PATCH.
	OSRepo = "riptidewave93/d200-buildroot"
	// OSUpdateFile is a release's file for the device's update page, by
	// tag and flavour (release or debug).
	OSUpdateFile = "d200-%s-%s-update.tar"
)

// Config is the whole key setup: named pages of buttons, starting at Home.
type Config struct {
	Version int             `json:"version"`
	Home    string          `json:"home"`
	Pages   map[string]Page `json:"pages"`
	// Images are custom icons by ID: PNG, JPEG or GIF files, base64-encoded.
	Images map[string]string `json:"images,omitempty"`
	// Fonts are custom fonts by ID: TrueType or OpenType files, base64-encoded.
	Fonts map[string]string `json:"fonts,omitempty"`
	// Icons are the icons buttons name, by name.
	Icons map[string]Icon `json:"icons,omitempty"`
}

// Page assigns buttons to keys; keys without a button are blank.
type Page struct {
	Keys map[int]Button `json:"keys"`
}

// Button is what a key shows and does. It shows an icon (built-in or
// custom), a label, or both.
type Button struct {
	// Icon names an icon in Config.Icons.
	Icon string `json:"icon,omitempty"`
	// Image names a custom icon in Config.Images, instead of Icon.
	Image string `json:"image,omitempty"`
	// Label may have up to MaxLabelLines lines on a key without an icon.
	Label string `json:"label,omitempty"`
	// Font is the label's font: one of BuiltinFonts or an ID in
	// Config.Fonts; empty means the default.
	Font string `json:"font,omitempty"`
	// Color is the icon and label colour and Background the key's, as
	// "#rrggbb"; empty means the default.
	Color      string `json:"color,omitempty"`
	Background string `json:"background,omitempty"`
	Action     Action `json:"action"`
	// On is how the button looks while its action's state (Action.State) is
	// on, e.g. while Discord is muted: the fields it sets replace the
	// button's. Without it, some actions have a built-in on look.
	On *Look `json:"on,omitempty"`
}

// Look is how a button appears; the fields mean what they do in Button.
type Look struct {
	Icon       string `json:"icon,omitempty"`
	Image      string `json:"image,omitempty"`
	Label      string `json:"label,omitempty"`
	Font       string `json:"font,omitempty"`
	Color      string `json:"color,omitempty"`
	Background string `json:"background,omitempty"`
}

func (b Button) look() Look { return Look{b.Icon, b.Image, b.Label, b.Font, b.Color, b.Background} }

// lit is the button while its action's state is on.
func (b Button) lit() Button {
	on := b.On
	if on == nil {
		on = appOps[b.Action.appOp()].on
		if on == nil {
			return b
		}
	}
	if on.Icon != "" {
		b.Icon, b.Image = on.Icon, ""
	}
	if on.Image != "" {
		b.Image, b.Icon = on.Image, ""
	}
	for _, f := range []struct {
		to   *string
		from string
	}{
		{&b.Label, on.Label}, {&b.Font, on.Font}, {&b.Color, on.Color}, {&b.Background, on.Background},
	} {
		if f.from != "" {
			*f.to = f.from
		}
	}
	return b
}

// ActionType says what pressing a key does.
type ActionType string

const (
	// ActionNone does nothing.
	ActionNone ActionType = ""
	// ActionConsumer sends a USB HID consumer control usage (media keys).
	ActionConsumer ActionType = "hid-consumer"
	// ActionSystem sends a USB HID system control (System: sleep, power down,
	// wake up).
	ActionSystem ActionType = "hid-system"
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
	// ActionApp runs Op of an app integration on the PC (see apps.go).
	ActionApp ActionType = "app"
)

// OnPC reports whether the action runs on the PC rather than the device.
func (t ActionType) OnPC() bool {
	return t == ActionOpenURL || t == ActionLaunch || t == ActionHTTP || t == ActionApp
}

// ForPC is the action as sent to the PC, without the fallback.
func (a Action) ForPC() Action {
	a.Fallback = nil
	return a
}

// Action is what pressing a key does; the fields used depend on Type.
type Action struct {
	Type    ActionType        `json:"type,omitempty"`
	Usage   Usage             `json:"usage,omitempty"`
	System  System            `json:"system,omitempty"`
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
	App     string            `json:"app,omitempty"`
	Op      string            `json:"op,omitempty"`
	Args    json.RawMessage   `json:"args,omitempty"`
	// Fallback, on a PC action, runs on the device instead while the PC
	// can't run the action (see FallbackAction).
	Fallback *Action `json:"fallback,omitempty"`
}

// Usage is a HID consumer control usage (USB HID usage tables, page 0x0C).
// In JSON it is a name from UsageNames or a number.
type Usage uint16

// Common consumer control usages.
const (
	UsageNextTrack Usage = 0x00b5
	UsagePrevTrack Usage = 0x00b6
	UsageStop      Usage = 0x00b7
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
	"stop":        UsageStop,
	"mute":        UsageMute,
	"volume-up":   UsageVolumeUp,
	"volume-down": UsageVolumeDn,
}

// System is a HID system control, as its bit in the system control report.
// In JSON it is a name from SystemNames.
type System byte

// System controls.
const (
	SystemPowerDown System = 1 << 0
	SystemSleep     System = 1 << 1
	SystemWakeUp    System = 1 << 2
)

// SystemNames are the system control names accepted in JSON.
var SystemNames = map[string]System{
	"power-down": SystemPowerDown,
	"sleep":      SystemSleep,
	"wake-up":    SystemWakeUp,
}

func (s System) MarshalJSON() ([]byte, error) {
	for name, v := range SystemNames {
		if v == s {
			return json.Marshal(name)
		}
	}
	return nil, fmt.Errorf("unknown system control %d", s)
}

func (s *System) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	v, ok := SystemNames[name]
	if !ok {
		return fmt.Errorf("unknown system control %q", name)
	}
	*s = v
	return nil
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
	for id, data := range c.Images {
		if !validID(id) {
			return fmt.Errorf("image %q: IDs are 1-64 letters, digits, '-', '_' or '.'", id)
		}
		if _, err := ImageSize(data); err != nil {
			return fmt.Errorf("image %q: %w", id, err)
		}
	}
	for id, data := range c.Fonts {
		if !validID(id) || slices.Contains(BuiltinFonts, id) {
			return fmt.Errorf("font %q: IDs are 1-64 letters, digits, '-', '_' or '.', and not a built-in font's name", id)
		}
		if err := CheckFont(data); err != nil {
			return fmt.Errorf("font %q: %w", id, err)
		}
	}
	if len(c.Icons) > MaxIcons {
		return fmt.Errorf("over %d icons", MaxIcons)
	}
	for name, ic := range c.Icons {
		if !validID(name) {
			return fmt.Errorf("icon %q: names are 1-64 letters, digits, '-', '_' or '.'", name)
		}
		if err := validateIcon(ic); err != nil {
			return fmt.Errorf("icon %q: %w", name, err)
		}
	}
	for name, p := range c.Pages {
		for k, b := range p.Keys {
			where := "page " + strconv.Quote(name) + " key " + strconv.Itoa(k)
			if k < 0 || k >= KeyCount {
				return fmt.Errorf("%s: no such key", where)
			}
			if err := c.validateLook(b.look()); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			if b.On != nil {
				if err := c.validateLook(*b.On); err != nil {
					return fmt.Errorf("%s: on: %w", where, err)
				}
			}
			if err := c.validateAction(b.Action, false); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
		}
	}
	return nil
}

func (c *Config) validateLook(l Look) error {
	if utf8.RuneCountInString(l.Label) > MaxLabel {
		return fmt.Errorf("label longer than %d characters", MaxLabel)
	}
	if strings.Count(l.Label, "\n") >= MaxLabelLines {
		return fmt.Errorf("label has more than %d lines", MaxLabelLines)
	}
	if l.Icon != "" && l.Image != "" {
		return errors.New("icon and image are both set")
	}
	if _, ok := c.Fonts[l.Font]; l.Font != "" && !ok && !slices.Contains(BuiltinFonts, l.Font) {
		return fmt.Errorf("font %q does not exist", l.Font)
	}
	if _, ok := c.Images[l.Image]; l.Image != "" && !ok {
		return fmt.Errorf("image %q does not exist", l.Image)
	}
	for _, col := range []string{l.Color, l.Background} {
		if _, err := ParseColor(col); col != "" && err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateAction(a Action, inMacro bool) error {
	if f := a.Fallback; f != nil {
		if !a.Type.OnPC() {
			return errors.New("only PC actions have a fallback")
		}
		switch f.Type {
		case ActionKey, ActionConsumer, ActionSystem, ActionText, ActionMacro:
		default:
			return fmt.Errorf("fallback: %q can't be one; it must run on the device", f.Type)
		}
		if err := c.validateAction(*f, false); err != nil {
			return fmt.Errorf("fallback: %w", err)
		}
	}
	if a.Type.OnPC() {
		if b, _ := json.Marshal(a.ForPC()); len(b) > MaxPCAction {
			return fmt.Errorf("%s action is over %d bytes", a.Type, MaxPCAction)
		}
	}
	switch a.Type {
	case ActionNone, ActionBack, ActionHome:
	case ActionConsumer:
		if a.Usage == 0 {
			return errors.New("hid-consumer needs a usage")
		}
	case ActionSystem:
		if a.System == 0 {
			return errors.New("hid-system needs a system control")
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
			if step.Type == ActionPage || step.Type == ActionBack || step.Type == ActionHome || step.Type.OnPC() {
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
	case ActionApp:
		if !isName(a.App) || !isName(a.Op) || a.Op == "ready" {
			return errors.New("app needs an app and an op: up to 32 lowercase letters, digits and -")
		}
		if len(a.Args) > 0 {
			var args map[string]any
			if json.Unmarshal(a.Args, &args) != nil {
				return errors.New("app args must be an object")
			}
		}
		if a.appOp() == AppMedia+"."+MediaNowPlaying {
			if _, err := a.songArgs(); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown action type %q", a.Type)
	}
	return nil
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse reads and validates a config file's contents.
func Parse(b []byte) (*Config, error) {
	if len(b) > MaxConfigBytes {
		return nil, fmt.Errorf("larger than %d bytes", MaxConfigBytes)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
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
			"browser": {Keys: map[int]Button{
				0: back,
				1: key("browser-back", "Back", "alt", "left"),
				2: key("reload", "Reload", "ctrl", "r"),
				3: key("browser-forward", "Forward", "alt", "right"),
				6: key("open-external", "New Tab", "ctrl", "t"),
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
			// Shortcuts that work on Windows and most Linux desktops, and in
			// Zoom, Google Meet and Teams.
			"macros": {Keys: map[int]Button{
				0: back,
				1: key("lock", "Lock", "gui", "l"),
				2: key("screenshot_monitor", "Screenshot", "printscreen"),
				3: key("content_paste", "Paste Plain", "ctrl", "shift", "v"),
				5: {Icon: "select_all", Label: "Copy All", Action: Action{Type: ActionMacro, Steps: []Action{
					{Type: ActionKey, Keys: []string{"ctrl", "a"}},
					{Type: ActionDelay, MS: 50},
					{Type: ActionKey, Keys: []string{"ctrl", "c"}},
				}}},
				// in a browser: search for the selected text in a new tab
				6: {Icon: "search", Label: "Search It", Action: Action{Type: ActionMacro, Steps: []Action{
					{Type: ActionKey, Keys: []string{"ctrl", "c"}},
					{Type: ActionDelay, MS: 50},
					{Type: ActionKey, Keys: []string{"ctrl", "t"}},
					{Type: ActionDelay, MS: 300},
					{Type: ActionKey, Keys: []string{"ctrl", "v"}},
					{Type: ActionKey, Keys: []string{"enter"}},
				}}},
				7:  {Icon: "reply", Label: "Reply", Action: Action{Type: ActionText, Text: "Thanks! I'll take a look and get back to you."}},
				10: key("mic_off", "Zoom Mic", "alt", "a"),
				11: key("mic_off", "Meet Mic", "ctrl", "d"),
				12: key("mic_off", "Teams Mic", "ctrl", "shift", "m"),
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
