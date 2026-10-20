package deck

import (
	"maps"
	"reflect"
	"slices"
	"strings"
)

// FeatureSet is what this version of the device software supports, for a
// configuration tool to offer only that: the names it takes and the limits
// it checks. Fields are "<type>.<field>", e.g. "button.on".
type FeatureSet struct {
	Actions   []string            `json:"actions"`
	Fields    []string            `json:"fields"`
	Keys      []string            `json:"keys"`
	Modifiers []string            `json:"modifiers"`
	Usages    []string            `json:"usages"`
	Systems   []string            `json:"systems"`
	Fonts     []string            `json:"fonts"`
	Apps      map[string][]string `json:"apps"` // ops by app
	Args      map[string][]string `json:"args"` // args by app
	Limits    map[string]int      `json:"limits"`
	// Settings are the settings the device takes (proto.Settings); its
	// settings reply leaves out those its hardware lacks.
	Settings []string `json:"settings"`
	// Commands are the optional config channel commands it takes, e.g.
	// "reset" (proto.CmdReset).
	Commands []string `json:"commands"`
}

// settingNames are proto.Settings' writable fields.
var settingNames = []string{"brightness", "editing", "host_os", "screen_offset", "screen_pattern", "sleep_screen", "usb_drive", "usb_net4"}

// actionTypes are the action types Validate takes.
var actionTypes = []ActionType{
	ActionBack, ActionConsumer, ActionDelay, ActionHome, ActionKey, ActionMacro, ActionPage,
	ActionSystem, ActionText, ActionOpenURL, ActionLaunch, ActionHTTP, ActionApp,
}

// Features are this version's features.
func Features() FeatureSet {
	f := FeatureSet{
		Keys:      slices.Sorted(maps.Keys(Keys)),
		Modifiers: slices.Sorted(maps.Keys(Modifiers)),
		Usages:    slices.Sorted(maps.Keys(UsageNames)),
		Systems:   slices.Sorted(maps.Keys(SystemNames)),
		Fonts:     BuiltinFonts,
		Apps:      map[string][]string{AppDiscord: DiscordOps, AppOBS: OBSOps, AppMedia: MediaOps},
		Args:      AppArgs,
		Settings:  settingNames,
		Commands:  []string{"reset"},
		Limits: map[string]int{
			"label": MaxLabel, "label_lines": MaxLabelLines, "text": MaxText, "steps": MaxSteps,
			"delay_ms": MaxDelayMS, "pc_action": MaxPCAction, "config_bytes": MaxConfigBytes,
			"image_bytes": MaxImageBytes, "image_side": MaxImageSide, "font_bytes": MaxFontBytes,
			"icons": MaxIcons, "icon_path": MaxIconPath, "icon_bytes": MaxIconBytes,
			"icon_side": MaxIconSide, "gif_frames": MaxFrames, "gif_pixels": MaxGIFPixels,
			"anim_bytes": MaxAnimBytes,
			"song_cycle": MaxSongCycle,
		},
	}
	for _, t := range actionTypes {
		f.Actions = append(f.Actions, string(t))
	}
	slices.Sort(f.Actions)
	for _, t := range []reflect.Type{
		reflect.TypeOf(Config{}), reflect.TypeOf(Page{}), reflect.TypeOf(Button{}),
		reflect.TypeOf(Look{}), reflect.TypeOf(Action{}), reflect.TypeOf(Icon{}),
	} {
		for i := range t.NumField() {
			if name := jsonName(t.Field(i)); name != "" {
				f.Fields = append(f.Fields, strings.ToLower(t.Name())+"."+name)
			}
		}
	}
	slices.Sort(f.Fields)
	return f
}
