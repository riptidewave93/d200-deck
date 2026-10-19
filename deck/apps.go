package deck

import "encoding/json"

// App integrations. An app action, e.g.
// {"type": "app", "app": "discord", "op": "mute"}, runs on the action host,
// which knows the app; the device knows only these conventions:
//
//   - The host sets state "<app>.ready" while it can run the app's actions
//     (e.g. it's logged in to the app). Until then the device runs the key's
//     fallback instead, if it has one.
//   - A key shows its on look while state "<app>.<op>" is on, or for an
//     action with an id in its args (e.g. a channel), "<app>.<op>:<id>".
//
// So a new app needs only a new deckhost. appOps can add built-in looks and
// fallbacks for its ops; keys can always set their own.

// builtin is what the device knows of an app op; all of it optional.
type builtin struct {
	look     Look     // for a key that sets no icon, image or label
	on       *Look    // while the op's state is on
	fallback []string // a keybind, while the host can't run the op
}

// appOps are the built-in looks and fallbacks, by "<app>.<op>".
var appOps = map[string]builtin{
	"discord.mute":         {discordLook("mic"), &Look{Icon: "mic_off", Background: red}, []string{"ctrl", "shift", "m"}},
	"discord.deafen":       {discordLook("headphones"), &Look{Icon: "headset_off", Background: red}, []string{"ctrl", "shift", "d"}},
	"discord.push-to-talk": {look: discordLook("record_voice_over")},
	"discord.push-to-mute": {look: discordLook("voice_over_off")},
	"discord.open":         {look: discordLook("discord")},
	"discord.channel":      {look: discordLook("tag")},
	"discord.join-voice":   {look: discordLook("headset_mic"), on: &Look{Background: green}},
	"discord.leave-voice":  {look: discordLook("call_end"), on: &Look{Background: red}},
	"discord.voice-mode": {look: discordLabel("graphic_eq", "Voice"),
		on: &Look{Icon: "keyboard", Label: "PTT"}},
	"discord.noise-suppression":  {look: discordLabel("noise_control_off", "Noise"), on: &Look{Icon: "noise_aware"}},
	"discord.echo-cancellation":  {look: discordLabel("spatial_audio_off", "Echo"), on: &Look{Icon: "spatial_audio"}},
	"discord.auto-gain":          {look: discordLabel("tune", "Gain"), on: &Look{Background: green}},
	"discord.input-volume-up":    {look: discordLabel("mic", "Mic +")},
	"discord.input-volume-down":  {look: discordLabel("mic", "Mic −")},
	"discord.output-volume-up":   {look: discordLook("volume_up")},
	"discord.output-volume-down": {look: discordLook("volume_down")},
}

// iconColors are the colours of brand icons in keys that set none, as their
// brand rules require.
var iconColors = map[string]string{"discord": white}

const (
	white = "#ffffff"
	red   = "#c62828"
	green = "#2e7d32"
)

// AppDiscord is the Discord app. Its colours are from discord.com/branding:
// the symbol may be Blurple, white or black, never another colour.
const (
	AppDiscord     = "discord"
	DiscordBlurple = "#5865f2"
)

// Discord operations (Action.Op).
const (
	DiscordMute       = "mute"         // toggle mute
	DiscordDeafen     = "deafen"       // toggle deafen
	DiscordPushToTalk = "push-to-talk" // unmuted while the key is held
	DiscordPushToMute = "push-to-mute" // muted while the key is held

	DiscordOpen       = "open"        // open or focus Discord
	DiscordChannel    = "channel"     // open text channel or DM args.id (in server args.guild)
	DiscordJoinVoice  = "join-voice"  // join voice channel args.id, or leave it
	DiscordLeaveVoice = "leave-voice" // leave the voice channel

	DiscordVoiceMode        = "voice-mode"        // toggle push to talk / voice activity
	DiscordNoiseSuppression = "noise-suppression" // toggle
	DiscordEchoCancellation = "echo-cancellation" // toggle
	DiscordAutoGain         = "auto-gain"         // toggle automatic gain control

	// volume by args.step (default 10): input 0-100, output 0-200
	DiscordInputVolumeUp    = "input-volume-up"
	DiscordInputVolumeDown  = "input-volume-down"
	DiscordOutputVolumeUp   = "output-volume-up"
	DiscordOutputVolumeDown = "output-volume-down"
)

// DiscordOps are the Discord operations; the host checks them.
var DiscordOps = []string{
	DiscordMute, DiscordDeafen, DiscordPushToTalk, DiscordPushToMute,
	DiscordOpen, DiscordChannel, DiscordJoinVoice, DiscordLeaveVoice,
	DiscordVoiceMode, DiscordNoiseSuppression, DiscordEchoCancellation, DiscordAutoGain,
	DiscordInputVolumeUp, DiscordInputVolumeDown, DiscordOutputVolumeUp, DiscordOutputVolumeDown,
}

func discordLook(icon string) Look {
	return Look{Icon: icon, Color: white, Background: DiscordBlurple}
}

func discordLabel(icon, label string) Look {
	l := discordLook(icon)
	l.Label = label
	return l
}

// appOp is "<app>.<op>", the key of the app op's built-ins; "" for actions
// that aren't an app's.
func (a Action) appOp() string {
	if a.Type == ActionApp {
		return a.App + "." + a.Op
	}
	return ""
}

// State names the on/off state the action's key shows, set by the action
// host (proto.State): "<app>.<op>", with ":<id>" if its args have an id.
func (a Action) State() string {
	s := a.appOp()
	if s != "" && len(a.Args) > 0 {
		var args struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(a.Args, &args) == nil && args.ID != "" {
			s += ":" + args.ID
		}
	}
	return s
}

// Ready names the state the action host sets while it can run the action,
// for actions that need more than a connected host.
func (a Action) Ready() string {
	if a.Type == ActionApp {
		return a.App + ".ready"
	}
	return ""
}

// FallbackAction is what a PC action does on the device while the PC can't
// run it: its Fallback, or its app op's built-in keybind. Nil if nothing.
func (a Action) FallbackAction() *Action {
	if a.Fallback != nil {
		return a.Fallback
	}
	if keys := appOps[a.appOp()].fallback; keys != nil {
		return &Action{Type: ActionKey, Keys: keys}
	}
	return nil
}

// Shown is the button as drawn, lit if its action's state is on. An app key
// that sets no icon, image or label gets its op's built-in look, and brand
// icons get their colour.
func (b Button) Shown(on bool) Button {
	if l := appOps[b.Action.appOp()].look; l.Icon != "" && b.Icon == "" && b.Image == "" && b.Label == "" {
		b.Icon, b.Label = l.Icon, l.Label
		if b.Color == "" {
			b.Color = l.Color
		}
		if b.Background == "" {
			b.Background = l.Background
		}
	}
	if on {
		b = b.lit()
	}
	if c, ok := iconColors[b.Icon]; ok && b.Image == "" && b.Color == "" {
		b.Color = c
	}
	return b
}

// isName reports whether s is a valid app or op name.
func isName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
