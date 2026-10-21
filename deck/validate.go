package deck

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

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
	if len(a.MacKeys) > 0 {
		if a.Type != ActionKey {
			return errors.New("mac_keys: only on hid-key")
		}
		if _, _, err := ParseKeys(a.MacKeys); err != nil {
			return fmt.Errorf("mac_keys: %w", err)
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
