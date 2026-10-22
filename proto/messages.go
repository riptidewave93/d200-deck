package proto

import (
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/osrelease"
)

// Host look limits.
const (
	MaxHostLooks = 16
	MaxHostLabel = 96 // bytes, up to 3 lines
	MaxHostImage = 64 << 10
)

// HostLook is how the action host wants keys with action state Name to
// look, e.g. the song playing on a now-playing key.
type HostLook struct {
	Host        string `json:"host"`
	Name        string `json:"name"`
	Rev         string `json:"rev"` // the host's name for this version
	Label       string `json:"label,omitempty"`
	ImageSize   int    `json:"image_size,omitempty"` // a JPEG, PNG or GIF
	ImageSHA256 string `json:"image_sha256,omitempty"`
}

// ActionHostReply answers CmdActionHost: the Rev of each look the device
// shows for the host, by Name. Older devices reply with nothing.
type ActionHostReply struct {
	Looks map[string]string `json:"looks,omitempty"`
}

// Hello describes the device.
type Hello struct {
	Protocol int            `json:"protocol"`
	Firmware string         `json:"firmware"` // deckd version
	OS       osrelease.Info `json:"os"`
	Serial   string         `json:"serial"`
	Keys     int            `json:"keys"`
	// Model is the kind of device, its device tree's first compatible
	// string, e.g. ulanzi,d200; "" if unknown.
	Model string `json:"model,omitempty"`
	IPv4  string `json:"ipv4,omitempty"` // its address on the USB network, now
	// Features are what its software supports; a device without them has
	// the first release's.
	Features *deck.FeatureSet `json:"features,omitempty"`
}

// ActionHost identifies an action host and what it can do. ID is random,
// chosen when the host starts.
type ActionHost struct {
	ID      string    `json:"id"`
	Name    string    `json:"name,omitempty"`    // e.g. deckhost
	Version string    `json:"version,omitempty"` // the host program's
	OS      string    `json:"os,omitempty"`      // linux, windows, ...
	Apps    []HostApp `json:"apps,omitempty"`
}

// HostApp is an app integration of the action host.
type HostApp struct {
	Name   string   `json:"name"`             // as in app actions, e.g. discord
	Title  string   `json:"title"`            // for people, e.g. Discord
	Status string   `json:"status,omitempty"` // e.g. connected; "" if not set up
	Ops    []string `json:"ops,omitempty"`
}

// HostInfo is the action host as the device knows it.
type HostInfo struct {
	Connected bool `json:"connected"`
	ActionHost
	State map[string]bool `json:"state,omitempty"`
}

// Time is the time now, in Unix milliseconds.
type Time struct {
	UnixMS int64 `json:"unix_ms"`
}

// Now is the time now, for CmdTime.
func Now() Time { return Time{time.Now().UnixMilli()} }

// KeyEvent is a key pressed or let go on the device.
type KeyEvent struct {
	Key     int  `json:"key"`
	Release bool `json:"release,omitempty"`
}

// ConfigInfo names a config version.
type ConfigInfo struct {
	Rev  string `json:"rev"` // SHA-256 of the config file, hex
	Size int    `json:"size"`
	// GzipSize is the size of the config gzipped, if asked for (protocol 4).
	GzipSize int `json:"gzip_size,omitempty"`
}

// ConfigInfoRequest asks CmdConfigInfo for more.
type ConfigInfoRequest struct {
	Gzip bool `json:"gzip,omitempty"` // fill in ConfigInfo.GzipSize
}

// ConfigWrite starts a config write.
type ConfigWrite struct {
	Base   string `json:"base"` // the Rev it replaces; "" to replace any
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"` // of the new config, hex
	// GzipSize, if set, says the data is the config gzipped, this long
	// (protocol 4).
	GzipSize int `json:"gzip_size,omitempty"`
}

