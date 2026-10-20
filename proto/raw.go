// Package proto is the config channel protocol between the D200 and its
// clients (the website over WebHID, the PC app over hidraw/HID API).
//
// Requests are output reports of RawOutSize bytes:
//
//	[0:2] seq (LE)  [2] cmd  [3:5] payload length (LE)  [5:] payload
//
// Each request is answered by a message: one or more input reports of
// RawInSize bytes,
//
//	[0:2] seq (LE)  [2] status  [3] flags  [4:6] payload length (LE)  [6:] payload
//
// where the first report has FlagFirst and every report but the last has
// FlagMore. Messages are reassembled per seq, so events and replies may
// interleave.
//
// Every client of the device sees every input report. A client starts at a
// random seq and ignores replies to seqs it didn't send. Each client has one
// request in flight at a time: the device keeps few unread output reports,
// dropping the oldest.
//
// The device also sends events on its own: messages with seq EventSeq and
// an event code in place of the status. Requests never use EventSeq.
package proto

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/osrelease"
)

// Report sizes; they match the raw HID report descriptor.
const (
	RawInSize  = 1024
	RawOutSize = 1024

	MaxRequest = RawOutSize - 5
	MaxReply   = RawInSize - 6

	// MaxMessage bounds a reassembled reply or event.
	MaxMessage = 64 << 10
)

// RawUsagePage is the vendor usage page that identifies the config channel.
const RawUsagePage = 0xffd2

// RawDescriptor is the config channel's report descriptor: one vendor
// collection, no report IDs, 1024-byte input and output reports. Hosts
// identify the channel by it.
var RawDescriptor = []byte{
	0x06, RawUsagePage & 0xff, RawUsagePage >> 8, // Usage Page (vendor)
	0x09, 0x01, // Usage (1)
	0xa1, 0x01, // Collection (Application)
	0x15, 0x00, 0x26, 0xff, 0x00, 0x75, 0x08, // bytes
	0x09, 0x02, 0x96, RawInSize & 0xff, RawInSize >> 8, 0x81, 0x02, // Input
	0x09, 0x03, 0x96, RawOutSize & 0xff, RawOutSize >> 8, 0x91, 0x02, // Output
	0xc0,
}

// Version is the protocol version reported by CmdHello, and MinVersion the
// oldest this package talks. It changes only when old and new can't talk;
// features come and go through Hello.Features.
const (
	Version    = 4
	MinVersion = 4
)

