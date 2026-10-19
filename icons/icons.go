// Package icons is the catalog of icons the configuration website offers:
// Material Symbols (Rounded, filled), a few media icons, and brand marks.
// Configs carry the icons they use (deck.Config.Icons); the device has none
// of its own.
package icons

import (
	"maps"
	"slices"
	"sort"

	"github.com/riptidewave93/d200-deck/deck"
)

type symbol struct{ name, d string }

// symbolBox is the Material Symbols viewBox.
const symbolBox = "0 -960 960 960"

// extra are the icons that aren't Material Symbols.
var extra = map[string]deck.Icon{
	"play-pause":  {D: "M8 22L8 78L50 50ZM60 22L71 22L71 78L60 78ZM81 22L92 22L92 78L81 78Z", ViewBox: "0 0 100 100"},
	"next":        {D: "M10 22L10 78L44 50ZM44 22L44 78L78 50ZM78 22L89 22L89 78L78 78Z", ViewBox: "0 0 100 100"},
	"prev":        {D: "M90 22L90 78L56 50ZM56 22L56 78L22 50ZM22 22L11 22L11 78L22 78Z", ViewBox: "0 0 100 100"},
	"mute":        {D: muteD, ViewBox: "0 0 100 100"},
	"volume-up":   {D: "M6 37L24 37L24 63L6 63ZM24 37L46 18L46 82L24 63ZM58 45.5L92 45.5L92 54.5L58 54.5ZM70.5 33L79.5 33L79.5 67L70.5 67Z", ViewBox: "0 0 100 100"},
	"volume-down": {D: "M6 37L24 37L24 63L6 63ZM24 37L46 18L46 82L24 63ZM58 45.5L92 45.5L92 54.5L58 54.5Z", ViewBox: "0 0 100 100"},
	"github":      {D: githubMark, ViewBox: "0.5 0.5 24 24"},
	"discord":     {D: discordSymbol, ViewBox: "-10.18 -25.5 147 147"}, // 86% wide, centred
}

// aliases are other names for Material Symbols.
var aliases = map[string]string{
	"back":            "undo",
	"browser-back":    "arrow_back",
	"browser-forward": "arrow_forward",
	"globe":           "public",
	"hang-up":         "call_end",
	"headphones-off":  "headset_off",
	"launch":          "rocket_launch",
	"mic-off":         "mic_off",
	"open-external":   "open_in_new",
	"reload":          "refresh",
	"sleep":           "bedtime",
}

// Get finds an icon by name: the extra icons, aliases, then Material
// Symbols by their own names (e.g. "volume_up").
func Get(name string) (deck.Icon, bool) {
	if ic, ok := extra[name]; ok {
		return ic, true
	}
	if a, ok := aliases[name]; ok {
		name = a
	}
	i := sort.Search(len(symbols), func(i int) bool { return symbols[i].name >= name })
	if i == len(symbols) || symbols[i].name != name {
		return deck.Icon{}, false
	}
	return deck.Icon{D: symbols[i].d, ViewBox: symbolBox}, true
}

// Names lists every icon name, aliases included, sorted.
func Names() []string {
	names := slices.Collect(maps.Keys(extra))
	names = append(names, slices.Collect(maps.Keys(aliases))...)
	for _, s := range symbols {
		names = append(names, s.name)
	}
	slices.Sort(names)
	return slices.Compact(names) // an alias can share a symbol's name
}

// Embed makes c.Icons hold the icons c's keys can show: it adds those
// missing from the catalog and drops those no key uses. Icons already there
// are kept as they are. It returns the names it couldn't find. c needn't be
// valid otherwise.
func Embed(c *deck.Config) (unknown []string) {
	used := c.IconsUsed()
	icons := map[string]deck.Icon{}
	for _, n := range used {
		if ic, ok := c.Icons[n]; ok {
			icons[n] = ic
		} else if ic, ok := Get(n); ok {
			icons[n] = ic
		} else {
			unknown = append(unknown, n)
		}
	}
	c.Icons = icons
	if len(icons) == 0 {
		c.Icons = nil
	}
	return unknown
}