// ActionEvent asks action host Host to run a PC action. The host answers
// with CmdResult and the same ID. When the key is let go, an event with the
// same ID and Release set follows; it gets no result.
type ActionEvent struct {
	ID      uint32      `json:"id"`
	Host    string      `json:"host"`
	Key     int         `json:"key"`
	Release bool        `json:"release,omitempty"`
	Action  deck.Action `json:"action"`
}

// State is set by action host Host: names such as "discord.mute" and their
// values. Names not given keep their value.
type State struct {
	Host  string          `json:"host"`
	State map[string]bool `json:"state"`
}

// Settings are the device settings. In a change, a missing field stays as
// it is.
type Settings struct {
	USBDrive *bool `json:"usb_drive,omitempty"` // the read-only USB drive
	// USBNet4 picks the USB network: the device takes the first address of
	// the /30 holding it, the PC the second. "" is the default, derived from
	// the serial.
	USBNet4 *string `json:"usb_net4,omitempty"`
	// Brightness is the screen's brightness in percent, BrightnessMin to
	// 100; a change applies at once. Only a device with a dimmable screen
	// has it.
	Brightness *int `json:"brightness,omitempty"`
	// BrightnessMin is the lowest Brightness, and the sleep screen's; read
	// only.
	BrightnessMin *int `json:"brightness_min,omitempty"`
	// SleepScreen is what the screen does while the PC sleeps:
	// SleepScreenZzz or SleepScreenOff. Only a device with a dimmable screen
	// has it.
	SleepScreen *string `json:"sleep_screen,omitempty"`
	// HostOS is the PC's OS as its software last said: HostMac, HostOther
	// or "" for not known. On HostMac, keys send their mac_keys.
	HostOS *string `json:"host_os,omitempty"`
	// ScreenOffset moves the picture on the panel, in pixels (x right, y
	// down), to line it up with the key windows; values outside
	// ScreenOffsetMin to ScreenOffsetMax are clamped. Only a device with a
	// screen has it.
	ScreenOffset *Offset `json:"screen_offset,omitempty"`
	// ScreenOffsetMin and ScreenOffsetMax are ScreenOffset's range; read only.
	ScreenOffsetMin *Offset `json:"screen_offset_min,omitempty"`
	ScreenOffsetMax *Offset `json:"screen_offset_max,omitempty"`
	// ScreenPattern shows a frame inside each key window, to line them up.
	// It ends when set false, PatternTimeout after the last change to it or
	// ScreenOffset, or when the PC sleeps.
	ScreenPattern *bool `json:"screen_pattern,omitempty"`
	// Editing has key presses only send their key events, for an editor to
	// select keys: they run no actions and don't change pages. It ends when
	// set false, EditingTimeout after it was last set true, or when the PC
	// sleeps.
	Editing *bool `json:"editing,omitempty"`
	// Panel is the panel config the device picks at boot: PanelAuto, PanelH1
	// or PanelH3. A change restarts the device to apply it. Only firmware
	// that can pick has it.
	Panel *string `json:"panel,omitempty"`
	// PanelActive is the panel config in use, PanelH1 or PanelH3; read only.
	PanelActive *string `json:"panel_active,omitempty"`
}

// Offset is a move on the screen, in pixels.
type Offset struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// PatternTimeout ends a forgotten Settings.ScreenPattern.
const PatternTimeout = time.Minute

// EditingTimeout ends a forgotten Settings.Editing: set it again sooner.
const EditingTimeout = 10 * time.Second

// Host OSes.
const (
	HostMac   = "mac"
	HostOther = "other"
)

// Sleep screens: dimmed with the sleep mark (the default), or off.
const (
	SleepScreenZzz = "zzz"
	SleepScreenOff = "off"
)

// Panel configs: detected from the hardware (the default), or forced.
const (
	PanelAuto = "auto"
	PanelH1   = "h1"
	PanelH3   = "h3"
)

// Result says how a PC action went; Error is empty on success.
type Result struct {
	ID    uint32 `json:"id"`
	Error string `json:"error,omitempty"`
}
