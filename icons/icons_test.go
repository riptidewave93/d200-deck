package icons

import (
	"slices"
	"testing"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/svgpath"
)

func TestSymbolsSortedAndUnique(t *testing.T) {
	for i := 1; i < len(symbols); i++ {
		if symbols[i-1].name >= symbols[i].name {
			t.Fatalf("%q before %q", symbols[i-1].name, symbols[i].name)
		}
	}
}

func TestAllIconsParse(t *testing.T) {
	names := Names()
	if len(names) < 3000 {
		t.Fatalf("%d icons", len(names))
	}
	for _, n := range names {
		ic, ok := Get(n)
		if !ok {
			t.Fatalf("%s: not found", n)
		}
		if len(ic.D) > deck.MaxIconPath {
			t.Errorf("%s: %d bytes", n, len(ic.D))
		}
		if _, err := svgpath.Parse(ic.D); err != nil {
			t.Errorf("%s: %v", n, err)
		}
		if _, _, _, _, err := svgpath.ViewBox(ic.ViewBox); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	if a, _ := Get("reload"); a != must(Get("refresh")) {
		t.Error("an alias isn't its icon")
	}
	if _, ok := Get("no-such-icon"); ok {
		t.Error("found a missing icon")
	}
	if !slices.Contains(names, "launch") || !slices.Contains(names, "discord") {
		t.Error("names missing")
	}
}

func must(ic deck.Icon, ok bool) deck.Icon {
	if !ok {
		panic("no icon")
	}
	return ic
}

func TestEmbed(t *testing.T) {
	custom := deck.Icon{D: "M0 0H1V1Z", ViewBox: "0 0 1 1"}
	c := &deck.Config{
		Pages: map[string]deck.Page{"main": {Keys: map[int]deck.Button{
			0: {Icon: "mine"},
			1: {Action: deck.Action{Type: deck.ActionApp, App: deck.AppDiscord, Op: "mute"}},
			2: {Icon: "nope"},
			3: {Icon: "back", On: &deck.Look{Icon: "globe"}},
		}}},
		Icons: map[string]deck.Icon{"mine": custom, "unused": custom},
	}
	if missing := c.MissingIcons(); !slices.Equal(missing, []string{"back", "globe", "mic", "mic_off", "nope"}) {
		t.Errorf("missing %q", missing)
	}
	if unknown := Embed(c); !slices.Equal(unknown, []string{"nope"}) {
		t.Errorf("unknown %q", unknown)
	}
	for _, n := range []string{"mine", "mic", "mic_off", "back", "globe"} {
		if _, ok := c.Icons[n]; !ok {
			t.Errorf("%s not embedded", n)
		}
	}
	if _, ok := c.Icons["unused"]; ok || c.Icons["mine"] != custom || c.Icons["back"] != must(Get("undo")) {
		t.Errorf("icons %v", len(c.Icons))
	}
}

func TestAppLooks(t *testing.T) {
	// every built-in look's icons are in the catalog
	keys := map[int]deck.Button{}
	for app, ops := range map[string][]string{deck.AppDiscord: deck.DiscordOps, deck.AppOBS: deck.OBSOps, deck.AppMedia: deck.MediaOps} {
		for _, op := range ops {
			keys[len(keys)] = deck.Button{Action: deck.Action{Type: deck.ActionApp, App: app, Op: op}}
		}
	}
	c := &deck.Config{Pages: map[string]deck.Page{"p": {Keys: keys}}}
	if unknown := Embed(c); len(unknown) > 0 {
		t.Errorf("not in the catalog: %q", unknown)
	}
}