// Commands.
const (
	CmdHello = 0x01 // reply: Hello as JSON
	// CmdIdentify (payload: a key number) flashes the key; with no payload,
	// the whole screen white for a second.
	CmdIdentify = 0x03
	CmdResult   = 0x04 // payload: Result as JSON
	// CmdActionHost (payload: ActionHost as JSON) says the sender runs PC
	// actions. The action host sends it every ActionHostPeriod; the device
	// sends EventAction only while it has heard one within
	// ActionHostTimeout, addressed to the host it heard from last.
	CmdActionHost = 0x05
	// CmdState (payload: State as JSON) sets state the keys show, such as
	// whether Discord is muted. The device takes it from the current action
	// host only and forgets it when the host goes away.
	CmdState = 0x06
	// CmdSettings reads the device settings (no payload) or changes the
	// ones given (Settings as JSON). Reply: Settings, after any change.
	CmdSettings = 0x07

	// The config, which can be larger than one request or reply, moves in
	// parts. To read it: CmdConfigInfo, then CmdConfigRead until it's all
	// there; it's the version Rev names if its SHA-256 matches. To write it:
	// CmdConfigWrite, CmdConfigData for each part in order, CmdConfigCommit.

	// CmdConfigInfo (payload: none, or ConfigInfoRequest as JSON). Reply:
	// ConfigInfo as JSON.
	CmdConfigInfo = 0x08
	// CmdConfigRead payload: offset (uint32), length (uint16, 0 for
	// ConfigReadSize; at most MaxConfigRead) and, optionally, flags (byte:
	// ConfigReadGzip). Reply: the bytes there, none past the end.
	CmdConfigRead = 0x09
	// CmdConfigWrite (payload: ConfigWrite as JSON) starts a write,
	// dropping any other unfinished one.
	CmdConfigWrite = 0x0a
	// CmdConfigData payload: offset (uint32), the next one, and the bytes:
	// of the gzip stream if the write has GzipSize.
	CmdConfigData = 0x0b
	// CmdConfigCommit checks the config written, saves and applies it, then
	// sends EventConfigChanged. Reply: ConfigInfo. It fails (StatusFailed,
	// with why) if the config changed since Base or isn't valid. If it gives
	// keys new actions, the device asks first (see ConfirmTimeout).
	CmdConfigCommit = 0x0c

	// CmdHost reports the action host. Reply: HostInfo as JSON.
	CmdHost = 0x0d
	// CmdTime (payload: Time as JSON) sets the device's clock: it has no
	// battery-backed one. Clients send it on connecting and every TimePeriod.
	CmdTime = 0x0e

	// CmdHostLook (payload: HostLook as JSON), from the action host, sets
	// how keys whose action state (deck.Action.State) is Name look: a label,
	// or an image that follows in CmdHostLookData parts. A HostLook with
	// neither clears it. The device keeps up to MaxHostLooks, forgets them
	// with the host, and lists them in its reply to CmdActionHost.
	CmdHostLook = 0x0f
	// CmdHostLookData payload: offset (uint32), the next one, and bytes of
	// the image of the last CmdHostLook. The look shows once it's whole.
	CmdHostLookData = 0x10
	// CmdReset (no payload) erases the device's config and settings and
	// restarts it, once confirmed on the device: OK just before it restarts,
	// or Failed if it isn't confirmed (the same reasons as a config commit's).
	CmdReset = 0x11
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

// TimePeriod is how often clients send the time.
const TimePeriod = time.Hour

// Config transfer sizes.
const (
	ConfigReadSize = 4096
	MaxConfigRead  = 61440
	MaxConfigData  = MaxRequest - 4
)

// ConfigReadGzip, in CmdConfigRead's flags, reads the config gzipped (its
// size is ConfigInfo.GzipSize).
const ConfigReadGzip = 0x01

// Action host heartbeat timing.
const (
	ActionHostPeriod  = 2 * time.Second
	ActionHostTimeout = 5 * time.Second
)

// ConfirmTimeout is how long the device waits for a press when it asks
// about a change: a config that gives keys new actions, or a new USB
// network. The reply comes after the press.
const ConfirmTimeout = 60 * time.Second

// EventSeq is the seq of events.
const EventSeq = 0

// Events.
const (
	EventAction = 0x01 // payload: ActionEvent as JSON
	// EventUSBRebinding says the USB gadget re-plugs in a moment, after a
	// setting changed: reconnect.
	EventUSBRebinding = 0x02
	// EventConfigChanged (payload: ConfigInfo as JSON) says a new config was
	// saved.
	EventConfigChanged = 0x03
	EventKey           = 0x04 // payload: KeyEvent as JSON
)

// Reply statuses.
const (
	StatusOK         = 0x00
	StatusUnknownCmd = 0x01
	StatusBadRequest = 0x02
	StatusFailed     = 0x03 // payload: why
)

// Report flags.
const (
	FlagMore  = 0x01 // another report of the message follows
	FlagFirst = 0x02 // the first report of a message
)

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

// Result says how a PC action went; Error is empty on success.
type Result struct {
	ID    uint32 `json:"id"`
	Error string `json:"error,omitempty"`
}

// Request is a decoded request.
type Request struct {
	Seq     uint16
	Cmd     byte
	Payload []byte
}

// EncodeRequest builds an output report.
func EncodeRequest(seq uint16, cmd byte, payload []byte) ([]byte, error) {
	if len(payload) > MaxRequest {
		return nil, errors.New("request too long")
	}
	b := make([]byte, RawOutSize)
	binary.LittleEndian.PutUint16(b, seq)
	b[2] = cmd
	binary.LittleEndian.PutUint16(b[3:], uint16(len(payload)))
	copy(b[5:], payload)
	return b, nil
}

// DecodeRequest parses an output report.
func DecodeRequest(b []byte) (Request, error) {
	if len(b) < 5 {
		return Request{}, errors.New("short request")
	}
	n := int(binary.LittleEndian.Uint16(b[3:]))
	if n > len(b)-5 {
		return Request{}, errors.New("bad request length")
	}
	return Request{Seq: binary.LittleEndian.Uint16(b), Cmd: b[2], Payload: b[5 : 5+n]}, nil
}

// EncodeReply splits a message into input reports.
func EncodeReply(seq uint16, status byte, payload []byte) [][]byte {
	var out [][]byte
	flags := byte(FlagFirst)
	for {
		n := min(len(payload), MaxReply)
		b := make([]byte, RawInSize)
		binary.LittleEndian.PutUint16(b, seq)
		b[2] = status
		binary.LittleEndian.PutUint16(b[4:], uint16(n))
		copy(b[replyHeader:], payload[:n])
		payload = payload[n:]
		if len(payload) > 0 {
			flags |= FlagMore
		}
		b[3] = flags
		out = append(out, b)
		if len(payload) == 0 {
			return out
		}
		flags = 0
	}
}

// ReplyPart is one decoded input report.
type ReplyPart struct {
	Seq         uint16
	Status      byte
	First, More bool
	Payload     []byte
}

// replyHeader is the input report header's size.
const replyHeader = RawInSize - MaxReply

// DecodeReply parses an input report.
func DecodeReply(b []byte) (ReplyPart, error) {
	if len(b) < replyHeader {
		return ReplyPart{}, errors.New("bad reply")
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	if n > len(b)-replyHeader {
		return ReplyPart{}, errors.New("bad reply")
	}
	return ReplyPart{
		Seq:     binary.LittleEndian.Uint16(b),
		Status:  b[2],
		First:   b[3]&FlagFirst != 0,
		More:    b[3]&FlagMore != 0,
		Payload: b[replyHeader : replyHeader+n],
	}, nil
}
