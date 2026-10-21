// Package deck is the device model shared by the D200 binary, the PC daemon
// and the configuration app: keys, pages of buttons and what they do.
package deck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

	MaxConfigBytes = 4 << 20   // the whole config file, images included
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
	// HostRepo is deckhost's GitHub repository, whose releases are tagged
	// vMAJOR.MINOR.PATCH.
	HostRepo = "riptidewave93/d200-deck"
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

// ForMac is the action as sent to a Mac: its steps and itself with MacKeys
// in place of Keys, where they have them.
func (a Action) ForMac() Action {
	if len(a.MacKeys) > 0 {
		a.Keys = a.MacKeys
	}
	if len(a.Steps) > 0 {
		steps := make([]Action, len(a.Steps))
		for i, s := range a.Steps {
			steps[i] = s.ForMac()
		}
		a.Steps = steps
	}
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
	// MacKeys, on a hid-key action, are sent instead of Keys to a Mac.
	MacKeys []string `json:"mac_keys,omitempty"`
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
